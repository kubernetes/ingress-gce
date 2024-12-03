package filteredinformer

import (
	"k8s.io/client-go/tools/cache"
)

// clusterSliceFilteredCache implements cache.Store and cache.Indexer with cluster slice filtering.
type clusterSliceFilteredCache struct {
	cache.Indexer
	clusterSliceName string
}

func (pc *clusterSliceFilteredCache) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	items, err := pc.Indexer.ByIndex(indexName, indexedValue)
	if err != nil {
		return nil, err
	}
	return clusterSliceFilteredList(items, pc.clusterSliceName), nil
}

func (pc *clusterSliceFilteredCache) Index(indexName string, obj interface{}) ([]interface{}, error) {
	items, err := pc.Indexer.Index(indexName, obj)
	if err != nil {
		return nil, err
	}
	return clusterSliceFilteredList(items, pc.clusterSliceName), nil
}

func (pc *clusterSliceFilteredCache) List() []interface{} {
	return clusterSliceFilteredList(pc.Indexer.List(), pc.clusterSliceName)
}

func (pc *clusterSliceFilteredCache) ListKeys() []string {
	items := pc.List()
	var keys []string
	for _, item := range items {
		if key, err := cache.MetaNamespaceKeyFunc(item); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

func (pc *clusterSliceFilteredCache) Get(obj interface{}) (item interface{}, exists bool, err error) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return nil, false, err
	}
	return pc.GetByKey(key)
}

func (pc *clusterSliceFilteredCache) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists, err = pc.Indexer.GetByKey(key)
	if !exists || err != nil {
		return nil, exists, err
	}
	if isObjectInClusterSlice(item, pc.clusterSliceName) {
		return item, true, nil
	}
	return nil, false, nil
}
