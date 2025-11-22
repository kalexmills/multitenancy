package controllers

import (
	"context"
	specsv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Manager", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc

		fakeDynamicClient *fakedynamic.FakeDynamicClient
		fakeClient        client.WithWatch
		manager           *Manager
	)
	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		fakeClient = fake.NewFakeClient()
		fakeDynamicClient = fakedynamic.NewSimpleDynamicClient(scheme.Scheme)

		manager = NewManager(ctx, fakeClient, fakeDynamicClient)

		manager.WaitUntilSynced(ctx.Done())
	})

	AfterEach(func() {
		cancel()
	})

	When("a tenant resource is created", func() {
		BeforeEach(func() {
			Expect(fakeClient.Create(ctx, &specsv1alpha1.Tenant{
				ObjectMeta: metav1.ObjectMeta{Name: "test-tenant"},
				Spec: specsv1alpha1.TenantSpec{
					Namespaces: []string{"test-ns1", "test-ns2"},
					Resources:  []string{"test-resource"},
				},
			})).To(Succeed())

			Expect(fakeClient.Create(ctx, &specsv1alpha1.TenantResource{
				ObjectMeta: metav1.ObjectMeta{Name: "test-resource"},
				Spec: specsv1alpha1.TenantResourceSpec{
					Resource: metav1.GroupVersionResource{
						Group:    "",
						Version:  "v1",
						Resource: "configmaps",
					},
					Manifest: runtime.RawExtension{
						Raw: []byte(`{"apiVersion": "v1","kind":"ConfigMap","metadata":{"name":"test-resource"},"data":{"foo":"YmFyCg=="}}`),
					},
				},
			}))
		})

		It("should create copies of tenant resources in each tenant namespace", func() {
			By("waiting for test namespaces to be created")
			Eventually(func(g Gomega) {
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "test-ns1"}, &corev1.Namespace{})).To(Succeed())
				g.Expect(fakeClient.Get(ctx, client.ObjectKey{Name: "test-ns2"}, &corev1.Namespace{})).To(Succeed())
			}).Should(Succeed())

			assertCopy := func(g Gomega, obj runtime.Object) {
				u, ok := obj.(*unstructured.Unstructured)
				g.Expect(ok).To(BeTrue(), "expected an Unstructured, got %T", obj)

				var cm corev1.ConfigMap
				Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &cm)).To(Succeed())

				g.Expect(cm.GetName()).To(Equal("test-resource"))
				g.Expect(cm.GetLabels()).To(HaveKeyWithValue(tenantResourceLabel, "test-resource"))
				g.Expect(cm.GetLabels()).To(HaveKeyWithValue(tenantLabel, "test-tenant"))
				g.Expect(cm.Data).To(HaveKeyWithValue("foo", "YmFyCg=="))
			}

			gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
			Eventually(func(g Gomega) {
				obj, err := fakeDynamicClient.Tracker().Get(gvr, "test-ns1", "test-resource")
				g.Expect(err).ToNot(HaveOccurred())
				assertCopy(g, obj)

				obj, err = fakeDynamicClient.Tracker().Get(gvr, "test-ns2", "test-resource")
				g.Expect(err).ToNot(HaveOccurred())
				assertCopy(g, obj)
			}).Should(Succeed())
		})
	})
})
