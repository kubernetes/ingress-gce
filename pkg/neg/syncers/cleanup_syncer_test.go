package syncers

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/syncers/negstatushandler"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	fakenegbinding "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned/fake"
	informernegbinding "k8s.io/ingress-gce/pkg/negbinding/client/informers/externalversions/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

func TestCleanupSyncerNEGNotFound(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNBClient := fakenegbinding.NewSimpleClientset()

	namespace := "test-ns"
	bindingName := "test-binding"
	now := metav1.Now()

	ref := negbindingv1beta1.StatusNegRef{
		ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/non-existent-neg",
		SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/default",
	}

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              bindingName,
			DeletionTimestamp: &now,
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{ref},
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

	negMetrics := metrics.FakeNegMetrics()
	cloudAdapter := negtypes.NewAdapter(fakeGCE, negMetrics)
	syncerClusterURL := "https://container.googleapis.com/v1/projects/test-project/locations/us-central1/clusters/test-cluster"
	syncer := NewCleanupSyncer(syncerKey, cloudAdapter, statusHandler, bindingLister, syncerClusterURL, klog.TODO()).(*cleanupSyncer)

	err := syncer.cleanUpNEG(ref)
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

	negMetrics := metrics.FakeNegMetrics()
	cloudAdapter := negtypes.NewAdapter(fakeGCE, negMetrics)
	syncerClusterURL := "https://container.googleapis.com/v1/projects/test-project/locations/us-central1/clusters/test-cluster"
	syncer := NewCleanupSyncer(syncerKey, cloudAdapter, statusHandler, bindingLister, syncerClusterURL, klog.TODO()).(*cleanupSyncer)

	err := syncer.cleanUpNEG(negbindingv1beta1.StatusNegRef{ResourceURL: "invalid-url-string"})
	if err != nil {
		t.Errorf("Expected cleanUpNEG to return nil for unparseable NEG URL, got %v", err)
	}
}

func TestCleanupSyncerValidation(t *testing.T) {
	namespace := "test-ns"
	svcName := "test-svc"
	bindingName := "test-binding"
	zone := "us-central1-a"
	syncerClusterURL := "https://container.googleapis.com/v1/projects/test-project/locations/us-central1/clusters/test-cluster"
	defaultSubnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/default"
	otherSubnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/other"

	validDesc := utils.BoundNEGDescription{
		ClusterURL: syncerClusterURL,
		Namespace:  namespace,
		BackendRef: svcName,
	}.String()
	invalidDesc := utils.BoundNEGDescription{
		ClusterURL: syncerClusterURL,
		Namespace:  namespace,
		BackendRef: "other-svc",
	}.String()

	type testNEG struct {
		name                   string
		negDescription         string
		negSubnetwork          string
		statusSubnetURL        string
		expectEndpointsRemoved bool
	}

	testCases := []struct {
		desc string
		negs []testNEG
	}{
		{
			desc: "valid description and subnet detaches endpoints and clears status",
			negs: []testNEG{
				{
					name:                   "test-neg",
					negDescription:         validDesc,
					negSubnetwork:          defaultSubnetURL,
					statusSubnetURL:        defaultSubnetURL,
					expectEndpointsRemoved: true,
				},
			},
		},
		{
			desc: "invalid description skips detaching endpoints and is treated as cleaned up",
			negs: []testNEG{
				{
					name:                   "test-neg",
					negDescription:         invalidDesc,
					negSubnetwork:          defaultSubnetURL,
					statusSubnetURL:        defaultSubnetURL,
					expectEndpointsRemoved: false,
				},
			},
		},
		{
			desc: "mismatched subnet skips detaching endpoints and is treated as cleaned up",
			negs: []testNEG{
				{
					name:                   "test-neg",
					negDescription:         validDesc,
					negSubnetwork:          otherSubnetURL,
					statusSubnetURL:        defaultSubnetURL,
					expectEndpointsRemoved: false,
				},
			},
		},
		{
			desc: "multiple NEGs in status where one is valid and one fails validation",
			negs: []testNEG{
				{
					name:                   "valid-neg",
					negDescription:         validDesc,
					negSubnetwork:          defaultSubnetURL,
					statusSubnetURL:        defaultSubnetURL,
					expectEndpointsRemoved: true,
				},
				{
					name:                   "invalid-neg",
					negDescription:         invalidDesc,
					negSubnetwork:          defaultSubnetURL,
					statusSubnetURL:        defaultSubnetURL,
					expectEndpointsRemoved: false,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeNBClient := fakenegbinding.NewSimpleClientset()
			negMetrics := metrics.FakeNegMetrics()
			fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(defaultSubnetURL, "default")

			var statusNegRefs []negbindingv1beta1.StatusNegRef
			for _, n := range tc.negs {
				err := fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
					Version:             meta.VersionGA,
					Name:                n.name,
					NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
					Subnetwork:          n.negSubnetwork,
					Description:         n.negDescription,
				}, zone, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to create NEG %s: %v", n.name, err)
				}

				err = fakeCloud.AttachNetworkEndpoints(n.name, zone, []*composite.NetworkEndpoint{
					{
						Instance:  "node-1",
						IpAddress: "10.0.0.1",
						Port:      8080,
					},
				}, meta.VersionGA, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to attach endpoint to NEG %s: %v", n.name, err)
				}

				createdNEG, err := fakeCloud.GetNetworkEndpointGroup(n.name, zone, meta.VersionGA, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to get NEG %s: %v", n.name, err)
				}
				statusNegRefs = append(statusNegRefs, negbindingv1beta1.StatusNegRef{
					ResourceURL: createdNEG.SelfLink,
					SubnetURL:   n.statusSubnetURL,
				})
			}

			now := metav1.Now()
			binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         namespace,
					Name:              bindingName,
					DeletionTimestamp: &now,
				},
				Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
					NetworkEndpointGroups: statusNegRefs,
				},
			}

			_, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.Background(), binding, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create binding in client: %v", err)
			}

			indexers := cache.Indexers{
				cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
			}
			bindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
			if err := bindingLister.Add(binding); err != nil {
				t.Fatalf("Failed to add binding to lister: %v", err)
			}

			statusHandler := negstatushandler.NewNEGBindingStatusHandler(
				bindingName,
				namespace,
				fakeNBClient,
				bindingLister,
				negMetrics,
				&testNegBindingRegistry{},
				klog.TODO(),
			)

			syncerKey := negtypes.NegSyncerKey{
				Namespace:      namespace,
				Name:           svcName,
				NEGBindingName: bindingName,
				NegType:        negtypes.VmIpPortEndpointType,
			}

			syncer := NewCleanupSyncer(syncerKey, fakeCloud, statusHandler, bindingLister, syncerClusterURL, klog.TODO()).(*cleanupSyncer)

			if err := syncer.cleanUp(); err != nil {
				t.Fatalf("cleanUp() returned unexpected error: %v", err)
			}

			for _, n := range tc.negs {
				endpoints, err := fakeCloud.ListNetworkEndpoints(n.name, zone, false, meta.VersionGA, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to list endpoints for NEG %s: %v", n.name, err)
				}
				if n.expectEndpointsRemoved && len(endpoints) != 0 {
					t.Errorf("Expected endpoints in NEG %s to be detached, got %d endpoints", n.name, len(endpoints))
				}
				if !n.expectEndpointsRemoved && len(endpoints) != 1 {
					t.Errorf("Expected 1 endpoint in NEG %s to remain untouched, got %d endpoints", n.name, len(endpoints))
				}
			}

			updatedBinding, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.Background(), bindingName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get updated binding: %v", err)
			}
			if len(updatedBinding.Status.NetworkEndpointGroups) != 0 {
				t.Errorf("Expected status NEGs to be cleared after cleanUp, got %v", updatedBinding.Status.NetworkEndpointGroups)
			}
		})
	}
}
