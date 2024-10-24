package namespacedinformer

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestNamespacedStore_List(t *testing.T) {
	testCases := []struct {
		desc              string
		storeNamespace    string
		objectsInCache    []interface{}
		expectedItemNames []string
	}{
		{
			desc:           "List items in the specified namespace",
			storeNamespace: "test-namespace",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
				&v1.ObjectMeta{Namespace: "other-namespace", Name: "obj2"},
			},
			expectedItemNames: []string{"obj1"},
		},
		{
			desc:           "List no items when namespace has no objects",
			storeNamespace: "empty-namespace",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
			},
			expectedItemNames: []string{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			store := cache.NewStore(cache.MetaNamespaceKeyFunc)
			nsStore := &namespacedStore{
				Store:     store,
				namespace: tc.storeNamespace,
			}

			for _, obj := range tc.objectsInCache {
				store.Add(obj)
			}

			items := nsStore.List()
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

func TestNamespacedStore_GetByKey(t *testing.T) {
	testCases := []struct {
		desc           string
		storeNamespace string
		queryKey       string
		objectsInCache []interface{}
		expectedExist  bool
		expectedName   string
	}{
		{
			desc:           "Get existing item by key in namespace",
			storeNamespace: "test-namespace",
			queryKey:       "test-namespace/obj1",
			objectsInCache: []interface{}{&v1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"}},
			expectedExist:  true,
			expectedName:   "obj1",
		},
		{
			desc:           "Item exists but in different namespace",
			storeNamespace: "test-namespace",
			queryKey:       "other-namespace/obj1",
			objectsInCache: []interface{}{&v1.ObjectMeta{Namespace: "other-namespace", Name: "obj1"}},
			expectedExist:  false,
		},
		{
			desc:           "Item does not exist",
			storeNamespace: "test-namespace",
			objectsInCache: []interface{}{&v1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"}},
			queryKey:       "test-namespace/obj2",
			expectedExist:  false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			store := cache.NewStore(cache.MetaNamespaceKeyFunc)
			nsStore := &namespacedStore{
				Store:     store,
				namespace: tc.storeNamespace,
			}

			for _, obj := range tc.objectsInCache {
				store.Add(obj)
			}

			item, exists, err := nsStore.GetByKey(tc.queryKey)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if exists != tc.expectedExist {
				t.Errorf("Expected exists to be %v, got %v", tc.expectedExist, exists)
			}
			if exists && item != nil {
				metaObj, _ := meta.Accessor(item)
				if metaObj.GetName() != tc.expectedName {
					t.Errorf("Expected item name %s, got %s", tc.expectedName, metaObj.GetName())
				}
			}
		})
	}
}
