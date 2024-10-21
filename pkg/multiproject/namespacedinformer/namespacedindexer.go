package namespacedinformer

import (
	"k8s.io/client-go/tools/cache"
)

// namespacedIndexer wraps an Indexer to provide namespace filtering.
type namespacedIndexer struct {
	cache.Indexer
	namespace string
}

func (n *namespacedIndexer) List() []interface{} {
	return namespaceFilteredList(n.Indexer.List(), n.namespace)
}

func (n *namespacedIndexer) ByIndex(indexName, indexKey string) ([]interface{}, error) {
	items, err := n.Indexer.ByIndex(indexName, indexKey)
	if err != nil {
		return nil, err
	}
	return namespaceFilteredList(items, n.namespace), nil
}

func (n *namespacedIndexer) Index(indexName string, obj interface{}) ([]interface{}, error) {
	items, err := n.Indexer.Index(indexName, obj)
	if err != nil {
		return nil, err
	}
	return namespaceFilteredList(items, n.namespace), nil
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
