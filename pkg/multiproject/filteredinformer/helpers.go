package filteredinformer

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/ingress-gce/pkg/flags"
)

// isObjectInClusterSlice checks if an object belongs to a specific cluster slice.
func isObjectInClusterSlice(obj interface{}, clusterSliceName string) bool {
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	return metaObj.GetLabels()[flags.F.ClusterSliceNameLabelKey] == clusterSliceName
}

// clusterSliceFilteredList filters a list of objects by cluster slice name.
func clusterSliceFilteredList(items []interface{}, clusterSliceName string) []interface{} {
	var filtered []interface{}
	for _, item := range items {
		if isObjectInClusterSlice(item, clusterSliceName) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
