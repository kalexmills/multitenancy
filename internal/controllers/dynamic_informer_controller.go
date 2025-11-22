package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	specsv1alpha1 "github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"log/slog"
	"maps"
	"slices"
	"sync"
)

// DynamicInformerController stores a collection of DynamicInformers -- a collection of collections. The multitenancy
// controller needs an informer for every kind specified in a TenantResource. When TenantResources with a new kind are
// created or delete, new informers will need to be created or deleted. This controller ensures these informers are
// life-cycled properly.
type DynamicInformerController struct {
	client dynamic.Interface

	// input collections
	tenantResources krtlite.Collection[*specsv1alpha1.TenantResource]

	// internal collections
	gvrCollection krtlite.Collection[GroupVersionResource]

	// output collections
	dynamicInformers krtlite.StaticCollection[*DynamicInformer]
}

func NewDynamicInformerController(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	tenantResources krtlite.Collection[*specsv1alpha1.TenantResource],
	tenantNamespaces krtlite.Collection[TenantNamespace],
) *DynamicInformerController {
	res := &DynamicInformerController{
		client:          dynamicClient,
		tenantResources: tenantResources,
	}

	opts := []krtlite.CollectionOption{
		krtlite.WithContext(ctx),
	}

	// To ensure we only set up informers for TenantResources which are actually in use, we map TenantNamespaces to
	// TenantResources, and form a collection of all GVRs in use across all TenantNamespaces.
	res.gvrCollection = krtlite.FlatMap(tenantNamespaces, res.mapToGVRs, opts...)

	// Then we watch each resource in gvrCollection, and create a new DynamicInformer whenever they are changed. These
	// DynamicInformers are stored in a Static collection. We could use FlatMap instead of a static collection, but
	// creating a new informer is a side effect which breaks the pure function requirement.
	res.gvrCollection.Register(res.dynamicCollectionHandler(ctx))
	res.dynamicInformers = krtlite.NewStaticCollection[*DynamicInformer](res.gvrCollection, nil, opts...)

	return res
}

// DynamicInformers is a collection of DynamicInformers keyed by the GroupVersionResource they watch.
func (c *DynamicInformerController) DynamicInformers() krtlite.Collection[*DynamicInformer] {
	return c.dynamicInformers
}

// mapToGVRs maps TenantNamespaces to a list of GVRs for any TenantResources they contain.
func (c *DynamicInformerController) mapToGVRs(ktx krtlite.Context, tns TenantNamespace) []GroupVersionResource {
	result := make(map[GroupVersionResource]struct{})

	resources := krtlite.Fetch(ktx, c.tenantResources, krtlite.MatchNames(tns.Tenant.Spec.Resources...))

	for _, r := range resources {
		result[GroupVersionResource{r.Spec.Resource}] = struct{}{}
	}
	return slices.Collect(maps.Keys(result))
}

// dynamicCollectionHandler creates or deletes a new DynamicCollection for each GroupVersionResource used in a
// TenantResource.
func (c *DynamicInformerController) dynamicCollectionHandler(ctx context.Context) func(krtlite.Event[GroupVersionResource]) {
	return func(ev krtlite.Event[GroupVersionResource]) {
		gvr := ev.Latest()

		l := slog.With("gvr", gvr.Key(), "event", ev.Type)

		coll := c.dynamicInformers.GetKey(gvr.Key())

		switch ev.Type {
		case krtlite.EventAdd:
			if coll != nil {
				l.InfoContext(ctx, "received add event for existing dynamic collection", "gvr", gvr)
				return
			}

			schemaGVR := schema.GroupVersionResource{
				Group:    gvr.Group,
				Version:  gvr.Version,
				Resource: gvr.Resource,
			}

			stopCh := make(chan struct{})

			inf := krtlite.NewDynamicInformer(c.client, schemaGVR,
				krtlite.WithFilterByLabel(tenantResourceLabel),
				krtlite.WithStop(stopCh))

			c.dynamicInformers.Update(&DynamicInformer{
				Collection: inf,
				gvrKey:     gvr,
				stopCh:     stopCh,
				closeStop:  &sync.Once{},
			})

			l.InfoContext(ctx, "started dynamic informer")

		case krtlite.EventUpdate:
			l.ErrorContext(ctx, "error, GroupVersionResource was updated -- the entire object is a key, and keys should not change")

		// shutdown the collection and remove it from the static collection.
		case krtlite.EventDelete:
			if coll != nil {
				coll := *coll
				coll.Stop()
				c.dynamicInformers.Delete(coll.Key())
				l.InfoContext(ctx, "deleted dynamic informer")
			}
		}
	}
}
