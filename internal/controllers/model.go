package controllers

import "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

func cleanObj(obj *unstructured.Unstructured) *unstructured.Unstructured {
	res := obj.DeepCopy()
	unstructured.RemoveNestedField(res.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(res.Object, "metadata", "generation")
	unstructured.RemoveNestedField(res.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(res.Object, "status")
	return res
}
