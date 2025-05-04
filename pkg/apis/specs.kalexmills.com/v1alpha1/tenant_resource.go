package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+genclient:nonNamespaced
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TenantResource describes a Kubernetes resource that is copied into Tenant namespaces.
type TenantResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantSpec   `json:"spec"`
	Status TenantStatus `json:"status"`
}

// TenantResourceSpec is the spec for a TenantResource.
type TenantResourceSpec struct {
	// Spec holds the entire object for the resource to be replicated across namespaces.
	Spec map[string]string `json:"spec"`
}

// TenantResourceStatus is the status for a TenantResource.
type TenantResourceStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TenantResourceList is a list of TenantResource objects.
type TenantResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []TenantResource `json:"items"`
}
