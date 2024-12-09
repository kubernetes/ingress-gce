package filteredinformer

import (
	"fmt"
	"testing"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/ingress-gce/pkg/flags"
)

func TestIsObjectInClusterSlice(t *testing.T) {
	testCases := []struct {
		desc             string
		clusterSliceName string
		object           interface{}
		expectedToMatch  bool
	}{
		{
			desc:             "Object in cluster slice should return true",
			clusterSliceName: "p123456-abc",
			object:           &metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
			expectedToMatch:  true,
		},
		{
			desc:             "Object in different cluster slice should return false",
			clusterSliceName: "p123456-abc",
			object:           &metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p654321-def"}},
			expectedToMatch:  false,
		},
		{
			desc:             "Object with no cluster slice should return false",
			clusterSliceName: "p123456-abc",
			object:           &metav1.ObjectMeta{Name: "obj3"},
			expectedToMatch:  false,
		},
		{
			desc:             "Invalid object should return false",
			clusterSliceName: "p123456-abc",
			object:           "invalid-object",
			expectedToMatch:  false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			result := isObjectInClusterSlice(tc.object, tc.clusterSliceName)
			if result != tc.expectedToMatch {
				t.Errorf("Expected isObjectInClusterSlice to return %v, got %v", tc.expectedToMatch, result)
			}
		})
	}
}

func TestClusterSliceFilteredList(t *testing.T) {
	testCases := []struct {
		desc             string
		clusterSliceName string
		objects          []interface{}
		expectedObjects  []interface{}
	}{
		{
			desc:             "All objects in the cluster slice",
			clusterSliceName: "p123456-abc",
			objects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
			},
		},
		{
			desc:             "Some objects in the cluster slice",
			clusterSliceName: "p123456-abc",
			objects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p654321-def"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
			},
		},
		{
			desc:             "No objects in the cluster slice",
			clusterSliceName: "p123456-abc",
			objects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p654321-def"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p654321-def"}},
			},
			expectedObjects: []interface{}{},
		},
		{
			desc:             "Invalid objects in the list",
			clusterSliceName: "p123456-abc",
			objects: []interface{}{
				"invalid-object",
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
				12345, // Non-object type
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ClusterSliceNameLabelKey: "p123456-abc"}},
			},
		},
		{
			desc:             "Empty object list",
			clusterSliceName: "p123456-abc",
			objects:          []interface{}{},
			expectedObjects:  []interface{}{},
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			result := clusterSliceFilteredList(tc.objects, tc.clusterSliceName)

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
