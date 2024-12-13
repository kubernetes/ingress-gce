package finalizer

import (
	"context"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	providerconfigv1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	providerconfigfake "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned/fake"
	"k8s.io/klog/v2/ktesting"
)

func TestEnsureProviderConfigNEGCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		desc                     string
		cs                       *providerconfigv1.ProviderConfig
		expectedFinalizerPresent bool
	}{
		{
			desc: "Finalizer not present, should add it",
			cs: &providerconfigv1.ProviderConfig{
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
			cs: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cs-finalizer-present",
					Namespace: "default",
					Finalizers: []string{
						ProviderConfigNEGCleanupFinalizer,
					},
				},
			},
			expectedFinalizerPresent: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			csClient := providerconfigfake.NewSimpleClientset(tc.cs)
			csInterface := csClient.FlagsV1().ProviderConfigs(tc.cs.Namespace)
			// Create the initial ProviderConfig object
			_, err := csInterface.Create(context.TODO(), tc.cs, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create ProviderConfig: %v", err)
			}

			logger, _ := ktesting.NewTestContext(t)

			err = EnsureProviderConfigNEGCleanupFinalizer(tc.cs, csClient, logger)
			if err != nil {
				t.Fatalf("EnsureProviderConfigNEGCleanupFinalizer returned error: %v", err)
			}

			// Retrieve the updated ProviderConfig
			updatedCS, err := csInterface.Get(context.TODO(), tc.cs.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated ProviderConfig: %v", err)
			}

			// Check if the finalizer is present as expected
			hasFinalizer := slices.Contains(updatedCS.Finalizers, ProviderConfigNEGCleanupFinalizer)
			if hasFinalizer != tc.expectedFinalizerPresent {
				t.Errorf("Finalizer presence mismatch: expected %v, got %v", tc.expectedFinalizerPresent, hasFinalizer)
			}
		})
	}
}

func TestDeleteProviderConfigNEGCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		desc                     string
		cs                       *providerconfigv1.ProviderConfig
		expectedFinalizerPresent bool
	}{
		{
			desc: "Finalizer present, should be removed",
			cs: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cs-remove-finalizer",
					Namespace: "default",
					Finalizers: []string{
						ProviderConfigNEGCleanupFinalizer,
					},
				},
			},
			expectedFinalizerPresent: false,
		},
		{
			desc: "Finalizer not present, remains absent",
			cs: &providerconfigv1.ProviderConfig{
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
			csClient := providerconfigfake.NewSimpleClientset(tc.cs)
			csInterface := csClient.FlagsV1().ProviderConfigs(tc.cs.Namespace)

			// Create the initial ProviderConfig object
			_, err := csInterface.Create(context.TODO(), tc.cs, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create ProviderConfig: %v", err)
			}

			logger, _ := ktesting.NewTestContext(t)

			err = DeleteProviderConfigNEGCleanupFinalizer(tc.cs, csClient, logger)
			if err != nil {
				t.Fatalf("DeleteProviderConfigNEGCleanupFinalizer returned error: %v", err)
			}

			// Retrieve the updated ProviderConfig
			updatedCS, err := csInterface.Get(context.TODO(), tc.cs.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated ProviderConfig: %v", err)
			}

			// Check if the finalizer is present as expected
			hasFinalizer := slices.Contains(updatedCS.Finalizers, ProviderConfigNEGCleanupFinalizer)
			if hasFinalizer != tc.expectedFinalizerPresent {
				t.Errorf("Finalizer presence mismatch: expected %v, got %v", tc.expectedFinalizerPresent, hasFinalizer)
			}
		})
	}
}
