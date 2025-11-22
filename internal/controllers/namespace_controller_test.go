package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("NamespaceController", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc

		fakeClient client.Client
		namespaces krtlite.StaticCollection[*corev1.Namespace]
		tenants    krtlite.StaticCollection[*v1alpha1.Tenant]

		namespaceCtrl *NamespaceController
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		fakeClient = fake.NewFakeClient()
		namespaces = krtlite.NewStaticCollection[*corev1.Namespace](nil, nil)
		tenants = krtlite.NewStaticCollection[*v1alpha1.Tenant](nil, nil)
		namespaceCtrl = NewNamespaceController(ctx, fakeClient, namespaces, tenants)

		namespaceCtrl.TenantNamespaces().WaitUntilSynced(ctx.Done())
	})

	AfterEach(func() {
		cancel()
	})

	When("creating a new tenant", func() {
		It("should create namespaces for tenants", func() {
			tenants.Update(&v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo", "bar"},
				},
			})

			Eventually(func(g Gomega) {
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &corev1.Namespace{})).To(Succeed())
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "bar"}, &corev1.Namespace{})).To(Succeed())
			}).Should(Succeed())
		})

		It("should include labels from the tenant", func() {
			tenants.Update(&v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
					Labels:     map[string]string{"bar": "baz"},
				},
			})

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels["bar"]).To(Equal("baz"))
				g.Expect(ns.Labels[tenantLabel]).To(Equal("foo"))
			}).Should(Succeed())

			namespaceCtrl.TenantNamespaces().WaitUntilSynced(ctx.Done())
			Expect(namespaceCtrl.TenantNamespaces().List()).To(HaveLen(1))
			Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).ToNot(BeNil())
		})

		It("should update namespaces which already exist", func() {
			Expect(fakeClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
			})).To(Succeed())

			tenants.Update(&v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
					Labels:     map[string]string{"bar": "baz"},
				},
			})

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels["bar"]).To(Equal("baz"))
				g.Expect(ns.Labels[tenantLabel]).To(Equal("foo"))
			}).Should(Succeed())
		})

		It("should output a TenantNamespace entry per namespace", func() {
			namespaceCtrl.TenantNamespaces().WaitUntilSynced(ctx.Done())

			tenants.Update(&v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo", "bar"},
				},
			})

			Eventually(func(g Gomega) {
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).ToNot(BeNil())
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/bar")).ToNot(BeNil())
			}).Should(Succeed())
		})
	})

	When("updating an existing tenant", func() {
		It("should create any added namespaces", func() {
			tenant := &v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
				},
			}
			tenants.Update(tenant)

			tenant.Spec.Namespaces = append(tenant.Spec.Namespaces, "bar")
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "bar"}, &corev1.Namespace{})).To(Succeed())
			}).Should(Succeed())
		})

		It("should remove labels on namespaces no longer managed by a tenant", func() {
			tenant := &v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
				},
			}
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels[tenantLabel]).To(Equal("foo"))
			}).Should(Succeed())

			tenant.Spec.Namespaces = nil
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels[tenantLabel]).To(BeEmpty())
			}).Should(Succeed())
		})

		It("should update labels on existing namespaces", func() {
			tenant := &v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
				},
			}
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels[tenantLabel]).To(Equal("foo"))
			}).Should(Succeed())

			tenant.Spec.Labels = map[string]string{"bar": "baz"}
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels[tenantLabel]).To(Equal("foo"))
				g.Expect(ns.Labels["bar"]).To(Equal("baz"))
			}).Should(Succeed())
		})

		It("should output a TenantNamespace entry when namespaces are added", func() {
			namespaceCtrl.TenantNamespaces().WaitUntilSynced(ctx.Done())

			tenant := &v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
				},
			}
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).ToNot(BeNil())
			}).Should(Succeed())

			tenant.Spec.Namespaces = append(tenant.Spec.Namespaces, "bar")
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).ToNot(BeNil())
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/bar")).ToNot(BeNil())
			}).Should(Succeed())
		})

		It("should remove a TenantNamespace entry when namespaces are removed", func() {
			namespaceCtrl.TenantNamespaces().WaitUntilSynced(ctx.Done())

			tenant := &v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
				},
			}
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).ToNot(BeNil())
			}).Should(Succeed())

			tenant.Spec.Namespaces = nil
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).To(BeNil())
			}).Should(Succeed())
		})
	})

	When("deleting a tenant", func() {
		It("should remove tenant labels from namespaces", func() {
			tenant := &v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
				},
			}
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels[tenantLabel]).To(Equal("foo"))
			}).Should(Succeed())

			tenants.Delete("foo")

			Eventually(func(g Gomega) {
				var ns corev1.Namespace
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "foo"}, &ns)).To(Succeed())
				g.Expect(ns.Labels[tenantLabel]).To(BeEmpty())
			})
		})

		It("should remove a TenantNamespaces entry when tenants are removed", func() {
			tenant := &v1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "foo"},
				Spec: v1alpha1.TenantSpec{
					Namespaces: []string{"foo"},
				},
			}
			tenants.Update(tenant)

			Eventually(func(g Gomega) {
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).ToNot(BeNil())
			}).Should(Succeed())

			tenants.Delete("foo")

			Eventually(func(g Gomega) {
				g.Expect(namespaceCtrl.TenantNamespaces().GetKey("foo/foo")).To(BeNil())
			})
		})
	})
})
