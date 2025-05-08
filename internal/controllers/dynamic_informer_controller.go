package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"log/slog"
	"maps"
	"slices"
)

type DynamicInformerController struct {
	client dynamic.Interface

	GVRs             krtlite.Collection[GroupVersionResource]
	DynamicInformers krtlite.StaticCollection[DynamicInformer]
}

func NewDynamicInformerController(
	ctx context.Context,
	dynamicClient dynamic.Interface,
	tenantNamespaces krtlite.Collection[TenantNamespace],
) *DynamicInformerController {
	res := &DynamicInformerController{
		client: dynamicClient,
	}

	opts := []krtlite.CollectionOption{
		krtlite.WithContext(ctx),
	}

	res.GVRs = krtlite.FlatMap(tenantNamespaces, res.mapToGVRs, opts...)
	res.GVRs.Register(res.dynamicCollectionHandler(ctx))

	return res
}

func (c *DynamicInformerController) mapToGVRs(ktx krtlite.Context, tns TenantNamespace) []GroupVersionResource {
	result := make(map[GroupVersionResource]struct{})

	for _, r := range tns.Resources {
		result[GroupVersionResource{r.Spec.Resource}] = struct{}{}
	}
	return slices.Collect(maps.Keys(result))
}

func (c *DynamicInformerController) dynamicCollectionHandler(ctx context.Context) func(krtlite.Event[GroupVersionResource]) {
	return func(ev krtlite.Event[GroupVersionResource]) {
		gvr := ev.Latest()

		coll := c.DynamicInformers.GetKey(gvr.Key())

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

			slog.InfoContext(ctx, "starting dynamic informer", "gvr", gvr.Key())

			inf := krtlite.NewDynamicInformer(c.client, schemaGVR,
				krtlite.WithFilterByLabel(tenantResourceLabel),
				krtlite.WithStop(stopCh))

			c.DynamicInformers.Update(DynamicInformer{
				Collection: inf,

				gvrKey: gvr,
				stopCh: stopCh,
			})

		case krtlite.EventUpdate:
			slog.ErrorContext(ctx, "error, GroupVersionResource was updated -- entire object is key", "event", ev)

		// shutdown the collection and remove it from the static collection.
		case krtlite.EventDelete:
			if coll != nil {
				coll := *coll
				coll.Stop()
				c.DynamicInformers.Delete(coll.Key())
			}
		}
	}
}
