package controllers

import (
	krtlite "github.com/kalexmills/krt-lite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"strings"
	"sync"
)

// A DynamicInformer is an informer-backed collection which is created by the multitenancy controller at runtime. Used to
// listen for changes to resources mentioned by kind in TenantResource CRs.
type DynamicInformer struct {
	Collection krtlite.Collection[*unstructured.Unstructured]

	// gvrKey uniquely identifies this DynamicController by the GroupVersionResource of resources it watches.
	gvrKey GroupVersionResource

	stopCh    chan struct{}
	closeStop *sync.Once
}

// Key identifies a DynamicInformer by GroupVersionResource.
func (i *DynamicInformer) Key() string {
	return i.gvrKey.Key()
}

func (i *DynamicInformer) Stop() {
	i.closeStop.Do(func() {
		close(i.stopCh)
	})
}

// StopWith returns a CollectionOption which can be passed to new Collections, to ensure they stop along when this
// DynamicInformer is stopped.
func (i *DynamicInformer) StopWith() krtlite.CollectionOption {
	return krtlite.WithStop(i.stopCh)
}

// A GroupVersionResource wraps a [metav1.GroupVersionResource] to provide it with a key.
type GroupVersionResource struct {
	metav1.GroupVersionResource
}

// Key identifies a GroupVersionResource by (Group, Version, Resource)
func (g GroupVersionResource) Key() string {
	return strings.Join([]string{g.Group, g.Version, g.Resource}, ",")
}

func (g GroupVersionResource) GroupVersion() metav1.GroupVersion {
	return metav1.GroupVersion{Group: g.Group, Version: g.Version}
}
