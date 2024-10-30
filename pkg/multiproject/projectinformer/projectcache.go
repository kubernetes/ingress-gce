package projectinformer

import (
	"k8s.io/client-go/tools/cache"
)

// projectCache implements cache.Store and cache.Indexer with project filtering.
type projectCache struct {
	cache.Indexer
	projectName string
}

func (pc *projectCache) ByIndex(indexName, indexedValue string) ([]interface{}, error) {
	items, err := pc.Indexer.ByIndex(indexName, indexedValue)
	if err != nil {
		return nil, err
	}
	return projectNameFilteredList(items, pc.projectName), nil
}

func (pc *projectCache) Index(indexName string, obj interface{}) ([]interface{}, error) {
	items, err := pc.Indexer.Index(indexName, obj)
	if err != nil {
		return nil, err
	}
	return projectNameFilteredList(items, pc.projectName), nil
}

func (pc *projectCache) List() []interface{} {
	return projectNameFilteredList(pc.Indexer.List(), pc.projectName)
}

func (pc *projectCache) ListKeys() []string {
	items := pc.List()
	var keys []string
	for _, item := range items {
		if key, err := cache.MetaNamespaceKeyFunc(item); err == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

func (pc *projectCache) Get(obj interface{}) (item interface{}, exists bool, err error) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return nil, false, err
	}
	return pc.GetByKey(key)
}

func (pc *projectCache) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists, err = pc.Indexer.GetByKey(key)
	if !exists || err != nil {
		return nil, exists, err
	}
	if isObjectInProject(item, pc.projectName) {
		return item, true, nil
	}
	return nil, false, nil
}
