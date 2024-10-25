package namespacedinformer

import (
	"fmt"
	"testing"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestIsObjectInNamespace(t *testing.T) {
	testCases := []struct {
		desc            string
		namespace       string
		object          interface{}
		expectedToMatch bool
	}{
		{
			desc:            "Object in namespace should return true",
			namespace:       "test-namespace",
			object:          &metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
			expectedToMatch: true,
		},
		{
			desc:            "Object in different namespace should return false",
			namespace:       "test-namespace",
			object:          &metav1.ObjectMeta{Namespace: "other-namespace", Name: "obj2"},
			expectedToMatch: false,
		},
		{
			desc:            "Object with no namespace should return false",
			namespace:       "test-namespace",
			object:          &metav1.ObjectMeta{Name: "obj3"},
			expectedToMatch: false,
		},
		{
			desc:            "Invalid object should return false",
			namespace:       "test-namespace",
			object:          "invalid-object",
			expectedToMatch: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			result := isObjectInNamespace(tc.object, tc.namespace)
			if result != tc.expectedToMatch {
				t.Errorf("Expected isObjectInNamespace to return %v, got %v", tc.expectedToMatch, result)
			}
		})
	}
}

func TestNamespaceFilteredList(t *testing.T) {
	testCases := []struct {
		desc            string
		namespace       string
		objects         []interface{}
		expectedObjects []interface{}
	}{
		{
			desc:      "All objects in the namespace",
			namespace: "test-namespace",
			objects: []interface{}{
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj2"},
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj2"},
			},
		},
		{
			desc:      "Some objects in the namespace",
			namespace: "test-namespace",
			objects: []interface{}{
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
				&metav1.ObjectMeta{Namespace: "other-namespace", Name: "obj2"},
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj3"},
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj1"},
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj3"},
			},
		},
		{
			desc:      "No objects in the namespace",
			namespace: "test-namespace",
			objects: []interface{}{
				&metav1.ObjectMeta{Namespace: "other-namespace", Name: "obj1"},
				&metav1.ObjectMeta{Namespace: "another-namespace", Name: "obj2"},
			},
			expectedObjects: []interface{}{},
		},
		{
			desc:      "Invalid objects in the list",
			namespace: "test-namespace",
			objects: []interface{}{
				"invalid-object",
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj2"},
				12345, // Non-object type
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Namespace: "test-namespace", Name: "obj2"},
			},
		},
		{
			desc:            "Empty object list",
			namespace:       "test-namespace",
			objects:         []interface{}{},
			expectedObjects: []interface{}{},
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			result := namespaceFilteredList(tc.objects, tc.namespace)

			if len(result) != len(tc.expectedObjects) {
				t.Errorf("Expected %d objects, got %d", len(tc.expectedObjects), len(result))
			}

			for i, obj := range result {
				expectedObj := tc.expectedObjects[i]

				objMeta, err1 := metaAccessor(obj)
				expectedMeta, err2 := metaAccessor(expectedObj)

				if err1 != nil || err2 != nil {
					t.Errorf("Error accessing object metadata: %v, %v", err1, err2)
					continue
				}

				if objMeta.GetName() != expectedMeta.GetName() || objMeta.GetNamespace() != expectedMeta.GetNamespace() {
					t.Errorf("Expected object %v, got %v", expectedMeta, objMeta)
				}
			}
		})
	}
}

// Helper function to access metadata
func metaAccessor(obj interface{}) (metav1.Object, error) {
	if accessor, ok := obj.(metav1.Object); ok {
		return accessor, nil
	}
	if runtimeObj, ok := obj.(runtime.Object); ok {
		return meta.Accessor(runtimeObj)
	}
	return nil, fmt.Errorf("object does not have ObjectMeta")
}
