package filteredinformer

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
)

// TestFilteredinformer_AddEventHandler verifies that the
// filteredinformer.AddEventHandler method does not return an error.
func TestFilteredinformer_AddEventHandler(t *testing.T) {
	flags.F.ClusterSliceNameLabelKey = "cluster-slice-name-label"

	sharedInformer := cache.NewSharedIndexInformer(nil, &corev1.Pod{}, 0, nil)
	filteredinformer := NewClusterSliceFilteredInformer(sharedInformer, "test-cluster-slice")

	handler := cache.ResourceEventHandlerFuncs{}

	_, err := filteredinformer.AddEventHandler(handler)
	if err != nil {
		t.Fatalf("Failed to add event handler: %v", err)
	}
}

// TestNamespacedInformer_AddEventHandlerWithResyncPeriod verifies that the
// namespacedinformer.AddEventHandlerWithResyncPeriod method does not return an
// error.
func TestFilteredinformer_AddEventHandlerWithResyncPeriod(t *testing.T) {
	flags.F.ClusterSliceNameLabelKey = "cluster-slice-name-label"

	testCases := []struct {
		desc             string
		clusterSliceName string
		resyncPeriod     time.Duration
	}{
		{
			desc:             "Add event handler with resync period",
			clusterSliceName: "test-cluster-slice",
			resyncPeriod:     time.Minute,
		},
		{
			desc:             "Add event handler with zero resync period",
			clusterSliceName: "test-cluster-slice",
			resyncPeriod:     0,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			sharedInformer := cache.NewSharedIndexInformer(nil, &corev1.Pod{}, 0, nil)
			filteredinformer := NewClusterSliceFilteredInformer(sharedInformer, tc.clusterSliceName)

			handler := cache.ResourceEventHandlerFuncs{}
			_, err := filteredinformer.AddEventHandlerWithResyncPeriod(handler, tc.resyncPeriod)
			if err != nil {
				t.Fatalf("Failed to add event handler with resync period: %v", err)
			}
		})
	}
}
