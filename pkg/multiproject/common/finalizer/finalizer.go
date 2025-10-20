package finalizer

import (
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/klog/v2"
)

const (
	ProviderConfigNEGCleanupFinalizer = "multiproject.networking.gke.io/neg-cleanup"
)

func EnsureProviderConfigNEGCleanupFinalizer(cs *providerconfig.ProviderConfig, csClient providerconfigclient.Interface, logger klog.Logger) error {
	return EnsureProviderConfigFinalizer(cs, ProviderConfigNEGCleanupFinalizer, csClient, logger)
}

func DeleteProviderConfigNEGCleanupFinalizer(cs *providerconfig.ProviderConfig, csClient providerconfigclient.Interface, logger klog.Logger) error {
	return DeleteProviderConfigFinalizer(cs, ProviderConfigNEGCleanupFinalizer, csClient, logger)
}

// EnsureProviderConfigFinalizer ensures a finalizer with the given name is present on the ProviderConfig.
func EnsureProviderConfigFinalizer(pc *providerconfig.ProviderConfig, key string, csClient providerconfigclient.Interface, logger klog.Logger) error {
	if HasGivenFinalizer(pc.ObjectMeta, key) {
		return nil
	}

	// Make a deep copy of the ProviderConfig to avoid mutating the shared informer cache.
	updatedObjectMeta := pc.ObjectMeta.DeepCopy()
	updatedObjectMeta.Finalizers = append(updatedObjectMeta.Finalizers, key)

	logger.V(2).Info("Adding finalizer to ProviderConfig", "finalizerKey", key, "providerConfig", pc.Name)
	return patch.PatchProviderConfigObjectMetadata(csClient, pc, *updatedObjectMeta)
}

// DeleteProviderConfigFinalizer removes a finalizer with the given name from the ProviderConfig.
func DeleteProviderConfigFinalizer(pc *providerconfig.ProviderConfig, key string, csClient providerconfigclient.Interface, logger klog.Logger) error {
	if !HasGivenFinalizer(pc.ObjectMeta, key) {
		return nil
	}

	updatedObjectMeta := pc.ObjectMeta.DeepCopy()
	updatedObjectMeta.Finalizers = slice.RemoveString(updatedObjectMeta.Finalizers, key, nil)
	logger.V(2).Info("Deleting finalizer from ProviderConfig", "finalizerKey", key, "providerConfig", pc.Name)
	return patch.PatchProviderConfigObjectMetadata(csClient, pc, *updatedObjectMeta)
}

// HasGivenFinalizer is true if the passed in meta has the specified finalizer.
func HasGivenFinalizer(m metav1.ObjectMeta, key string) bool {
	return slices.Contains(m.Finalizers, key)
}
