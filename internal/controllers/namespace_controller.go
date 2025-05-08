package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"log/slog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type TenantNamespace struct {
	Tenant    *v1alpha1.Tenant
	Namespace *corev1.Namespace
	Resources []*v1alpha1.TenantResource
}

func (t TenantNamespace) Key() string {
	return t.Tenant.Name + "/" + t.Namespace.Name
}

// NamespaceController creates and reconciles namespaces. Serves as the home of
type NamespaceController struct {
	TenantNamespaces krtlite.Collection[TenantNamespace] // TODO: hide these behind a getter

	client client.Client
}

func NewNamespaceController(
	ctx context.Context,
	client client.Client,
	namespaces krtlite.Collection[*corev1.Namespace],
	tenants krtlite.Collection[*v1alpha1.Tenant],
) *NamespaceController {
	res := &NamespaceController{
		client: client,
	}

	opts := []krtlite.CollectionOption{
		krtlite.WithContext(ctx),
	}

	// Track TenantNamespace groupings and create namespaces
	res.TenantNamespaces = krtlite.FlatMap(tenants, res.tenantToNamespaces(namespaces), opts...)
	res.TenantNamespaces.Register(res.reconcileNamespaces(ctx))

	return res
}

func (c *NamespaceController) tenantToNamespaces(
	namespaces krtlite.Collection[*corev1.Namespace],
) krtlite.FlatMapper[*v1alpha1.Tenant, TenantNamespace] {
	return func(ktx krtlite.Context, tenant *v1alpha1.Tenant) []TenantNamespace {
		namespaces := krtlite.Fetch(ktx, namespaces, krtlite.MatchNames(tenant.Spec.Namespaces...))

		byName := make(map[string]*corev1.Namespace)
		for _, ns := range namespaces {
			byName[ns.Name] = ns
		}

		var result []TenantNamespace
		for _, nsName := range tenant.Spec.Namespaces {
			ns, ok := byName[nsName]
			if !ok {
				ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}}
			}

			ns.Labels = labels.Merge(tenant.Spec.Labels, map[string]string{tenantLabel: tenant.Name})

			result = append(result, TenantNamespace{
				Namespace: ns,
				Tenant:    tenant,
			})
		}

		return result
	}
}

// reconcileNamespaces is responsible for keeping tenant namespaces up-to-date.
func (c *NamespaceController) reconcileNamespaces(ctx context.Context) func(krtlite.Event[TenantNamespace]) {
	return func(ev krtlite.Event[TenantNamespace]) {
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
