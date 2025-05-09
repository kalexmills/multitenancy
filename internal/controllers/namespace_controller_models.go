package controllers

import (
	"github.com/kalexmills/multitenancy/pkg/apis/specs.kalexmills.com/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type TenantNamespace struct {
	Tenant    *v1alpha1.Tenant
	Namespace *corev1.Namespace
}

func (t TenantNamespace) Key() string {
	return t.Tenant.Name + "/" + t.Namespace.Name
}
