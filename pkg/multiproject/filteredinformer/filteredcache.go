package filteredinformer

import (
	"k8s.io/client-go/tools/cache"
)

// providerConfigFilteredCache implements cache.Store and cache.Indexer with provider config filtering.
type providerConfigFilteredCache struct {
	cache.Indexer
	providerConfigName string
}

func (pc *providerConfigFilteredCache) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	items, err := pc.Indexer.ByIndex(indexName, indexedValue)
	if err != nil {
		return nil, err
	}
	return providerConfigFilteredList(items, pc.providerConfigName), nil
}

func (pc *providerConfigFilteredCache) Index(indexName string, obj interface{}) ([]interface{}, error) {
	items, err := pc.Indexer.Index(indexName, obj)
	if err != nil {
		return nil, err
	}
	return providerConfigFilteredList(items, pc.providerConfigName), nil
}

func (pc *providerConfigFilteredCache) List() []interface{} {
	return providerConfigFilteredList(pc.Indexer.List(), pc.providerConfigName)
}

func (pc *providerConfigFilteredCache) ListKeys() []string {
	items := pc.List()
	var keys []string
	for _, item := range items {
		if key, err := cache.MetaNamespaceKeyFunc(item); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

func (pc *providerConfigFilteredCache) Get(obj interface{}) (item interface{}, exists bool, err error) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return nil, false, err
	}
	return pc.GetByKey(key)
}

func (pc *providerConfigFilteredCache) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists, err = pc.Indexer.GetByKey(key)
	if !exists || err != nil {
		return nil, exists, err
	}
	if isObjectInProviderConfig(item, pc.providerConfigName) {
		return item, true, nil
	}
	return nil, false, nil
}
