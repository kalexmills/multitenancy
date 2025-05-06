package controllers

import (
	"context"
	"fmt"
	krtlite "github.com/kalexmills/krt-lite"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"log/slog"
	"maps"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"slices"
	"strings"
	"sync"
)

// TODO: tighten up RBAC after testing
//+kubebuilder:rbac:groups=*,resources=*,verbs=*
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=specs.kalexmills.com,resources=tenants;tenantresources,verbs=get;list;watch;update

const tenantLabel = "multitenancy.kalexmills.com/tenant"                  // TODO: move to separate package
const tenantResourceLabel = "multitenancy.kalexmills.com/tenant-resource" // TODO: move to separate package

type TenantNamespaceResources struct {
	Tenant    *v1alpha1.Tenant
	Namespace *corev1.Namespace
	Resources []*v1alpha1.TenantResource
}

func (t TenantNamespaceResources) Key() string {
	return t.Tenant.Name + "/" + t.Namespace.Name
}

type GroupVersionResource struct {
	metav1.GroupVersionResource
}

func (g GroupVersionResource) Key() string {
	return strings.Join([]string{g.Group, g.Version, g.Resource}, ",")
}

func (g GroupVersionResource) GroupVersion() metav1.GroupVersion {
	return metav1.GroupVersion{Group: g.Group, Version: g.Version}
}

type NamespaceResource struct {
	Tenant               *v1alpha1.Tenant
	Namespace            string
	ResourceName         string
	Object               *unstructured.Unstructured
	GroupVersionResource schema.GroupVersionResource
}

func (t NamespaceResource) Key() string {
	return strings.Join([]string{t.Tenant.Name, t.ResourceName, t.Namespace}, "/")
}

type DynamicInformer struct {
	GroupVersionResource GroupVersionResource
	Wrapped              krtlite.Collection[UnstructuredResource]
	InformerCollection   krtlite.Collection[*unstructured.Unstructured]
	Joined               krtlite.Collection[krtlite.Joined[NamespaceResource, UnstructuredResource]]

	stopCh    chan struct{}
	closeStop *sync.Once
}

func (dc DynamicInformer) Key() string {
	return dc.GroupVersionResource.Key()
}

func (dc DynamicInformer) Stop() {
	dc.closeStop.Do(func() {
		close(dc.stopCh)
	})
}

type GVRUnstructured struct {
	Manifest *unstructured.Unstructured
	GVR      schema.GroupVersionResource
}

type UnstructuredResource struct {
	Unstructured *unstructured.Unstructured
}

func (r UnstructuredResource) Key() string {
	return strings.Join([]string{
		r.Unstructured.GetLabels()[tenantLabel],
		r.Unstructured.GetLabels()[tenantResourceLabel],
		r.Unstructured.GetNamespace(),
	}, "/")
}

type TenantController struct {
	client    client.WithWatch
	dynamic   dynamic.Interface
	discovery discovery.DiscoveryInterface

	Namespaces               krtlite.Collection[*corev1.Namespace]
	Tenants                  krtlite.Collection[*v1alpha1.Tenant]
	TenantResources          krtlite.Collection[*v1alpha1.TenantResource]
	TenantNamespaceResources krtlite.Collection[TenantNamespaceResources]
	NamespaceResources       krtlite.Collection[NamespaceResource]
	ResourceTypesInUse       krtlite.Collection[GroupVersionResource]

	// DynamicCollections is a collection of DynamicInformers which are registered for types being watched.
	DynamicCollections krtlite.StaticCollection[DynamicInformer]
}

func NewTenantController(
	ctx context.Context,
	watchClient client.WithWatch,
	dynamicClient dynamic.Interface,
	discoveryClient discovery.DiscoveryInterface,
) *TenantController {
	tc := &TenantController{
		client:    watchClient,
		dynamic:   dynamicClient,
		discovery: discoveryClient,
	}

	opts := []krtlite.CollectionOption{krtlite.WithContext(ctx)}

	// setup informers
	tc.Namespaces = krtlite.NewInformer[*corev1.Namespace, corev1.NamespaceList](ctx, watchClient, opts...)
	tc.Tenants = krtlite.NewInformer[*v1alpha1.Tenant, v1alpha1.TenantList](ctx, watchClient, opts...)
	tc.TenantResources = krtlite.NewInformer[*v1alpha1.TenantResource, v1alpha1.TenantResourceList](ctx, watchClient, opts...)

	// Track TenantNamespaceResources groupings and create namespaces
	tc.TenantNamespaceResources = krtlite.FlatMap[*v1alpha1.Tenant, TenantNamespaceResources](tc.Tenants, tc.mapToTenantNamespaceResources, opts...)
	tc.TenantNamespaceResources.Register(tc.reconcileNamespaceHandler(ctx))

	// Track individual Resources which need to be placed in Namespaces.
	tc.NamespaceResources = krtlite.FlatMap[TenantNamespaceResources, NamespaceResource](tc.TenantNamespaceResources, tc.mapToNamespaceResources, opts...)

	// Track GVRs used in TenantNamespaceResources, and create and track a new DynamicInformer for each of them.
	tc.ResourceTypesInUse = krtlite.FlatMap[TenantNamespaceResources, GroupVersionResource](tc.TenantNamespaceResources, tc.mapToGVRs, opts...)
	tc.ResourceTypesInUse.Register(tc.dynamicCollectionHandler(ctx))

	tc.DynamicCollections = krtlite.NewStaticCollection[DynamicInformer](tc.ResourceTypesInUse, nil, opts...)

	return tc
}

func (c *TenantController) mapToTenantNamespaceResources(ktx krtlite.Context, tenant *v1alpha1.Tenant) []TenantNamespaceResources {

	resources := krtlite.Fetch(ktx, c.TenantResources, krtlite.MatchNames(tenant.Spec.Resources...))
	namespaces := krtlite.Fetch(ktx, c.Namespaces, krtlite.MatchNames(tenant.Spec.Namespaces...))

	byName := make(map[string]*corev1.Namespace)
	for _, ns := range namespaces {
		byName[ns.Name] = ns
	}

	var result []TenantNamespaceResources
	for _, nsName := range tenant.Spec.Namespaces {
		ns, ok := byName[nsName]
		if !ok {
			ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
		}

		ns.Labels = labels.Merge(tenant.Spec.Labels, map[string]string{tenantLabel: tenant.Name})

		result = append(result, TenantNamespaceResources{
			Namespace: ns,
			Tenant:    tenant,
			Resources: resources,
		})
	}

	return result
}

func (c *TenantController) mapToGVRs(ktx krtlite.Context, tns TenantNamespaceResources) []GroupVersionResource {
	result := make(map[GroupVersionResource]struct{})

	for _, r := range tns.Resources {
		result[GroupVersionResource{r.Spec.Resource}] = struct{}{}
	}
	return slices.Collect(maps.Keys(result))
}

// mapToNamespaceResources creates a NamespaceResource for every TenantNamespaceResource that needs to be created.
func (c *TenantController) mapToNamespaceResources(ktx krtlite.Context, tns TenantNamespaceResources) []NamespaceResource {
	var result []NamespaceResource

	for _, r := range tns.Resources {
		ext := r.Spec.Manifest

		// unmarshal the manifest and override any needed fields
		var mapAny map[string]any
		if err := json.Unmarshal(ext.Raw, &mapAny); err != nil {
			slog.Error("error unmarshalling manifest", "err", err)
			continue
		}

		obj := &unstructured.Unstructured{Object: mapAny}

		// override namespace to match target
		obj.SetNamespace(tns.Namespace.Name)
		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		// set labels for tracking
		labels[tenantResourceLabel] = r.Name
		labels[tenantLabel] = tns.Tenant.Name
		obj.SetLabels(labels)

		result = append(result, NamespaceResource{
			Tenant:               tns.Tenant,
			Namespace:            tns.Namespace.Name,
			ResourceName:         r.Name,
			GroupVersionResource: r.SchemaGVR(),
			Object:               obj,
		})
	}
	return result
}

// tenantResourceHandler reconciles tenant resources.
func (c *TenantController) tenantResourceHandler(ctx context.Context) func(krtlite.Event[krtlite.Joined[NamespaceResource, UnstructuredResource]]) {
	return func(ev krtlite.Event[krtlite.Joined[NamespaceResource, UnstructuredResource]]) {
		// TODO: make this work with the joined resource.
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
				if !errors.IsNotFound(err) {
					slog.ErrorContext(ctx, "error updating object", "error", err)
					return
				}
				_, err = dynamicClient.Create(ctx, obj, metav1.CreateOptions{})
				if err != nil {
					slog.ErrorContext(ctx, "error creating object during update", "error", err)
				}
			}
		}
	}
}

// reconcileNamespaceHandler is responsible for keeping tenant namespaces up-to-date.
func (c *TenantController) reconcileNamespaceHandler(ctx context.Context) func(krtlite.Event[TenantNamespaceResources]) {
	return func(ev krtlite.Event[TenantNamespaceResources]) {
		var (
			tns = ev.Latest()
			ns  = tns.Namespace
			err error
		)

		switch ev.Type {

		case krtlite.EventAdd:
			if ns.CreationTimestamp.IsZero() {
				err = c.client.Create(ctx, ns)
				if !errors.IsAlreadyExists(err) {
					slog.ErrorContext(ctx, "error creating ns", "err", err, "ns", ns.Name)
					return
				}
			}
			err = c.client.Update(ctx, ns)
			if err != nil {
				slog.ErrorContext(ctx, "error updating namespace", "err", err, "ns", ns.Name)
			}

		case krtlite.EventUpdate:
			// the only changes we need to make are to namespace labels.
			if labels.Equals((*ev.Old).Namespace.Labels, (*ev.New).Namespace.Labels) {
				return
			}

			err = c.client.Update(ctx, ns)
			if err != nil {
				if !errors.IsNotFound(err) {
					slog.ErrorContext(ctx, "error updating ns", "err", err, "ns", ns.Name)
					return
				}
				err := c.client.Create(ctx, ns)
				if err != nil {
					slog.ErrorContext(ctx, "error creating ns", "err", err, "ns", ns.Name)
				}
			}

		case krtlite.EventDelete:
			slog.Info("namespace no longer managed by tenant")
			delete(ns.Labels, tenantResourceLabel)
			err := c.client.Update(ctx, ns)
			if err != nil {
				slog.ErrorContext(ctx, "error updating namespace to remove tenant label", "err", err, "ns", ns.Name)
			}
		}
	}
}

// dynamicCollectionHandler manages dynamic collections for new types that show up in TenantResources.
func (c *TenantController) dynamicCollectionHandler(ctx context.Context) func(krtlite.Event[GroupVersionResource]) {
	return func(ev krtlite.Event[GroupVersionResource]) {
		gvr := ev.Latest()

		coll := c.DynamicCollections.GetKey(gvr.Key())

		switch ev.Type {
		case krtlite.EventAdd:
			if coll != nil {
				slog.InfoContext(ctx, "received add event for existing dynamic collection", "gvr", gvr)
				return
			}
			schemaGVR := schema.GroupVersionResource{
				Group:    gvr.Group,
				Version:  gvr.Version,
				Resource: gvr.Resource,
			}
			stopCh := make(chan struct{})
			inf := krtlite.NewDynamicInformer(c.dynamic, schemaGVR,
				krtlite.WithFilterByLabel(tenantResourceLabel), krtlite.WithStop(stopCh))

			dynInf := DynamicInformer{
				InformerCollection:   inf,
				GroupVersionResource: gvr,
				stopCh:               stopCh,
			}

			// TODO: need WithConversion for simple mapping like this which doesn't deserve its own queue.

			dynInf.Wrapped = krtlite.Map(dynInf.InformerCollection,
				func(ktx krtlite.Context, i *unstructured.Unstructured) *UnstructuredResource {
					return &UnstructuredResource{Unstructured: i}
				},
				krtlite.WithStop(stopCh),
			)

			c.DynamicCollections.Update(dynInf)

			dynInf.Joined = krtlite.Join(c.NamespaceResources, dynInf.Wrapped, krtlite.LeftJoin, krtlite.WithStop(stopCh))

			dynInf.Joined.Register(c.tenantResourceHandler(ctx))

		case krtlite.EventUpdate:
			slog.ErrorContext(ctx, "error, GroupVersionResource was updated -- entire object is key", "event", ev)

		// shutdown the collection and remove it from the static collection.
		case krtlite.EventDelete:
			if coll != nil {
				coll := *coll
				coll.Stop()
				c.DynamicCollections.Delete(coll.Key())
			}
		}
	}
}

func (c *TenantController) clientFor(obj *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvk := obj.GroupVersionKind()

	r, err := c.client.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("error fetching GVR from rest mapping: %w", err)
	}

	return c.dynamic.Resource(r.Resource).Namespace(obj.GetNamespace()), nil
}

// simpleReconciler is an event handler which performs simple CRUD operations for each event using the provided client.
// Any errors which occur are logged via slog.
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
