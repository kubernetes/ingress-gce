package namespacedinformer

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestNamespacedIndexer_ByIndex(t *testing.T) {
	testCases := []struct {
		desc              string
		indexerNamespace  string
		objectsInCache    []interface{}
		queryName         string
		expectedItemNames []string
	}{
		{
			desc:             "Retrieve items by index in namespace",
			indexerNamespace: "test-namespace",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
				&v1.ObjectMeta{Namespace: "test-namespace", Name: "obj2"},
				&v1.ObjectMeta{Namespace: "other-namespace", Name: "obj1"},
			},
			queryName:         "obj1",
			expectedItemNames: []string{"obj1"},
		},
		{
			desc:             "No items when index key does not match",
			indexerNamespace: "test-namespace",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
			},
			queryName:         "nonexistent",
			expectedItemNames: []string{},
		},
	}

	indexName := "byName"
	indexers := cache.Indexers{
		indexName: func(obj interface{}) ([]string, error) {
			metaObj, _ := meta.Accessor(obj)
			return []string{metaObj.GetName()}, nil
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, indexers)
			nsIndexer := &namespacedIndexer{
				Indexer:   indexer,
				namespace: tc.indexerNamespace,
			}

			for _, obj := range tc.objectsInCache {
				indexer.Add(obj)
			}

			items, err := nsIndexer.ByIndex(indexName, tc.queryName)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if len(items) != len(tc.expectedItemNames) {
				t.Errorf("Expected %d items, got %d", len(tc.expectedItemNames), len(items))
			}

			for i, item := range items {
				metaObj, _ := meta.Accessor(item)
				if metaObj.GetName() != tc.expectedItemNames[i] {
					t.Errorf("Expected item name %s, got %s", tc.expectedItemNames[i], metaObj.GetName())
				}
			}
		})
	}
}
