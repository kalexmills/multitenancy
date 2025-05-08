package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/dynamic"
	"log/slog"
	"reflect"
)

const tenantLabel = "multitenancy/tenant"
const tenantResourceLabel = "multitenancy/tenant-resource"

// TenantResourceController creates tenant resources.
type TenantResourceController struct {
	client dynamic.Interface

	DynamicResources krtlite.Collection[DesiredTenantResource]
}

func NewDynamicResourceController(
	ctx context.Context,
	client dynamic.Interface,
	tenantNamespaces krtlite.Collection[TenantNamespace],
	dynamicInformers krtlite.Collection[DynamicInformer],
) *TenantResourceController {
	res := &TenantResourceController{
		client: client,
	}

	opts := []krtlite.CollectionOption{krtlite.WithContext(ctx)}

	res.DynamicResources = krtlite.FlatMap(tenantNamespaces, res.namespaceToDesiredResource, opts...) // TODO: should this also return the collection?
	dynamicInformers.Register(res.joinAndRegister(ctx))

	return res
}

// namespaceToDesiredResource maps a TenantNamespace to a list of its DesiredTenantResources.
func (c *TenantResourceController) namespaceToDesiredResource(ktx krtlite.Context, tns TenantNamespace) []DesiredTenantResource {
	var result []DesiredTenantResource

	for _, r := range tns.Resources {
		ext := r.Spec.Manifest

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

		// set required labels for tracking
		labels[tenantResourceLabel] = r.Name
		labels[tenantLabel] = tns.Tenant.Name
		obj.SetLabels(labels)

		result = append(result, DesiredTenantResource{
			TenantName:           tns.Tenant.Name,
			Namespace:            tns.Namespace.Name,
			ResourceName:         r.Name,
			GroupVersionResource: r.SchemaGVR(),
			Object:               obj,
		})
	}
	return result
}

// joinAndRegister joins ActualResources from newly created DynamicInformers with DynamicResources managed by this
// controller.
func (c *TenantResourceController) joinAndRegister(ctx context.Context) func(krtlite.Event[DynamicInformer]) {
	return func(ev krtlite.Event[DynamicInformer]) {
		if ev.Type != krtlite.EventAdd {
			return
		}

		dynInf := ev.Latest()

		// map resources from the informer to align the keyspaces.
		actualResources := krtlite.Map(dynInf.Collection, c.toTenantResource,
			dynInf.StopWith())

		// create a new Join collection which will be stopped along with the dynamic informer. Joining these two collections
		// ensures events which modify tenant resources trigger a new reconciliation.
		joined := krtlite.Join(c.DynamicResources, actualResources, krtlite.LeftJoin,
			dynInf.StopWith())
		joined.Register(c.reconcileTenantResources(ctx))
	}
}

// TODO: need WithConversion upstream for simple mappings like this which don't deserve their own queue.
func (c *TenantResourceController) toTenantResource(ktx krtlite.Context, i *unstructured.Unstructured) *ActualTenantResource {
	return &ActualTenantResource{Object: i}
}

// reconcileTenantResources reconciles tenant resources.
func (c *TenantResourceController) reconcileTenantResources(ctx context.Context) func(krtlite.Event[TenantResource]) {
	return func(ev krtlite.Event[TenantResource]) {
		var (
			latestNR      = ev.Latest().Left
			dynamicClient = c.client.Resource(latestNR.GroupVersionResource).Namespace(latestNR.Namespace)

			desiredObj = ev.Latest().Left.Object // LeftJoined; so Left will never be nil
			actualObj  *unstructured.Unstructured
		)

		switch ev.Type {
		case krtlite.EventAdd:
			_, err := dynamicClient.Create(ctx, desiredObj, metav1.CreateOptions{})
			if err != nil {
				if !errors.IsAlreadyExists(err) {
					slog.ErrorContext(ctx, "error creating object", "error", err)
				}

				// overwrite whatever is there.
				if _, err := dynamicClient.Update(ctx, desiredObj, metav1.UpdateOptions{}); err != nil {
					slog.ErrorContext(ctx, "error updating object during create", "error", err)
				}
				return
			}

		case krtlite.EventUpdate:
			if ev.New.Right != nil {
				actualObj = ev.New.Right.Object

				if reflect.DeepEqual(cleanObj(actualObj), cleanObj(desiredObj)) {
					return
				}
			}

			_, err := dynamicClient.Update(ctx, desiredObj, metav1.UpdateOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					slog.ErrorContext(ctx, "error updating object", "error", err)
					return
				}
				_, err = dynamicClient.Create(ctx, desiredObj, metav1.CreateOptions{})
				if err != nil {
					slog.ErrorContext(ctx, "error creating object during update", "error", err)
				}
			}

		case krtlite.EventDelete:
			err := dynamicClient.Delete(ctx, desiredObj.GetName(), metav1.DeleteOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					slog.ErrorContext(ctx, "error deleting object", "error", err)
				}
			}
		}
	}
}
