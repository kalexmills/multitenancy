package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"log/slog"
	"maps"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"slices"
	"strings"
	"sync"
)

//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;create;update;delete
//+kubebuilder:rbac:groups=specs.kalexmills.com,resources=tenants;tenantresources,verbs=get;list;watch;update

const tenantQuotaName = "tenant-quota"
const tenantLabel = "multitenancy.kalexmills.com/tenant"                  // TODO: move to separate package
const tenantResourceLabel = "multitenancy.kalexmills.com/tenant-resource" // TODO: move to separate package

const (
	namespaceStatusPending  = "Pending"
	namespaceStatusReady    = "Ready"
	namespaceStatusError    = "Error"
	namespaceStatusDeleting = "Deleting"
)

type NamespaceStatus struct {
	Namespace string
	Tenant    string
	Status    string
}

func (s NamespaceStatus) Key() string {
	return s.Tenant + "/" + s.Namespace
}

type TenantNamespaceResources struct {
	Tenant    *v1alpha1.Tenant
	Namespace *corev1.Namespace
	Resources []*v1alpha1.TenantResource
}

func (t TenantNamespaceResources) Key() string {
	return t.Tenant.Name + "/" + t.Namespace.Name
}

type NamespaceResource struct {
	Tenant               *v1alpha1.Tenant
	Namespace            string
	ResourceID           string
	Object               *unstructured.Unstructured
	GroupVersionResource schema.GroupVersionResource
}

func (t NamespaceResource) Key() string {
	return strings.Join([]string{t.Tenant.Name, t.ResourceID, t.Namespace}, "/")
}

func (t TenantNamespaceResources) NewStatus(status string) NamespaceStatus {
	return NamespaceStatus{
		Namespace: t.Namespace.Name,
		Tenant:    t.Tenant.Name,
		Status:    status,
	}
}

type DynamicInformer struct {
	GroupVersionResource metav1.GroupVersionResource
	InformerCollection   krtlite.Collection[*unstructured.Unstructured]

	stopCh    chan struct{}
	closeStop *sync.Once
}

func (dc *DynamicInformer) Key() string {
	return dc.GroupVersionResource.String()
}

func (dc *DynamicInformer) Stop() {
	dc.closeStop.Do(func() {
		close(dc.stopCh)
	})
}

type GVRUnstructured struct {
	Manifest *unstructured.Unstructured
	GVR      schema.GroupVersionResource
}

type TenantController struct {
	client  client.WithWatch
	dynamic dynamic.Interface

	Namespaces         krtlite.Collection[*corev1.Namespace]
	Tenants            krtlite.Collection[*v1alpha1.Tenant]
	TenantResources    krtlite.Collection[*v1alpha1.TenantResource]
	TenantNamespaces   krtlite.Collection[TenantNamespaceResources]
	NamespaceResources krtlite.Collection[NamespaceResource]
	ResourceTypesInUse krtlite.Collection[metav1.GroupVersionResource]

	// DynamicCollections is a collection of DynamicInformers which are registered for types being watched.
	DynamicCollections krtlite.StaticCollection[DynamicInformer]
}

func NewTenantController(ctx context.Context, watchClient client.WithWatch, dynamicClient dynamic.Interface) *TenantController {
	tc := &TenantController{
		client:  watchClient,
		dynamic: dynamicClient,
	}

	opts := []krtlite.CollectionOption{krtlite.WithContext(ctx)}

	// setup informers
	tc.Namespaces = krtlite.NewInformer[*corev1.Namespace, corev1.NamespaceList](ctx, watchClient, opts...)
	tc.Tenants = krtlite.NewInformer[*v1alpha1.Tenant, v1alpha1.TenantList](ctx, watchClient, opts...)
	tc.TenantResources = krtlite.NewInformer[*v1alpha1.TenantResource, v1alpha1.TenantResourceList](ctx, watchClient, opts...)

	// Track TenantNamespaceResources groupings and create namespaces
	tc.TenantNamespaces = krtlite.FlatMap[*v1alpha1.Tenant, TenantNamespaceResources](tc.Tenants, tc.toNamespaces, opts...)
	tc.TenantNamespaces.Register(tc.reconcileNamespaceHandler(ctx))

	// Track GVRs used in TenantNamespaces, and create and track a new DynamicInformer for each of them.
	tc.ResourceTypesInUse = krtlite.FlatMap[TenantNamespaceResources, metav1.GroupVersionResource](tc.TenantNamespaces, tc.toGVRs, opts...)
	tc.ResourceTypesInUse.Register(tc.dynamicCollectionHandler(ctx))

	// Track individual Resources which need to be placed in Namespaces.
	tc.NamespaceResources = krtlite.FlatMap[TenantNamespaceResources, NamespaceResource](tc.TenantNamespaces, tc.toNamespaceResources, opts...)
	tc.NamespaceResources.Register(tc.namespaceResourceHandler(ctx))

	return tc
}

func (c *TenantController) toNamespaces(ktx krtlite.Context, tenant *v1alpha1.Tenant) []TenantNamespaceResources {
	lbls := labels.Merge(tenant.Labels, map[string]string{tenantLabel: tenant.Name})

	resources := krtlite.Fetch(ktx, c.TenantResources, krtlite.MatchNames(tenant.Spec.Resources...))

	var result []TenantNamespaceResources
	for _, nsName := range tenant.Spec.Namespaces {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,

				Labels: lbls,
			},
		}
		result = append(result, TenantNamespaceResources{
			Namespace: ns,
			Tenant:    tenant,
			Resources: resources,
		})
	}

	return result
}

func (c *TenantController) toGVRs(ktx krtlite.Context, tns TenantNamespaceResources) []metav1.GroupVersionResource {
	result := make(map[metav1.GroupVersionResource]struct{})

	for _, r := range tns.Resources {
		result[r.Spec.Resource] = struct{}{}
	}
	return slices.Collect(maps.Keys(result))
}

// toNamespaceResources creates a NamespaceResource for every TenantNamespaceResource that needs to be created.
func (c *TenantController) toNamespaceResources(ktx krtlite.Context, tns TenantNamespaceResources) []NamespaceResource {
	var result []NamespaceResource

	for _, r := range tns.Resources {
		mfst := r.Spec.Manifest

		// copy the manifest and override any needed fields
		obj := mfst.DeepCopy()

		obj.SetNamespace(tns.Namespace.Name)
		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		resourceID := cache.MetaObjectToName(r).String()
		labels[tenantResourceLabel] = resourceID
		obj.SetLabels(labels)

		result = append(result, NamespaceResource{
			Tenant:               tns.Tenant,
			Namespace:            tns.Namespace.Name,
			ResourceID:           resourceID,
			GroupVersionResource: r.SchemaGVR(),
			Object:               obj,
		})
	}
	return result
}

func (c *TenantController) dynamicCollectionHandler(ctx context.Context) func(krtlite.Event[metav1.GroupVersionResource]) {
	return func(ev krtlite.Event[metav1.GroupVersionResource]) {
		gvr := ev.Latest()

		coll := c.DynamicCollections.GetKey(gvr.String())

		switch ev.Type {
		case krtlite.EventAdd:
			if coll != nil {
				slog.InfoContext(ctx, "received add event for existing dynamic collection", "gvr", gvr)
				return
			}

			stopCh := make(chan struct{})
			inf := krtlite.NewDynamicInformer(c.dynamic, schema.GroupVersionResource{
				Group:    gvr.Group,
				Version:  gvr.Version,
				Resource: gvr.Resource,
			}, krtlite.WithStop(stopCh))

			c.DynamicCollections.Update(DynamicInformer{
				InformerCollection:   inf,
				GroupVersionResource: gvr,
				stopCh:               stopCh,
			})

			// TODO: register for updates to keep items reconciled and index the handler

		case krtlite.EventUpdate:
			slog.ErrorContext(ctx, "error, GroupVersionResource was updated -- entire object is key", "event", ev)

		// shutdown the collection and remove it from the static collection.
		case krtlite.EventDelete:
			coll.Stop()
			c.DynamicCollections.Delete(coll.Key())
		}
	}
}

// namespaceResourceHandler is responsible for creating resources in namespaces.
func (c *TenantController) namespaceResourceHandler(ctx context.Context) func(krtlite.Event[NamespaceResource]) {
	return func(ev krtlite.Event[NamespaceResource]) {
		var (
			nr            = ev.Latest()
			obj           = nr.Object
			dynamicClient = c.dynamic.Resource(nr.GroupVersionResource).Namespace(nr.Namespace)
		)

		switch ev.Type {
		case krtlite.EventAdd:
			_, err := dynamicClient.Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				if !errors.IsAlreadyExists(err) {
					slog.ErrorContext(ctx, "error creating object", "error", err)
				}
				if _, err := dynamicClient.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
					slog.ErrorContext(ctx, "error updating existing tenant resource", "error", err)
				}
				return
			}

		case krtlite.EventDelete:
			err := dynamicClient.Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					slog.ErrorContext(ctx, "error creating object", "error", err)
				}
			}

		case krtlite.EventUpdate:
			_, err := dynamicClient.Update(ctx, obj, metav1.UpdateOptions{})
			if err != nil {
				slog.ErrorContext(ctx, "error updating object", "error", err)
			}
		}
	}
}

// reconcileNamespaceHandler is responsible for CRUD on namespaces.
func (c *TenantController) reconcileNamespaceHandler(ctx context.Context) func(krtlite.Event[TenantNamespaceResources]) {
	return func(ev krtlite.Event[TenantNamespaceResources]) {
		var (
			tns    = ev.Latest()
			ns     = tns.Namespace
			status = tns.NewStatus("")
			err    error
		)

		switch ev.Type {
		case krtlite.EventAdd:
			err = c.client.Create(ctx, ns)
			if !errors.IsAlreadyExists(err) {
				slog.ErrorContext(ctx, "error creating ns", slog.Any("err", err), slog.String("ns", ns.Name))
				status.Status = namespaceStatusError
			} else {
				status.Status = namespaceStatusPending
			}

		case krtlite.EventDelete:
			err = c.client.Delete(ctx, ns)
			if !errors.IsNotFound(err) {
				slog.ErrorContext(ctx, "error deleting ns", slog.Any("err", err), slog.String("ns", ns.Name))

				status.Status = namespaceStatusError
			} else {
				status.Status = namespaceStatusDeleting
			}

		case krtlite.EventUpdate:
			// the only changes we need to make are to namespace labels.
			if labels.Equals(ns.Labels, (*ev.Old).Namespace.Labels) {
				return
			}

			err = c.client.Update(ctx, ns)
			if err != nil {
				slog.ErrorContext(ctx, "error updating ns", slog.Any("err", err), slog.String("ns", ns.Name))
				status.Status = namespaceStatusError
			}
		}
	}
}

func (c *TenantController) statusHandler(ctx context.Context) func(krtlite.Event[NamespaceStatus]) {
	return func(ev krtlite.Event[NamespaceStatus]) {
		status := ev.Latest()

		tenantPtr := c.Tenants.GetKey(status.Tenant)
		if tenantPtr == nil {
			slog.ErrorContext(ctx, "unknown tenant for namespace status update", slog.Any("namespaceStatus", ev.Latest()))
			return
		}

		tenant := *tenantPtr

		switch ev.Type {
		case krtlite.EventAdd, krtlite.EventUpdate:
			if tenant.Status.NamespaceStatuses == nil {
				tenant.Status.NamespaceStatuses = map[string]string{
					status.Namespace: status.Status,
				}
			} else {
				tenant.Status.NamespaceStatuses[status.Namespace] = status.Status
			}

		case krtlite.EventDelete:
			if tenant.Status.NamespaceStatuses != nil {
				delete(tenant.Status.NamespaceStatuses, status.Namespace)
			}
		}

		err := c.client.Status().Update(ctx, tenant)
		if err != nil {
			slog.ErrorContext(ctx, "error updating tenant status", slog.String("err", err.Error()), slog.String("tenant", tenant.Name))
		}
	}
}

// simpleReconciler is an event handler which performs simple CRUD operations for each event using the provided client.
// Errors are logged via slog.
func simpleReconciler[T client.Object](ctx context.Context, cli client.Client) func(ev krtlite.Event[T]) {
	slogArgs := func(err error, obj client.Object) []any {
		return []any{
			slog.String("err", err.Error()),
			slog.String("kind", obj.GetObjectKind().GroupVersionKind().String()),
			slog.String("name", obj.GetNamespace()),
			slog.String("namespace", obj.GetNamespace()),
		}
	}

	return func(ev krtlite.Event[T]) {
		obj := ev.Latest()

		switch ev.Type {
		case krtlite.EventAdd:
			err := cli.Create(ctx, obj)
			if err != nil {
				if !errors.IsAlreadyExists(err) {
					slog.ErrorContext(ctx, "error creating object", slogArgs(err, obj)...)
				}
			}
		case krtlite.EventUpdate:
			err := cli.Update(ctx, obj)
			if err != nil {
				slog.ErrorContext(ctx, "error updating object", slogArgs(err, obj)...)
			}
		case krtlite.EventDelete:
			err := cli.Delete(ctx, obj)
			if err != nil {
				if !errors.IsNotFound(err) {
					slog.ErrorContext(ctx, "error deleting object", slogArgs(err, obj)...)
				}
			}
		}
	}
}
