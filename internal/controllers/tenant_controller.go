package controllers

import (
	"context"
	krtlite "github.com/kalexmills/krt-lite/pkg"
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"log/slog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
	TenantNamespaces  krtlite.Collection[TenantNamespace]
	NamespaceStatuses krtlite.StaticCollection[NamespaceStatus]
	ResourceQuotas    krtlite.Collection[*corev1.ResourceQuota]
}

func NewTenantController(ctx context.Context, c client.WithWatch) *TenantController {
	tc := &TenantController{
		client: c,
	}

	tc.Tenants = krtlite.NewInformer[*v1alpha1.Tenant, v1alpha1.TenantList](ctx, c)
	tc.TenantNamespaces = krtlite.FlatMap[*v1alpha1.Tenant, TenantNamespace](tc.Tenants, tc.toNamespaces)
	tc.TenantNamespaces.Register(tc.namespaceHandler(ctx))
	tc.NamespaceStatuses.Register(tc.statusHandler(ctx))
	tc.ResourceQuotas = krtlite.Map[TenantNamespace, *corev1.ResourceQuota](tc.TenantNamespaces, tc.toResourceQuota)
	tc.ResourceQuotas.Register(tc.resourceQuotaHandler(ctx))

	return tc
}

func (c *TenantController) toNamespaces(ktx krtlite.Context, tenant *v1alpha1.Tenant) []TenantNamespace {
	lbls := labels.Merge(tenant.Labels, map[string]string{tenantLabel: tenant.Name})

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
		})
	}

	return result
}

func (c *TenantController) toResourceQuota(ktx krtlite.Context, tns TenantNamespace) **corev1.ResourceQuota {
	if tns.Tenant.Spec.DefaultQuota == nil {
		c.NamespaceStatuses.Update(tns.NewStatus(namespaceStatusReady))
		return nil
	}
	result := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tenantQuotaName,
			Namespace: tns.Namespace.Name,
		},
		Spec: *tns.Tenant.Spec.DefaultQuota,
	}
	return &result
}

func (c *TenantController) namespaceHandler(ctx context.Context) func(krtlite.Event[TenantNamespace]) {
	return func(ev krtlite.Event[TenantNamespace]) {
		tns := ev.Latest()
		ns := tns.Namespace

		status := tns.NewStatus("")

		switch ev.Event {
		case krtlite.EventAdd:
			err := c.client.Create(ctx, ns)
			if err != nil {
				slog.ErrorContext(ctx, "error creating ns",
					slog.String("err", err.Error()),
					slog.String("ns", ns.Name))
				status.Status = namespaceStatusError
			} else {
				status.Status = namespaceStatusPending
			}
		case krtlite.EventDelete:
			err := c.client.Delete(ctx, ns)
			if err != nil {
				slog.ErrorContext(ctx, "error deleting ns",
					slog.String("err", err.Error()),
					slog.String("ns", ns.Name))

				status.Status = namespaceStatusError
			} else {
				status.Status = namespaceStatusDeleting
			}

		case krtlite.EventUpdate:
			if labels.Equals(ns.Labels, (*ev.Old).Namespace.Labels) {
				return
			}

			err := c.client.Update(ctx, ns)
			if err != nil {
				slog.ErrorContext(ctx, "error updating ns",
					slog.String("err", err.Error()),
					slog.String("ns", ns.Name))
				status.Status = namespaceStatusError
			} else {
				status.Status = namespaceStatusPending // TODO: transition from pending >> ready ??
			}
		}
		c.NamespaceStatuses.Update(status)
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

		switch ev.Event {
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

func (c *TenantController) resourceQuotaHandler(ctx context.Context) func(krtlite.Event[*corev1.ResourceQuota]) {
	return func(ev krtlite.Event[*corev1.ResourceQuota]) {
		quota := ev.Latest()

		switch ev.Event {
		case krtlite.EventAdd:
			err := c.client.Create(ctx, quota)
			if err != nil {
				slog.ErrorContext(ctx, "error creating tenant quota", slog.String("err", err.Error()), slog.String("namespace", quota.Namespace))
			}
		case krtlite.EventUpdate:
			err := c.client.Update(ctx, quota)
			if err != nil {
				slog.ErrorContext(ctx, "error updating tenant quota", slog.String("err", err.Error()), slog.String("namespace", quota.Namespace))
			}
		case krtlite.EventDelete:
			err := c.client.Delete(ctx, quota)
			if err != nil {
				slog.ErrorContext(ctx, "error deleting tenant quota", slog.String("err", err.Error()), slog.String("namespace", quota.Namespace))
			}
		}
	}
}
