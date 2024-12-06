package finalizer

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterslicev1 "k8s.io/ingress-gce/pkg/apis/clusterslice/v1"
	clusterslicefake "k8s.io/ingress-gce/pkg/clusterslice/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/klog/v2/ktesting"
)

func TestEnsureClusterSliceNEGCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		desc                     string
		cs                       *clusterslicev1.ClusterSlice
		expectedFinalizerPresent bool
	}{
		{
			desc: "Finalizer not present, should add it",
			cs: &clusterslicev1.ClusterSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cs-add-finalizer",
					Namespace:  "default",
					Finalizers: []string{},
				},
			},
			expectedFinalizerPresent: true,
		},
		{
			desc: "Finalizer already present, should remain",
			cs: &clusterslicev1.ClusterSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cs-finalizer-present",
					Namespace: "default",
					Finalizers: []string{
						ClusterSliceNEGCleanupFinalizer,
					},
				},
			},
			expectedFinalizerPresent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			csClient := clusterslicefake.NewSimpleClientset(tc.cs)
			csInterface := csClient.FlagsV1().ClusterSlices(tc.cs.Namespace)

			// Create the initial ClusterSlice object
			_, err := csInterface.Create(context.TODO(), tc.cs, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create ClusterSlice: %v", err)
			}

			logger, _ := ktesting.NewTestContext(t)

			err = EnsureClusterSliceNEGCleanupFinalizer(tc.cs, csInterface, logger)
			if err != nil {
				t.Fatalf("EnsureClusterSliceNEGCleanupFinalizer returned error: %v", err)
			}

			// Retrieve the updated ClusterSlice
			updatedCS, err := csInterface.Get(context.TODO(), tc.cs.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated ClusterSlice: %v", err)
			}

			// Check if the finalizer is present as expected
			hasFinalizer := slice.ContainsString(updatedCS.Finalizers, ClusterSliceNEGCleanupFinalizer, nil)
			if hasFinalizer != tc.expectedFinalizerPresent {
				t.Errorf("Finalizer presence mismatch: expected %v, got %v", tc.expectedFinalizerPresent, hasFinalizer)
			}
		})
	}
}

func TestEnsureDeleteClusterSliceNEGCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		desc                     string
		cs                       *clusterslicev1.ClusterSlice
		expectedFinalizerPresent bool
	}{
		{
			desc: "Finalizer present, should be removed",
			cs: &clusterslicev1.ClusterSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cs-remove-finalizer",
					Namespace: "default",
					Finalizers: []string{
						ClusterSliceNEGCleanupFinalizer,
					},
				},
			},
			expectedFinalizerPresent: false,
		},
		{
			desc: "Finalizer not present, remains absent",
			cs: &clusterslicev1.ClusterSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cs-finalizer-not-present",
					Namespace:  "default",
					Finalizers: []string{},
				},
			},
			expectedFinalizerPresent: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			csClient := clusterslicefake.NewSimpleClientset(tc.cs)
			csInterface := csClient.FlagsV1().ClusterSlices(tc.cs.Namespace)

			// Create the initial ClusterSlice object
			_, err := csInterface.Create(context.TODO(), tc.cs, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create ClusterSlice: %v", err)
			}

			logger, _ := ktesting.NewTestContext(t)

			err = DeleteClusterSliceNEGCleanupFinalizer(tc.cs, csInterface, logger)
			if err != nil {
				t.Fatalf("DeleteClusterSliceNEGCleanupFinalizer returned error: %v", err)
			}

			// Retrieve the updated ClusterSlice
			updatedCS, err := csInterface.Get(context.TODO(), tc.cs.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated ClusterSlice: %v", err)
			}

			// Check if the finalizer is present as expected
			hasFinalizer := slice.ContainsString(updatedCS.Finalizers, ClusterSliceNEGCleanupFinalizer, nil)
			if hasFinalizer != tc.expectedFinalizerPresent {
				t.Errorf("Finalizer presence mismatch: expected %v, got %v", tc.expectedFinalizerPresent, hasFinalizer)
			}
		})
	}
}
