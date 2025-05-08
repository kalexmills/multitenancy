package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TODO: tighten up RBAC after testing
//+kubebuilder:rbac:groups=*,resources=*,verbs=*
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=specs.kalexmills.com,resources=tenants;tenantresources,verbs=get;list;watch;update

// A Manager is responsible for bootstrapping all controllers and setting up dependencies between them.
type Manager struct {
	Namespaces      krtlite.Collection[*corev1.Namespace]
	Tenants         krtlite.Collection[*v1alpha1.Tenant]
	TenantResources krtlite.Collection[*v1alpha1.TenantResource]

	cNamespaces       *NamespaceController
	cDynamicResources *TenantResourceController
	cDynamicInformers *DynamicInformerController

	ResourceTypesInUse krtlite.Collection[GroupVersionResource]

	// DynamicCollections is a collection of DynamicInformers which are registered for types being watched.
	DynamicCollections krtlite.StaticCollection[DynamicInformer]
}

// NewManager creates and starts a new manager. The manager will stop when the provided context is cancelled.
func NewManager(
	ctx context.Context,
	watchClient client.WithWatch,
	dynamicClient dynamic.Interface,
) *Manager {
	tc := &Manager{}

	opts := []krtlite.CollectionOption{krtlite.WithContext(ctx)}

	//
	tc.Namespaces = krtlite.NewInformer[*corev1.Namespace, corev1.NamespaceList](ctx, watchClient, opts...)
	tc.Tenants = krtlite.NewInformer[*v1alpha1.Tenant, v1alpha1.TenantList](ctx, watchClient, opts...)
	tc.TenantResources = krtlite.NewInformer[*v1alpha1.TenantResource, v1alpha1.TenantResourceList](ctx, watchClient, opts...)

	tc.cNamespaces = NewNamespaceController(ctx, watchClient,
		tc.Namespaces, tc.Tenants)

	tc.cDynamicInformers = NewDynamicInformerController(ctx, dynamicClient,
		tc.cNamespaces.TenantNamespaces)

	tc.cDynamicResources = NewDynamicResourceController(ctx, dynamicClient,
		tc.cNamespaces.TenantNamespaces, tc.cDynamicInformers.DynamicInformers)

	return tc
}

func cleanObj(obj *unstructured.Unstructured) *unstructured.Unstructured {
	res := obj.DeepCopy()
	unstructured.RemoveNestedField(res.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(res.Object, "metadata", "generation")
	unstructured.RemoveNestedField(res.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(res.Object, "status")
	return res
}
