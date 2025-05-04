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

// +kubebuilder:rbac:groups=specs.kalexmills.com,resources=tenants;tenantresources,verbs=get;list;watch;update

const tenantQuotaName = "tenant-quota"
const tenantLabel = "multitenancy.kalexmills.com/tenant" // TODO: move to separate package

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

func (s NamespaceStatus) ResourceName() string {
	return s.Tenant + "/" + s.Namespace
}

type TenantNamespace struct {
	Namespace *corev1.Namespace
	Tenant    *v1alpha1.Tenant
	Resources []*v1alpha1.TenantResource
}

func (t TenantNamespace) ResourceName() string {
	return t.Tenant.Name + "/" + t.Namespace.Name
}

func (t TenantNamespace) NewStatus(status string) NamespaceStatus {
	return NamespaceStatus{
		Namespace: t.Namespace.Name,
		Tenant:    t.Tenant.Name,
		Status:    status,
	}
}

type TenantController struct {
	client client.WithWatch

	Tenants           krtlite.Collection[*v1alpha1.Tenant]
	TenantResources   krtlite.Collection[*v1alpha1.TenantResource]
	TenantNamespaces  krtlite.Collection[TenantNamespace]
	NamespaceStatuses krtlite.StaticCollection[NamespaceStatus]
}

func NewTenantController(ctx context.Context, c client.WithWatch) *TenantController {
	tc := &TenantController{
		client: c,
	}

	tc.Tenants = krtlite.NewInformer[*v1alpha1.Tenant, v1alpha1.TenantList](ctx, c,
		krtlite.WithContext(ctx))
	tc.TenantResources = krtlite.NewInformer[*v1alpha1.TenantResource, v1alpha1.TenantResourceList](ctx, c,
		krtlite.WithContext(ctx))
	tc.TenantNamespaces = krtlite.FlatMap[*v1alpha1.Tenant, TenantNamespace](tc.Tenants, tc.toNamespaces,
		krtlite.WithContext(ctx))
	tc.TenantNamespaces.Register(tc.tenantNamespaceHandler(ctx))

	return tc
}

func (c *TenantController) toNamespaces(ktx krtlite.Context, tenant *v1alpha1.Tenant) []TenantNamespace {
	lbls := labels.Merge(tenant.Labels, map[string]string{tenantLabel: tenant.Name})

	resources := krtlite.Fetch(ktx, c.TenantResources, krtlite.MatchNames(tenant.Spec.Resources...))

	var result []TenantNamespace
	for _, nsName := range tenant.Spec.Namespaces {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: nsName,

				Labels: lbls,
			},
		}
		result = append(result, TenantNamespace{
			Namespace: ns,
			Tenant:    tenant,
			Resources: resources,
		})
	}

	return result
}

func (c *TenantController) tenantNamespaceHandler(ctx context.Context) func(krtlite.Event[TenantNamespace]) {
	return func(ev krtlite.Event[TenantNamespace]) {
		// err := c.reconcileNamespace(ctx, ev)

	}
}

func (c *TenantController) reconcileNamespace(ctx context.Context, ev krtlite.Event[TenantNamespace]) (exists bool, err error) {
	var (
		tns    = ev.Latest()
		ns     = tns.Namespace
		status = tns.NewStatus("")
	)

	switch ev.Type {
	case krtlite.EventAdd:
		err = c.client.Create(ctx, ns)
		if err != nil {
			slog.ErrorContext(ctx, "error creating ns",
				slog.String("err", err.Error()),
				slog.String("ns", ns.Name))
			status.Status = namespaceStatusError
		} else {
			status.Status = namespaceStatusPending
		}

		exists = true

	case krtlite.EventDelete:
		err = c.client.Delete(ctx, ns)
		if err != nil {
			if !errors.IsNotFound(err) {
				exists = true
			}
			slog.ErrorContext(ctx, "error deleting ns",
				slog.String("err", err.Error()),
				slog.String("ns", ns.Name))

			status.Status = namespaceStatusError
		} else {
			status.Status = namespaceStatusDeleting
		}

	case krtlite.EventUpdate:
		exists = true

		// the only changes we need to make are to namespace labels.
		if labels.Equals(ns.Labels, (*ev.Old).Namespace.Labels) {
			return exists, err
		}

		err = c.client.Update(ctx, ns)
		if err == nil {
			if errors.IsNotFound(err) {
				exists = false
			}
			slog.ErrorContext(ctx, "error updating ns",
				slog.String("err", err.Error()),
				slog.String("ns", ns.Name))
			status.Status = namespaceStatusError
		}
	}
	c.NamespaceStatuses.Update(status)

	return exists, err
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
				slog.ErrorContext(ctx, "error creating object", slogArgs(err, obj)...)
			}
		case krtlite.EventUpdate:
			err := cli.Update(ctx, obj)
			if err != nil {
				slog.ErrorContext(ctx, "error updating object", slogArgs(err, obj)...)
			}
		case krtlite.EventDelete:
			err := cli.Delete(ctx, obj)
			if err != nil {
				slog.ErrorContext(ctx, "error deleting object", slogArgs(err, obj)...)
			}
		}
	}
}
