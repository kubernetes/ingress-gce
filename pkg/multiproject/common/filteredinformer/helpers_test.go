package filteredinformer

import (
	"fmt"
	"testing"

	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
)

func TestIsObjectInProviderConfig(t *testing.T) {
	flags.F.ProviderConfigNameLabelKey = "provider-config-name-label"

	testCases := []struct {
		desc               string
		providerConfigName string
		allowMissing       bool
		object             interface{}
		expectedToMatch    bool
	}{
		{
			desc:               "Object in provider config should return true",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			object:             &metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
			expectedToMatch:    true,
		},
		{
			desc:               "Object in different provider config should return false",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			object:             &metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p654321-def"}},
			expectedToMatch:    false,
		},
		{
			desc:               "Object with no provider config should return false when allowMissing is false",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			object:             &metav1.ObjectMeta{Name: "obj3"},
			expectedToMatch:    false,
		},
		{
			desc:               "Object with no provider config should return true when allowMissing is true",
			providerConfigName: "p123456-abc",
			allowMissing:       true,
			object:             &metav1.ObjectMeta{Name: "obj3"},
			expectedToMatch:    true,
		},
		{
			desc:               "Invalid object should return false",
			providerConfigName: "p123456-abc",
			allowMissing:       true, // shouldn't matter
			object:             "invalid-object",
			expectedToMatch:    false,
		},
		{
			desc:               "Tombstone object in provider config should return true",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			object: cache.DeletedFinalStateUnknown{
				Key: "some-key",
				Obj: &metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
			},
			expectedToMatch: true,
		},
		{
			desc:               "Tombstone object missing provider config should return true when allowMissing is true",
			providerConfigName: "p123456-abc",
			allowMissing:       true,
			object: cache.DeletedFinalStateUnknown{
				Key: "some-key",
				Obj: &metav1.ObjectMeta{Name: "obj3"},
			},
			expectedToMatch: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			result := isObjectInProviderConfig(tc.object, tc.providerConfigName, tc.allowMissing)
			if result != tc.expectedToMatch {
				t.Errorf("Expected isObjectInProviderConfig to return %v, got %v", tc.expectedToMatch, result)
			}
		})
	}
}

func TestProviderConfigFilteredList(t *testing.T) {
	flags.F.ProviderConfigNameLabelKey = "provider-config-name-label"

	testCases := []struct {
		desc               string
		providerConfigName string
		allowMissing       bool
		objects            []interface{}
		expectedObjects    []interface{}
	}{
		{
			desc:               "All objects in the provider config",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			objects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
			},
		},
		{
			desc:               "Some objects in the provider config",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			objects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p654321-def"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
			},
		},
		{
			desc:               "No objects in the provider config",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			objects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p654321-def"}},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p654321-def"}},
			},
			expectedObjects: []interface{}{},
		},
		{
			desc:               "Allow missing objects",
			providerConfigName: "p123456-abc",
			allowMissing:       true,
			objects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}, Name: "obj1"},
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p654321-def"}, Name: "obj2"},
				&metav1.ObjectMeta{Name: "obj3"}, // missing label
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}, Name: "obj1"},
				&metav1.ObjectMeta{Name: "obj3"},
			},
		},
		{
			desc:               "Invalid objects in the list",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			objects: []interface{}{
				"invalid-object",
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
				12345, // Non-object type
			},
			expectedObjects: []interface{}{
				&metav1.ObjectMeta{Labels: map[string]string{flags.F.ProviderConfigNameLabelKey: "p123456-abc"}},
			},
		},
		{
			desc:               "Empty object list",
			providerConfigName: "p123456-abc",
			allowMissing:       false,
			objects:            []interface{}{},
			expectedObjects:    []interface{}{},
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			result := providerConfigFilteredList(tc.objects, tc.providerConfigName, tc.allowMissing)

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
