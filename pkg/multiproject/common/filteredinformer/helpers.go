package filteredinformer

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/ingress-gce/pkg/flags"
)

// isObjectInProviderConfig checks if an object belongs to a specific provider config.
func isObjectInProviderConfig(obj interface{}, providerConfigName string) bool {
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	return metaObj.GetLabels()[flags.F.ProviderConfigNameLabelKey] == providerConfigName
}

// providerConfigFilteredList filters a list of objects by provider config name.
func providerConfigFilteredList(items []interface{}, providerConfigName string) []interface{} {
	var filtered []interface{}
	for _, item := range items {
		if isObjectInProviderConfig(item, providerConfigName) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
