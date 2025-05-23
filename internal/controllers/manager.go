package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//+kubebuilder:rbac:groups=*,resources=*,verbs=*
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=specs.kalexmills.com,resources=tenants;tenantresources,verbs=get;list;watch;update

// A Manager is responsible for bootstrapping all controllers and setting up dependencies between them.
type Manager struct {
	// collections owned by this component.
	namespaces      krtlite.Collection[*corev1.Namespace]
	tenants         krtlite.Collection[*v1alpha1.Tenant]
	tenantResources krtlite.Collection[*v1alpha1.TenantResource]

	cNamespaces       *NamespaceController
	cDynamicResources *TenantResourceController
	cDynamicInformers *DynamicInformerController
}

// NewManager creates and starts a new manager. The manager will stop when the provided context is cancelled.
func NewManager(
	ctx context.Context,
	watchClient client.WithWatch,
	dynamicClient dynamic.Interface,
) *Manager {
	tc := &Manager{}

	opts := []krtlite.CollectionOption{krtlite.WithContext(ctx)}

	// setup all informers
	tc.namespaces = krtlite.NewInformer[*corev1.Namespace, corev1.NamespaceList](ctx, watchClient, opts...)
	tc.tenants = krtlite.NewInformer[*v1alpha1.Tenant, v1alpha1.TenantList](ctx, watchClient, opts...)
	tc.tenantResources = krtlite.NewInformer[*v1alpha1.TenantResource, v1alpha1.TenantResourceList](ctx, watchClient, opts...)

	// setup controllers for any types we need to watch. We also set up some dynamic watches at runtime via the
	// DynamicInformerController.
	tc.cNamespaces = NewNamespaceController(ctx, watchClient,
		tc.Namespaces(), tc.Tenants())

	tc.cDynamicInformers = NewDynamicInformerController(ctx, dynamicClient,
		tc.TenantResources(), tc.cNamespaces.TenantNamespaces())

	tc.cDynamicResources = NewTenantResourceController(ctx, dynamicClient,
		tc.TenantResources(), tc.cNamespaces.TenantNamespaces(), tc.cDynamicInformers.DynamicInformers())

	return tc
}

// Namespaces is an informer-backed collection of Namespaces in Kubernetes.
func (m *Manager) Namespaces() krtlite.Collection[*corev1.Namespace] {
	return m.namespaces
}

// Tenants is an informer-backed collection of Tenants CRs in Kubernetes.
func (m *Manager) Tenants() krtlite.Collection[*v1alpha1.Tenant] {
	return m.tenants
}

// TenantResources is an informer-backed collection of TenantResource CRs in Kubernetes.
func (m *Manager) TenantResources() krtlite.Collection[*v1alpha1.TenantResource] {
	return m.tenantResources
}
