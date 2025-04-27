package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Tenant specifies a collection of namespaces which comprise a tenant.
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec"`
	Status TenantStatus `json:"status"`
}

// TenantSpec is the spec for a Tenant
type TenantSpec struct {
	Namespaces []string `json:"namespaces"`

	// Labels are added to every namespace created
	Labels map[string]string `json:"labels,omitempty"`

	// DefaultQuota specifies the resource quota for each namespace in the tenant.
	DefaultQuota *corev1.ResourceQuotaSpec `json:"defaultQuota,omitempty"`
}

// TenantStatus is the status for a Tenant.
type TenantStatus struct {
	// NamespaceStatuses maps from namespaces to their current status.
	NamespaceStatuses map[string]string `json:"namespaceStatuses"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TenantList is a list of Tenant resources.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Tenant `json:"items"`
}
