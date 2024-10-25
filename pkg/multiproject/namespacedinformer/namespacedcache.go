package namespacedinformer

import (
	"k8s.io/client-go/tools/cache"
)

// namespacedCache implements cache.Store and cache.Indexer with namespace filtering.
type namespacedCache struct {
	cache.Indexer
	namespace string
}

func (n *namespacedCache) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	items, err := n.Indexer.ByIndex(indexName, indexedValue)
	if err != nil {
		return nil, err
	}
	return namespaceFilteredList(items, n.namespace), nil
}

func (n *namespacedCache) Index(indexName string, obj interface{}) ([]interface{}, error) {
	items, err := n.Indexer.Index(indexName, obj)
	if err != nil {
		return nil, err
	}
	return namespaceFilteredList(items, n.namespace), nil
}

func (n *namespacedCache) List() []interface{} {
	return namespaceFilteredList(n.Indexer.List(), n.namespace)
}

func (n *namespacedCache) ListKeys() []string {
	items := n.List()
	var keys []string
	for _, item := range items {
		if key, err := cache.MetaNamespaceKeyFunc(item); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

func (n *namespacedCache) Get(obj interface{}) (item interface{}, exists bool, err error) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return nil, false, err
	}
	return n.GetByKey(key)
}

func (n *namespacedCache) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists, err = n.Indexer.GetByKey(key)
	if !exists || err != nil {
		return nil, exists, err
	}
	if isObjectInNamespace(item, n.namespace) {
		return item, true, nil
	}
	return nil, false, nil
}
