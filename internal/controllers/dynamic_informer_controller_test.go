package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	specsv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"time"
)

var _ = Describe("DynamicInformerController", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc

		fakeClient       *fake.FakeDynamicClient
		tenantResources  krtlite.StaticCollection[*specsv1alpha1.TenantResource]
		tenantNamespaces krtlite.StaticCollection[TenantNamespace]

		dynamicInfCtrl *DynamicInformerController
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		fakeClient = fake.NewSimpleDynamicClient(scheme.Scheme)
		tenantResources = krtlite.NewStaticCollection[*specsv1alpha1.TenantResource](nil, nil)
		tenantNamespaces = krtlite.NewStaticCollection[TenantNamespace](nil, nil)

		dynamicInfCtrl = NewDynamicInformerController(ctx, fakeClient, tenantResources, tenantNamespaces)
		dynamicInfCtrl.DynamicInformers().WaitUntilSynced(ctx.Done())
	})

	AfterEach(func() {
		cancel()
	})

	When("a TenantResource is created", func() {
		BeforeEach(func() {
			tenantNamespaces.Update(TenantNamespace{
				Tenant: &specsv1alpha1.Tenant{
					ObjectMeta: metav1.ObjectMeta{
						Name: "tenant",
					},
					Spec: specsv1alpha1.TenantSpec{
						Resources: []string{"tenant-resource"},
					},
				},
				Namespace: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "ns"},
				},
			})

			tenantResources.Update(&specsv1alpha1.TenantResource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "tenant-resource",
				},
				Spec: specsv1alpha1.TenantResourceSpec{
					Resource: metav1.GroupVersionResource{
						Group:    "apps",
						Version:  "v1",
						Resource: "deployments",
					},
				},
			})
		})

		It("should create and start a DynamicInformer for the resource", func() {
			var dynamicInf *DynamicInformer
			Eventually(func(g Gomega) {
				infPtr := dynamicInfCtrl.DynamicInformers().GetKey("apps/v1/deployments")
				g.Expect(infPtr).ToNot(BeNil())
				dynamicInf = *infPtr
			}).Should(Succeed())

			dynamicInf.Collection.WaitUntilSynced(ctx.Done())

			By("verifying that the informer is watching the correct resource")
			Expect(fakeClient.Tracker().Create(
				schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
				&unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "apps/v1",
						"kind":       "Deployment",
						"metadata": map[string]any{
							"name":      "deployment",
							"namespace": "test",
							"labels": map[string]any{
								tenantResourceLabel: "tenant",
							},
						},
					},
				},
				"test",
			)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(dynamicInf.Collection.GetKey("test/deployment")).ToNot(BeNil())
			}).Should(Succeed())
		})

		It("should not recreate a DynamicInformer if it already exists", func() {
			var dynamicInf *DynamicInformer
			Eventually(func(g Gomega) {
				infPtr := dynamicInfCtrl.DynamicInformers().GetKey("apps/v1/deployments")

				g.Expect(infPtr).ToNot(BeNil())
				dynamicInf = *infPtr
			}).Should(Succeed())

			tenantNamespaces.Update(TenantNamespace{
				Tenant: &specsv1alpha1.Tenant{
					ObjectMeta: metav1.ObjectMeta{
						Name: "tenant-2",
					},
					Spec: specsv1alpha1.TenantSpec{
						Resources: []string{"tenant-resource"},
					},
				},
				Namespace: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "ns"},
				},
			})

			Consistently(func(g Gomega) {
				g.Expect(dynamicInfCtrl.DynamicInformers().GetKey("apps/v1/deployments")).To(Equal(&dynamicInf))
			}).Within(2 * time.Second).Should(Succeed())
		})

		It("should stop and delete DynamicInformers once all of their TenantResources have been deleted", func() {
			var dynamicInf *DynamicInformer
			Eventually(func(g Gomega) {
				infPtr := dynamicInfCtrl.DynamicInformers().GetKey("apps/v1/deployments")

				g.Expect(infPtr).ToNot(BeNil())
				dynamicInf = *infPtr
			}).Should(Succeed())

			dynamicInf.Collection.WaitUntilSynced(ctx.Done())

			tenantResources.Delete("tenant-resource")

			Eventually(func(g Gomega) {
				g.Expect(dynamicInfCtrl.DynamicInformers().GetKey("apps/v1/deployments")).To(BeNil())
			}).Should(Succeed())

			Expect(dynamicInf.stopCh).To(BeClosed())
		})
	})
})
