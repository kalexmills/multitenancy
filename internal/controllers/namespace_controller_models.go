package controllers

import (
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// A TenantNamespace represents a namespace owned by a Tenant.
type TenantNamespace struct {
	Tenant    *v1alpha1.Tenant
	Namespace *corev1.Namespace
}

// Key identifies each TenantNamespace uniquely by name of Namespace and Tenant.
func (t TenantNamespace) Key() string {
	return t.Tenant.Name + "/" + t.Namespace.Name
}
