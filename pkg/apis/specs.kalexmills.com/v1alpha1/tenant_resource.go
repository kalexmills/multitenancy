package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

//+genclient:nonNamespaced
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TenantResource describes a Kubernetes resource that is copied into Tenant namespaces and kept in-sync.
type TenantResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantResourceSpec   `json:"spec"`
	Status TenantResourceStatus `json:"status"`
}

func (t *TenantResource) SchemaGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    t.Spec.Resource.Group,
		Version:  t.Spec.Resource.Version,
		Resource: t.Spec.Resource.Resource,
	}
}

// TenantResourceSpec is the spec for a TenantResource.
type TenantResourceSpec struct {
	// Resource uniquely identifies the resource to create.
	Resource metav1.GroupVersionResource `json:"resource"`

	// Manifest is the entire YAML spec to copy into each namespace for this resource.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Manifest runtime.RawExtension `json:"manifest"`
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
