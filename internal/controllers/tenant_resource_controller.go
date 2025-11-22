package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
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

// TenantResourceController creates tenant resources. Owns the DesiredTenantResource collection.
type TenantResourceController struct {
	client          dynamic.Interface
	tenantResources krtlite.Collection[*v1alpha1.TenantResource]

	// collections owned by this controller.
	desiredTenantResources krtlite.Collection[DesiredTenantResource]
}

func NewTenantResourceController(
	ctx context.Context,
	client dynamic.Interface,
	tenantResources krtlite.Collection[*v1alpha1.TenantResource],
	tenantNamespaces krtlite.Collection[TenantNamespace],
	dynamicInformers krtlite.Collection[*DynamicInformer],
) *TenantResourceController {
	res := &TenantResourceController{
		client:          client,
		tenantResources: tenantResources,
	}

	opts := []krtlite.CollectionOption{
		krtlite.WithContext(ctx),
	}

	res.desiredTenantResources = krtlite.FlatMap(tenantNamespaces, res.namespaceToDesiredResource, opts...)

	dynamicInformers.Register(res.joinAndRegister(ctx))

	return res
}

// DesiredTenantResources returns a collection of DesiredTenantResource, which is kept in sync with TenantResource CRs
// in k8s.
func (c *TenantResourceController) DesiredTenantResources() krtlite.Collection[DesiredTenantResource] {
	return c.desiredTenantResources
}

// namespaceToDesiredResource maps a TenantNamespace to a list of its DesiredTenantResources.
func (c *TenantResourceController) namespaceToDesiredResource(ktx krtlite.Context, tns TenantNamespace) []DesiredTenantResource {
	var result []DesiredTenantResource

	// Fetch returns all TenantResources matching the resources specified in the Tenant. By passing ktx we create a
	// dependency on the tenantResources collection. Any change to resources returned from this fetch operation will
	// re-trigger this Mapper and could result in sending an Update or Delete event downstream.
	resources := krtlite.Fetch(ktx, c.tenantResources, krtlite.MatchNames(tns.Tenant.Spec.Resources...))

	for _, r := range resources {
		// fetch the desired manifest and store it in the DesiredTenantResource.
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

		// Set labels used to reconstruct the collection key for actual resources. In production, this controller would need
		// to be deployed along with a ValidatingWebhook that prevents updates to these fields by other users.
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

// joinAndRegister listens for new DynamicInformers. When one is created, it creates a new joined collection, which
// merges events from two event streams: 1) actual resource state changes, 2) desired resource state changes. These two
// event streams are joined based on a common key. The resulting collection will process an event if either desired or
// actual state is changed.
//
// For instance, consider a Secret foo created by a TenantResource. Changes to the TenantResource manifest reflect
// changes in the desired state of foo. If the TenantResource manifest is changed to add a new label to the secret,
// an update event will be reflected in the joined collection. Changes to the Secret reflect changes in the actual state
// of foo. If the secret is deleted, an event will be reflected in the joined collection. A reconciler listening to the
// joined collection can act on any change to the desired or actual state of the resource.
func (c *TenantResourceController) joinAndRegister(ctx context.Context) func(krtlite.Event[*DynamicInformer]) {
	return func(ev krtlite.Event[*DynamicInformer]) {
		if ev.Type != krtlite.EventAdd {
			return
		}

		dynInf := ev.Latest()

		slog.InfoContext(ctx, "starting informer", "gvr", dynInf.Key())

		// Map resources from the informer to align the keyspaces.
		actualResources := krtlite.Map(dynInf.Collection, c.toTenantResource,
			// Passing StopWith ensures that this collection is stopped when the DynamicInformer is stopped.
			dynInf.StopWith())

		// Create a new Join collection. Performing a LeftJoin ensures that the Desired resource is alwys present in the
		// resulting object.
		joined := krtlite.Join(c.desiredTenantResources, actualResources, krtlite.LeftJoin,
			dynInf.StopWith()) // stop this collection when the DynamicInformer is stopped.
		joined.Register(c.reconcileTenantResources(ctx))
	}
}

// TODO: need WithConversion upstream for simple mappings like this which don't deserve their own queue.
func (c *TenantResourceController) toTenantResource(ktx krtlite.Context, i *unstructured.Unstructured) *ActualTenantResource {
	return &ActualTenantResource{Object: i}
}

// reconcileTenantResources ensures the state of TenantResources are kept up-to-date with the TenantResource definition.
func (c *TenantResourceController) reconcileTenantResources(ctx context.Context) func(krtlite.Event[TenantResource]) {
	return func(ev krtlite.Event[TenantResource]) {
		latestNR := ev.Latest().Left

		l := slog.With("gvr", latestNR.GroupVersionResource.String(),
			"namespace", latestNR.Namespace,
			"resourceName", latestNR.ResourceName)

		dynamicClient := c.client.Resource(latestNR.GroupVersionResource).Namespace(latestNR.Namespace)

		// latestNR is never nil since joinAndRegister performs a LeftJoin.
		desiredObj := latestNR.Object

		switch ev.Type {

		// Add events are only fired when the desired state is created, since this controller is a LeftJoined collection.
		case krtlite.EventAdd:

			// create the object in the cluster -- or replace it, if we didn't clean up.
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
			l.InfoContext(ctx, "resource created")

		// Update events for a LeftJoin are received anytime the actual or the desired state has changed.
		case krtlite.EventUpdate:

			// if actual state exists -- check to see if an update is required
			if ev.New.Right != nil {
				actualObj := ev.New.Right.Object

				// compare objects ignoring status, resourceVersion, generation, and managedFields.
				if reflect.DeepEqual(cleanObj(actualObj), cleanObj(desiredObj)) {
					l.InfoContext(ctx, "update suppressed -- no substantial modification was found")
					return
				}
			}

			_, err := dynamicClient.Update(ctx, desiredObj, metav1.UpdateOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					l.ErrorContext(ctx, "error updating object", "error", err)
					return
				}
				_, err = dynamicClient.Create(ctx, desiredObj, metav1.CreateOptions{})
				if err != nil {
					l.ErrorContext(ctx, "error creating object during update", "error", err)
				}
			}

			l.InfoContext(ctx, "resource updated")

		// Delete events for a LeftJoin are only received when the desired state has been removed.
		case krtlite.EventDelete:

			// remove the actual object from the cluster.
			err := dynamicClient.Delete(ctx, desiredObj.GetName(), metav1.DeleteOptions{})
			if err != nil {
				if !errors.IsNotFound(err) {
					l.ErrorContext(ctx, "error deleting object", "error", err)
				}
				l.InfoContext(ctx, "resource already deleted")
			} else {
				l.InfoContext(ctx, "resource deleted")
			}
		}
	}
}
