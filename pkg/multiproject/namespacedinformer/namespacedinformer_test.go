package namespacedinformer

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
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

// TestNamespacedInformer_AddEventHandler verifies that the
// namespacedinformer.AddEventHandler method does not return an error.
func TestNamespacedInformer_AddEventHandler(t *testing.T) {
	sharedInformer := cache.NewSharedIndexInformer(nil, &corev1.Pod{}, 0, nil)
	namespacedInformer := NewNamespacedInformer(sharedInformer, "test-namespace")

	handler := cache.ResourceEventHandlerFuncs{}

	_, err := namespacedInformer.AddEventHandler(handler)
	if err != nil {
		t.Fatalf("Failed to add event handler: %v", err)
	}
}

// TestNamespacedInformer_AddEventHandlerWithResyncPeriod verifies that the
// namespacedinformer.AddEventHandlerWithResyncPeriod method does not return an
// error.
func TestNamespacedInformer_AddEventHandlerWithResyncPeriod(t *testing.T) {
	testCases := []struct {
		desc         string
		namespace    string
		resyncPeriod time.Duration
	}{
		{
			desc:         "Add event handler with resync period",
			namespace:    "test-namespace",
			resyncPeriod: time.Minute,
		},
		{
			desc:         "Add event handler with zero resync period",
			namespace:    "test-namespace",
			resyncPeriod: 0,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			sharedInformer := cache.NewSharedIndexInformer(nil, &corev1.Pod{}, 0, nil)
			namespacedInformer := NewNamespacedInformer(sharedInformer, tc.namespace)

			handler := cache.ResourceEventHandlerFuncs{}
			_, err := namespacedInformer.AddEventHandlerWithResyncPeriod(handler, tc.resyncPeriod)
			if err != nil {
				t.Fatalf("Failed to add event handler with resync period: %v", err)
			}
		})
	}
}
