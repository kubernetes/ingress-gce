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
		pc                       *providerconfigv1.ProviderConfig
		expectedFinalizerPresent bool
	}{
		{
			desc: "Finalizer not present, should add it",
			pc: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cs-add-finalizer",
					Finalizers: []string{},
				},
			},
			expectedFinalizerPresent: true,
		},
		{
			desc: "Finalizer already present, should remain",
			pc: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cs-finalizer-present",
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
			pcClient := providerconfigfake.NewSimpleClientset()
			pcInterface := pcClient.CloudV1().ProviderConfigs()

			// Create the initial ProviderConfig object
			_, err := pcInterface.Create(context.TODO(), tc.pc, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create ProviderConfig: %v", err)
			}

			logger, _ := ktesting.NewTestContext(t)

			err = EnsureProviderConfigNEGCleanupFinalizer(tc.pc, pcClient, logger)
			if err != nil {
				t.Fatalf("EnsureProviderConfigNEGCleanupFinalizer returned error: %v", err)
			}

			// Retrieve the updated ProviderConfig
			updatePC, err := pcInterface.Get(context.TODO(), tc.pc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated ProviderConfig: %v", err)
			}

			// Check if the finalizer is present as expected
			hasFinalizer := slices.Contains(updatePC.Finalizers, ProviderConfigNEGCleanupFinalizer)
			if hasFinalizer != tc.expectedFinalizerPresent {
				t.Errorf("Finalizer presence mismatch: expected %v, got %v", tc.expectedFinalizerPresent, hasFinalizer)
			}
		})
	}
}

func TestDeleteProviderConfigNEGCleanupFinalizer(t *testing.T) {
	testCases := []struct {
		desc                     string
		pc                       *providerconfigv1.ProviderConfig
		expectedFinalizerPresent bool
	}{
		{
			desc: "Finalizer present, should be removed",
			pc: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cs-remove-finalizer",
					Finalizers: []string{
						ProviderConfigNEGCleanupFinalizer,
					},
				},
			},
			expectedFinalizerPresent: false,
		},
		{
			desc: "Finalizer not present, remains absent",
			pc: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-cs-finalizer-not-present",
					Finalizers: []string{},
				},
			},
			expectedFinalizerPresent: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			pcClient := providerconfigfake.NewSimpleClientset()
			pcInterface := pcClient.CloudV1().ProviderConfigs()

			// Create the initial ProviderConfig object
			_, err := pcInterface.Create(context.TODO(), tc.pc, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create ProviderConfig: %v", err)
			}

			logger, _ := ktesting.NewTestContext(t)

			err = DeleteProviderConfigNEGCleanupFinalizer(tc.pc, pcClient, logger)
			if err != nil {
				t.Fatalf("DeleteProviderConfigNEGCleanupFinalizer returned error: %v", err)
			}

			// Retrieve the updated ProviderConfig
			updatedPC, err := pcInterface.Get(context.TODO(), tc.pc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated ProviderConfig: %v", err)
			}

			// Check if the finalizer is present as expected
			hasFinalizer := slices.Contains(updatedPC.Finalizers, ProviderConfigNEGCleanupFinalizer)
			if hasFinalizer != tc.expectedFinalizerPresent {
				t.Errorf("Finalizer presence mismatch: expected %v, got %v", tc.expectedFinalizerPresent, hasFinalizer)
			}
		})
	}
}
