package controllers

import (
	krtlite "github.com/kalexmills/krt-lite"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
)

// A DesiredTenantResource represents the desired state of a TenantResource in a particular.
type DesiredTenantResource struct {
	TenantName           string
	Namespace            string
	ResourceName         string
	Object               *unstructured.Unstructured
	GroupVersionResource schema.GroupVersionResource
}

// Key identifies each DesiredTenantResource by (TenantName, Namespace, GroupVersionKind, ResourceName).
func (t DesiredTenantResource) Key() string {
	return strings.Join([]string{t.TenantName, t.Namespace, t.Object.GroupVersionKind().String(), t.ResourceName}, "/")
}

// TenantResource is a pair of DesiredTenantResource and ActualTenantResource, with matching keys.
type TenantResource = krtlite.Joined[DesiredTenantResource, ActualTenantResource]

// ActualTenantResource represents the actual state of a TenantResource from the cluster.
type ActualTenantResource struct {
	Object *unstructured.Unstructured
}

// Key identifies each ActualTenantResource by (TenantName, Namespace, GroupVersionKind, ResourceName).
// TenantName and ResourceName are each fetched from labels on the resource.
func (r ActualTenantResource) Key() string {
	return strings.Join([]string{
		r.Object.GetLabels()[tenantLabel],
		r.Object.GetNamespace(),
		r.Object.GroupVersionKind().String(),
		r.Object.GetLabels()[tenantResourceLabel],
	}, "/")
}
