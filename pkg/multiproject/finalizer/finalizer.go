package finalizer

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterslice "k8s.io/ingress-gce/pkg/apis/clusterslice/v1"
	clustersliceclient "k8s.io/ingress-gce/pkg/clusterslice/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/klog/v2"
)

const (
	ClusterSliceNEGCleanupFinalizer = "multiproject.networking.gke.io/neg-cleanup"
)

func EnsureClusterSliceNEGCleanupFinalizer(cs *clusterslice.ClusterSlice, csClient clustersliceclient.Interface, logger klog.Logger) error {
	return ensureClusterSliceFinalizer(cs, ClusterSliceNEGCleanupFinalizer, csClient, logger)
}

func DeleteClusterSliceNEGCleanupFinalizer(cs *clusterslice.ClusterSlice, csClient clustersliceclient.Interface, logger klog.Logger) error {
	return deleteClusterSliceFinalizer(cs, ClusterSliceNEGCleanupFinalizer, csClient, logger)
}

func ensureClusterSliceFinalizer(cs *clusterslice.ClusterSlice, key string, csClient clustersliceclient.Interface, logger klog.Logger) error {
	if HasGivenFinalizer(cs.ObjectMeta, key) {
		return nil
	}

	// Make a deep copy of the ClusterSlice to avoid mutating the shared informer cache.
	updatedObjectMeta := cs.ObjectMeta.DeepCopy()
	updatedObjectMeta.Finalizers = append(updatedObjectMeta.Finalizers, key)

	logger.V(2).Info("Adding finalizer to ClusterSlice", "finalizerKey", key, "clusterSlice", fmt.Sprintf("%s/%s", cs.Namespace, cs.Name))
	return patch.PatchClusterSliceObjectMetadata(csClient, cs, *updatedObjectMeta)
}

func deleteClusterSliceFinalizer(cs *clusterslice.ClusterSlice, key string, csClient clustersliceclient.Interface, logger klog.Logger) error {
	if !HasGivenFinalizer(cs.ObjectMeta, key) {
		return nil
	}

	updatedObjectMeta := cs.ObjectMeta.DeepCopy()
	updatedObjectMeta.Finalizers = slice.RemoveString(updatedObjectMeta.Finalizers, key, nil)
	logger.V(2).Info("Deleting finalizer from ClusterSlice", "finalizerKey", key, "clusterSlice", fmt.Sprintf("%s/%s", cs.Namespace, cs.Name))
	return patch.PatchClusterSliceObjectMetadata(csClient, cs, *updatedObjectMeta)
}

// HasGivenFinalizer is true if the passed in meta has the specified finalizer.
func HasGivenFinalizer(m metav1.ObjectMeta, key string) bool {
	return slice.ContainsString(m.Finalizers, key, nil)
}
