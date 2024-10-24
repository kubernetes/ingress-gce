package namespacedinformer

import (
	"k8s.io/client-go/tools/cache"
)

// namespacedIndexer wraps an Indexer to provide namespace filtering.
type namespacedIndexer struct {
	cache.Indexer
	namespace string
}

func (n *namespacedIndexer) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	items, err := n.Indexer.ByIndex(indexName, indexedValue)
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
