package syncers

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/syncers/negstatushandler"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	fakenegbinding "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned/fake"
	informernegbinding "k8s.io/ingress-gce/pkg/negbinding/client/informers/externalversions/negbinding/v1beta1"
	"k8s.io/klog/v2"
)

func TestCleanupSyncerNEGNotFound(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNBClient := fakenegbinding.NewSimpleClientset()

	namespace := "test-ns"
	bindingName := "test-binding"
	now := metav1.Now()

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              bindingName,
			DeletionTimestamp: &now,
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/non-existent-neg",
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/default",
				},
			},
		},
	}

	indexers := cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	}
	bindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
	bindingLister.Add(binding)

	statusHandler := negstatushandler.NewNEGBindingStatusHandler(
		bindingName,
		namespace,
		fakeNBClient,
		bindingLister,
		nil,
		nil,
		klog.TODO(),
	)

	syncerKey := negtypes.NegSyncerKey{
		Namespace:      namespace,
		Name:           "test-svc",
		NEGBindingName: bindingName,
	}

	negMetrics := metrics.NewNegMetrics()
	cloudAdapter := negtypes.NewAdapter(fakeGCE, negMetrics)
	syncer := NewCleanupSyncer(syncerKey, cloudAdapter, statusHandler, bindingLister, klog.TODO()).(*cleanupSyncer)

	err := syncer.cleanUpNEG("https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/non-existent-neg")
	if err != nil {
		t.Errorf("Expected cleanUpNEG to return nil for non-existent NEG (404), got %v", err)
	}
}

func TestCleanupSyncerInvalidURL(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNBClient := fakenegbinding.NewSimpleClientset()

	namespace := "test-ns"
	bindingName := "test-binding-invalid"

	indexers := cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	}
	bindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()

	statusHandler := negstatushandler.NewNEGBindingStatusHandler(
		bindingName,
		namespace,
		fakeNBClient,
		bindingLister,
		nil,
		nil,
		klog.TODO(),
	)

	syncerKey := negtypes.NegSyncerKey{
		Namespace:      namespace,
		Name:           "test-svc",
		NEGBindingName: bindingName,
	}

	negMetrics := metrics.NewNegMetrics()
	cloudAdapter := negtypes.NewAdapter(fakeGCE, negMetrics)
	syncer := NewCleanupSyncer(syncerKey, cloudAdapter, statusHandler, bindingLister, klog.TODO()).(*cleanupSyncer)

	err := syncer.cleanUpNEG("invalid-url-string")
	if err != nil {
		t.Errorf("Expected cleanUpNEG to return nil for unparseable NEG URL, got %v", err)
	}
}
