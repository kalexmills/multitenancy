package controllers

import (
	krtlite "github.com/kalexmills/krt-lite"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
)

// A DesiredTenantResource represents the desired state of a TenantResource in a namespace.
type DesiredTenantResource struct {
	TenantName           string
	Namespace            string
	ResourceName         string
	Object               *unstructured.Unstructured
	GroupVersionResource schema.GroupVersionResource
}

func (t DesiredTenantResource) Key() string {
	return strings.Join([]string{t.TenantName, t.ResourceName, t.Namespace}, "/")
}

// TenantResource represents both an DesiredTenantResource and ActualTenantResource whoch are the same key.
type TenantResource = krtlite.Joined[DesiredTenantResource, ActualTenantResource]

// ActualTenantResource represents the actual state of a TenantResource from the cluster.
type ActualTenantResource struct {
	Object *unstructured.Unstructured
}

func (r ActualTenantResource) Key() string {
	return strings.Join([]string{
		r.Object.GetLabels()[tenantLabel],
		r.Object.GetLabels()[tenantResourceLabel],
		r.Object.GetNamespace(),
	}, "/")
}
