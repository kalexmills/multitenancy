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
)

type DynamicInformerController struct {
	client          dynamic.Interface
	tenantResources krtlite.Collection[*specsv1alpha1.TenantResource]

	GVRs             krtlite.Collection[GroupVersionResource]
	DynamicInformers krtlite.StaticCollection[DynamicInformer]
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

	res.GVRs = krtlite.FlatMap(tenantNamespaces, res.mapToGVRs, opts...)
	res.GVRs.Register(res.dynamicCollectionHandler(ctx))

	res.DynamicInformers = krtlite.NewStaticCollection[DynamicInformer](res.GVRs, nil, opts...)

	return res
}

func (c *DynamicInformerController) mapToGVRs(ktx krtlite.Context, tns TenantNamespace) []GroupVersionResource {
	result := make(map[GroupVersionResource]struct{})

	resources := krtlite.Fetch(ktx, c.tenantResources, krtlite.MatchNames(tns.Tenant.Spec.Resources...))

	for _, r := range resources {
		result[GroupVersionResource{r.Spec.Resource}] = struct{}{}
	}
	return slices.Collect(maps.Keys(result))
}

func (c *DynamicInformerController) dynamicCollectionHandler(ctx context.Context) func(krtlite.Event[GroupVersionResource]) {
	return func(ev krtlite.Event[GroupVersionResource]) {
		gvr := ev.Latest()

		l := slog.With("gvr", gvr.Key(), "event", ev.Type)

		coll := c.DynamicInformers.GetKey(gvr.Key())

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

			c.DynamicInformers.Update(DynamicInformer{
				Collection: inf,

				gvrKey: gvr,
				stopCh: stopCh,
			})

			l.InfoContext(ctx, "started dynamic informer")

		case krtlite.EventUpdate:
			l.ErrorContext(ctx, "error, GroupVersionResource was updated -- entire object is key")

		// shutdown the collection and remove it from the static collection.
		case krtlite.EventDelete:
			if coll != nil {
				coll := *coll
				coll.Stop()
				c.DynamicInformers.Delete(coll.Key())
				l.InfoContext(ctx, "deleted dynamic informer")
			}
		}
	}
}
