package filteredinformer

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
)

// isObjectInProviderConfig checks if an object belongs to a specific provider config.
func isObjectInProviderConfig(obj interface{}, providerConfigName string, allowMissing bool) bool {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return false
	}
	labels := metaObj.GetLabels()
	val, ok := labels[flags.F.ProviderConfigNameLabelKey]
	if !ok {
		return allowMissing
	}
	return val == providerConfigName
}

// providerConfigFilteredList filters a list of objects by provider config name.
func providerConfigFilteredList(items []interface{}, providerConfigName string, allowMissing bool) []interface{} {
	var filtered []interface{}
	for _, item := range items {
		if isObjectInProviderConfig(item, providerConfigName, allowMissing) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
