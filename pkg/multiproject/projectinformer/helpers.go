package projectinformer

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/ingress-gce/pkg/flags"
)

// isObjectInProject checks if an object belongs to a specific project.
func isObjectInProject(obj interface{}, projectName string) bool {
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	return metaObj.GetLabels()[flags.F.MultiProjectCRDProjectNameLabel] == projectName
}

// projectNameFilteredList filters a list of objects by project name.
func projectNameFilteredList(items []interface{}, projectName string) []interface{} {
	var filtered []interface{}
	for _, item := range items {
		if isObjectInProject(item, projectName) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
