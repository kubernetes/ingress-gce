package namespacedinformer

import (
	"k8s.io/client-go/tools/cache"
)

// namespacedStore wraps a Store to provide namespace filtering.
type namespacedStore struct {
	cache.Store
	namespace string
}

func (n *namespacedStore) List() []interface{} {
	return namespaceFilteredList(n.Store.List(), n.namespace)
}

func (n *namespacedStore) ListKeys() []string {
	items := n.List()
	var keys []string
	for _, item := range items {
		if key, err := cache.MetaNamespaceKeyFunc(item); err == nil {
			if isObjectInNamespace(item, n.namespace) {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func (n *namespacedStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return nil, false, err
	}
	return n.GetByKey(key)
}

func (n *namespacedStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists, err = n.Store.GetByKey(key)
	if !exists || err != nil {
		return nil, exists, err
	}
	if isObjectInNamespace(item, n.namespace) {
		return item, true, nil
	}
	return nil, false, nil
}
