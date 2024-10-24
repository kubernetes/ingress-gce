package namespacedinformer

import (
	"k8s.io/apimachinery/pkg/api/meta"
)

// isObjectInNamespace checks if an object belongs to a specific namespace.
func isObjectInNamespace(obj interface{}, namespace string) bool {
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	return metaObj.GetNamespace() == namespace
}

// namespaceFilteredList filters a list of objects by namespace.
func namespaceFilteredList(items []interface{}, namespace string) []interface{} {
	var filtered []interface{}
	for _, item := range items {
		if isObjectInNamespace(item, namespace) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
