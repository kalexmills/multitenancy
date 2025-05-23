package controllers

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

// cleanObj removes unneeded fields from an unstructured object.
func cleanObj(obj *unstructured.Unstructured) *unstructured.Unstructured {
	res := obj.DeepCopy()
	// removed to prevent write conflicts.
	unstructured.RemoveNestedField(res.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(res.Object, "metadata", "generation")

	// removed for better in-memory storage.
	unstructured.RemoveNestedField(res.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(res.Object, "status")
	return res
}
