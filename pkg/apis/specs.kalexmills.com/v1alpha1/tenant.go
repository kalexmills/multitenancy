package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+genclient
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status

// Tenant specifies a collection of namespaces which comprise a tenant.
type Tenant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec"`
	Status TenantStatus `json:"status"`
}

// TenantSpec is the spec for a Tenant
type TenantSpec struct {
	//+required
	Namespaces []string `json:"namespaces"`

	// Labels are added to every namespace created
	Labels map[string]string `json:"labels,omitempty"`

	// Resources is a list to named TenantResources which are kept up-to-date in Tenant namespaces.
	Resources []string `json:"resources"`
}

// TenantStatus is the status for a Tenant.
type TenantStatus struct {
	// NamespaceStatuses maps from namespaces to their current status.
	NamespaceStatuses map[string]string `json:"namespaceStatuses"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TenantList is a list of Tenant objects.
type TenantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Tenant `json:"items"`
}
