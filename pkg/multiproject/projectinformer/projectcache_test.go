package projectinformer

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
)

func TestProjectCache_ByIndex(t *testing.T) {
	flags.F.MultiProjectCRDProjectNameLabel = "project-name-label"

	testCases := []struct {
		desc              string
		cacheProject      string
		objectsInCache    []interface{}
		queryName         string
		expectedItemNames []string
	}{
		{
			desc:         "Retrieve items by index in project",
			cacheProject: "p123456-abc",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Labels: map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p123456-abc"}, Namespace: "p123456-abc-namespace", Name: "obj1"},
				&v1.ObjectMeta{Labels: map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p123456-abc"}, Namespace: "p123456-abc-namespace", Name: "obj2"},
				&v1.ObjectMeta{Labels: map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p654321-edf"}, Namespace: "p654321-edf-namespace", Name: "obj1"},
			},
			queryName:         "obj1",
			expectedItemNames: []string{"obj1"},
		},
		{
			desc:         "No items when index key does not match",
			cacheProject: "p123456-abc",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Labels: map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p123456-abc"}, Name: "obj1"},
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
			nsCache := &projectCache{
				Indexer:     indexer,
				projectName: tc.cacheProject,
			}

			for _, obj := range tc.objectsInCache {
				indexer.Add(obj)
			}

			items, err := nsCache.ByIndex(indexName, tc.queryName)
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

func TestProjectCache_List(t *testing.T) {
	flags.F.MultiProjectCRDProjectNameLabel = "project-name-label"

	testCases := []struct {
		desc              string
		cacheProject      string
		objectsInCache    []interface{}
		expectedItemNames []string
	}{
		{
			desc:         "List items in the project",
			cacheProject: "p123456-abc",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Labels: map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p123456-abc"}, Name: "obj1"},
				&v1.ObjectMeta{Labels: map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p654321-edf"}, Name: "obj2"},
			},
			expectedItemNames: []string{"obj1"},
		},
		{
			desc:         "List no items when project has no objects",
			cacheProject: "p123456-abc",
			objectsInCache: []interface{}{
				&v1.ObjectMeta{Labels: map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p654321-edf"}, Name: "obj1"},
			},
			expectedItemNames: []string{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, nil)
			nsCache := &projectCache{
				Indexer:     indexer,
				projectName: tc.cacheProject,
			}

			for _, obj := range tc.objectsInCache {
				indexer.Add(obj)
			}

			items := nsCache.List()
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

func TestProjectCache_GetByKey(t *testing.T) {
	flags.F.MultiProjectCRDProjectNameLabel = "project-name-label"

	testCases := []struct {
		desc           string
		cacheProject   string
		queryKey       string
		objectsInCache []interface{}
		expectedExist  bool
		expectedName   string
	}{
		{
			desc:         "Get existing item by key in project",
			cacheProject: "p123456-abc",
			queryKey:     "p123456-abc-namespace/obj1",
			objectsInCache: []interface{}{&v1.ObjectMeta{
				Labels:    map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p123456-abc"},
				Namespace: "p123456-abc-namespace",
				Name:      "obj1",
			}},
			expectedExist: true,
			expectedName:  "obj1",
		},
		{
			desc:         "Item exists but in different project",
			cacheProject: "p123456-abc",
			queryKey:     "p654321-edf-namespace/obj1",
			objectsInCache: []interface{}{&v1.ObjectMeta{
				Labels:    map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p654321-edf"},
				Namespace: "p654321-edf-namespace",
				Name:      "obj1",
			}},
			expectedExist: false,
		},
		{
			desc:         "Item does not exist",
			cacheProject: "p123456-abc",
			objectsInCache: []interface{}{&v1.ObjectMeta{
				Labels:    map[string]string{flags.F.MultiProjectCRDProjectNameLabel: "p123456-abc"},
				Namespace: "p123456-abc-namespace",
				Name:      "obj1",
			}},
			queryKey:      "p123456-abc-namespace/obj2",
			expectedExist: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, nil)
			nsCache := &projectCache{
				Indexer:     indexer,
				projectName: tc.cacheProject,
			}

			for _, obj := range tc.objectsInCache {
				indexer.Add(obj)
			}

			item, exists, err := nsCache.GetByKey(tc.queryKey)
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
