package syncers

import (
	"context"
	"fmt"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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

type cleanupErrorInjectingCloud struct {
	negtypes.NetworkEndpointGroupCloud
	getNEGErrZones        sets.Set[string]
	listEndpointsErrZones sets.Set[string]
}

func (c *cleanupErrorInjectingCloud) GetNetworkEndpointGroup(name string, zone string, version meta.Version, logger klog.Logger) (*composite.NetworkEndpointGroup, error) {
	if c.getNEGErrZones.Has(zone) {
		return nil, fmt.Errorf("injected GetNetworkEndpointGroup error for %s/%s", zone, name)
	}
	return c.NetworkEndpointGroupCloud.GetNetworkEndpointGroup(name, zone, version, logger)
}

func (c *cleanupErrorInjectingCloud) ListNetworkEndpoints(name, zone string, showHealthStatus bool, version meta.Version, logger klog.Logger) ([]*composite.NetworkEndpointWithHealthStatus, error) {
	if c.listEndpointsErrZones.Has(zone) {
		return nil, fmt.Errorf("injected ListNetworkEndpoints error for %s/%s", zone, name)
	}
	return c.NetworkEndpointGroupCloud.ListNetworkEndpoints(name, zone, showHealthStatus, version, logger)
}

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

	negMetrics := metrics.FakeNegMetrics()
	cloudAdapter := negtypes.NewAdapter(fakeGCE, negMetrics)
	syncer := NewCleanupSyncer(syncerKey, cloudAdapter, statusHandler, bindingLister, "", klog.TODO()).(*cleanupSyncer)

	err := syncer.cleanUpNEG("non-existent-neg", "us-central1-a")
	if err != nil {
		t.Errorf("Expected cleanUpNEG to return nil for non-existent NEG (404), got %v", err)
	}
}

func TestCleanupSyncerInvalidURL(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	fakeNBClient := fakenegbinding.NewSimpleClientset()

	namespace := "test-ns"
	bindingName := "test-binding-invalid"

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      bindingName,
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
				{
					ResourceURL: "invalid-url-string",
					SubnetURL:   "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/default",
				},
				{
					ResourceURL: "https://www.googleapis.com/compute/v1/projects/mock-project/zones/us-central1-a/networkEndpointGroups/neg-1",
					SubnetURL:   "invalid-subnet-url",
				},
			},
		},
	}

	indexers := cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	}
	bindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
	bindingLister.Add(binding)
	if _, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}

	negMetrics := metrics.FakeNegMetrics()
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
		Name:           "test-svc",
		NEGBindingName: bindingName,
	}

	cloudAdapter := negtypes.NewAdapter(fakeGCE, negMetrics)
	syncer := NewCleanupSyncer(syncerKey, cloudAdapter, statusHandler, bindingLister, "", klog.TODO()).(*cleanupSyncer)

	err := syncer.cleanUp()
	if err != nil {
		t.Errorf("Expected cleanUp to return nil for unparseable URLs, got %v", err)
	}

	updatedBinding, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), bindingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	if len(updatedBinding.Status.NetworkEndpointGroups) != 0 {
		t.Errorf("expected empty status NEGs after cleanUp, got %v", updatedBinding.Status.NetworkEndpointGroups)
	}
}

func TestCleanupSyncerDeduplicationAndStatusReporting(t *testing.T) {
	namespace := "test-ns"
	bindingName := "test-binding"
	svcName := "test-svc"
	clusterURL := "https://container.googleapis.com/v1/projects/test-project/locations/us-central1/clusters/test-cluster"
	networkURL := "https://www.googleapis.com/compute/v1/projects/mock-project/global/networks/default"
	subnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/default"
	zone1 := "us-central1-a"
	zone2 := "us-central1-b"
	neg1Name := "neg-1"
	neg2Name := "neg-2"
	negDupName := "neg-dup"

	neg1URL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone1, neg1Name).SelfLink(meta.VersionGA)
	neg2URL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone2, neg2Name).SelfLink(meta.VersionGA)
	negDupURL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone1, negDupName).SelfLink(meta.VersionGA)

	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(subnetURL, networkURL)
	for _, tc := range []struct {
		name string
		zone string
		ip   string
	}{
		{name: neg1Name, zone: zone1, ip: "10.0.0.1"},
		{name: neg2Name, zone: zone2, ip: "10.0.0.2"},
		{name: negDupName, zone: zone1, ip: "10.0.0.3"},
	} {
		desc := utils.BoundNEGDescription{
			ClusterURL: clusterURL,
			Namespace:  namespace,
			BackendRef: svcName,
		}.String()
		if err := fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Name:                tc.name,
			Version:             meta.VersionGA,
			Network:             networkURL,
			Subnetwork:          subnetURL,
			NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
			Description:         desc,
		}, tc.zone, klog.TODO()); err != nil {
			t.Fatalf("failed to create NEG %s/%s: %v", tc.zone, tc.name, err)
		}
		if err := fakeCloud.AttachNetworkEndpoints(tc.name, tc.zone, []*composite.NetworkEndpoint{
			{IpAddress: tc.ip, Instance: "node-1", Port: 80},
		}, meta.VersionGA, klog.TODO()); err != nil {
			t.Fatalf("failed to attach endpoint to %s/%s: %v", tc.zone, tc.name, err)
		}
	}

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      bindingName,
			// DeletionTimestamp intentionally nil to verify status is reported regardless of DeletionTimestamp
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
				{ResourceURL: neg1URL, SubnetURL: subnetURL},
				{ResourceURL: negDupURL, SubnetURL: subnetURL}, // duplicate (default, us-central1-a) location
				{ResourceURL: neg2URL, SubnetURL: subnetURL},
			},
		},
	}

	fakeNBClient := fakenegbinding.NewSimpleClientset()
	if _, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}
	indexers := cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	}
	bindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
	bindingLister.Add(binding)

	negMetrics := metrics.FakeNegMetrics()
	statusHandler := negstatushandler.NewNEGBindingStatusHandler(
		bindingName,
		namespace,
		fakeNBClient,
		bindingLister,
		negMetrics,
		&testNegBindingRegistry{},
		klog.TODO(),
	)

	errCloud := &cleanupErrorInjectingCloud{
		NetworkEndpointGroupCloud: fakeCloud,
		listEndpointsErrZones:     sets.New(zone2),
	}

	syncerKey := negtypes.NegSyncerKey{
		Namespace:      namespace,
		Name:           svcName,
		NEGBindingName: bindingName,
		PortTuple:      negtypes.SvcPortTuple{Port: 80, TargetPort: "80"},
		NegType:        negtypes.VmIpPortEndpointType,
	}
	syncer := NewCleanupSyncer(syncerKey, errCloud, statusHandler, bindingLister, clusterURL, klog.TODO()).(*cleanupSyncer)

	// First iteration: neg1 cleaned up, negDup ignored (duplicate location), neg2 fails with transient error.
	err := syncer.cleanUp()
	if err == nil {
		t.Fatalf("cleanUp() on first iteration expected error, got nil")
	}

	// Verify duplicate location NEG was NOT touched (its endpoint remains attached)
	dupEndpoints, err := fakeCloud.ListNetworkEndpoints(negDupName, zone1, false, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("failed to list endpoints in duplicate NEG: %v", err)
	}
	if len(dupEndpoints) != 1 {
		t.Errorf("expected duplicate location NEG to remain untouched with 1 endpoint, got %d", len(dupEndpoints))
	}

	// Verify ReportStatus was called on first iteration and only neg2 remains in status
	updatedBinding, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), bindingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	wantStatusRefs := []negbindingv1beta1.StatusNegRef{
		{ResourceURL: neg2URL, SubnetURL: subnetURL},
	}
	if diff := cmp.Diff(wantStatusRefs, updatedBinding.Status.NetworkEndpointGroups); diff != "" {
		t.Errorf("unexpected status NEGs after first iteration (-want +got):\n%s", diff)
	}
	bindingLister.Update(updatedBinding)

	// Clear transient error for zone2 and run second iteration
	errCloud.listEndpointsErrZones = sets.New[string]()
	if err := syncer.cleanUp(); err != nil {
		t.Fatalf("cleanUp() on second iteration unexpected error: %v", err)
	}

	updatedBinding, err = fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), bindingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding after second iteration: %v", err)
	}
	if len(updatedBinding.Status.NetworkEndpointGroups) != 0 {
		t.Errorf("expected empty status NEGs after second iteration, got %v", updatedBinding.Status.NetworkEndpointGroups)
	}
}

func TestCleanupSyncerVerification(t *testing.T) {
	namespace := "test-ns"
	bindingName := "test-binding"
	svcName := "test-svc"
	clusterURL := "https://container.googleapis.com/v1/projects/test-project/locations/us-central1/clusters/test-cluster"
	networkURL := "https://www.googleapis.com/compute/v1/projects/mock-project/global/networks/default"
	subnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/default"
	otherSubnetURL := "https://www.googleapis.com/compute/v1/projects/mock-project/regions/us-central1/subnetworks/other-subnet"

	zone1 := "us-central1-a"
	zone2 := "us-central1-b"
	zone3 := "us-central1-c"
	zone4 := "us-central1-f"
	zone5 := "us-east1-b"

	negValid := "neg-valid"
	negWrongDesc := "neg-wrong-desc"
	negWrongSubnet := "neg-wrong-subnet"
	negMissing := "neg-missing"
	negGetErr := "neg-get-err"

	validDesc := utils.BoundNEGDescription{
		ClusterURL: clusterURL,
		Namespace:  namespace,
		BackendRef: svcName,
	}.String()
	wrongDesc := utils.BoundNEGDescription{
		ClusterURL: clusterURL,
		Namespace:  namespace,
		BackendRef: "other-svc",
	}.String()

	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(subnetURL, networkURL)
	for _, tc := range []struct {
		name   string
		zone   string
		subnet string
		desc   string
		ip     string
	}{
		{name: negValid, zone: zone1, subnet: subnetURL, desc: validDesc, ip: "10.0.0.1"},
		{name: negWrongDesc, zone: zone2, subnet: subnetURL, desc: wrongDesc, ip: "10.0.0.2"},
		{name: negWrongSubnet, zone: zone3, subnet: otherSubnetURL, desc: validDesc, ip: "10.0.0.3"},
		{name: negGetErr, zone: zone5, subnet: subnetURL, desc: validDesc, ip: "10.0.0.5"},
	} {
		if err := fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Name:                tc.name,
			Version:             meta.VersionGA,
			Network:             networkURL,
			Subnetwork:          tc.subnet,
			NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
			Description:         tc.desc,
		}, tc.zone, klog.TODO()); err != nil {
			t.Fatalf("failed to create NEG %s/%s: %v", tc.zone, tc.name, err)
		}
		if err := fakeCloud.AttachNetworkEndpoints(tc.name, tc.zone, []*composite.NetworkEndpoint{
			{IpAddress: tc.ip, Instance: "node-1", Port: 80},
		}, meta.VersionGA, klog.TODO()); err != nil {
			t.Fatalf("failed to attach endpoint to %s/%s: %v", tc.zone, tc.name, err)
		}
	}

	negValidURL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone1, negValid).SelfLink(meta.VersionGA)
	negWrongDescURL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone2, negWrongDesc).SelfLink(meta.VersionGA)
	negWrongSubnetURL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone3, negWrongSubnet).SelfLink(meta.VersionGA)
	negMissingURL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone4, negMissing).SelfLink(meta.VersionGA)
	negGetErrURL := cloud.NewNetworkEndpointGroupsResourceID("mock-project", zone5, negGetErr).SelfLink(meta.VersionGA)

	binding := &negbindingv1beta1.NetworkEndpointGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      bindingName,
		},
		Status: negbindingv1beta1.NetworkEndpointGroupBindingStatus{
			NetworkEndpointGroups: []negbindingv1beta1.StatusNegRef{
				{ResourceURL: negValidURL, SubnetURL: subnetURL},
				{ResourceURL: negWrongDescURL, SubnetURL: subnetURL},
				{ResourceURL: negWrongSubnetURL, SubnetURL: subnetURL},
				{ResourceURL: negMissingURL, SubnetURL: subnetURL},
				{ResourceURL: negGetErrURL, SubnetURL: subnetURL},
			},
		},
	}

	fakeNBClient := fakenegbinding.NewSimpleClientset()
	if _, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Create(context.TODO(), binding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create binding: %v", err)
	}
	indexers := cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	}
	bindingLister := informernegbinding.NewNetworkEndpointGroupBindingInformer(fakeNBClient, "", 0, indexers).GetIndexer()
	bindingLister.Add(binding)

	negMetrics := metrics.FakeNegMetrics()
	statusHandler := negstatushandler.NewNEGBindingStatusHandler(
		bindingName,
		namespace,
		fakeNBClient,
		bindingLister,
		negMetrics,
		&testNegBindingRegistry{},
		klog.TODO(),
	)

	errCloud := &cleanupErrorInjectingCloud{
		NetworkEndpointGroupCloud: fakeCloud,
		getNEGErrZones:            sets.New(zone5),
	}

	syncerKey := negtypes.NegSyncerKey{
		Namespace:      namespace,
		Name:           svcName,
		NEGBindingName: bindingName,
		PortTuple:      negtypes.SvcPortTuple{Port: 80, TargetPort: "80"},
		NegType:        negtypes.VmIpPortEndpointType,
	}
	syncer := NewCleanupSyncer(syncerKey, errCloud, statusHandler, bindingLister, clusterURL, klog.TODO()).(*cleanupSyncer)

	// First iteration:
	// - negValid: verified and drained
	// - negWrongDesc: fails description verification -> logged only, endpoints untouched, removed from status
	// - negWrongSubnet: fails network/subnet verification -> logged only, endpoints untouched, removed from status
	// - negMissing: 404 on GetNetworkEndpointGroup -> treated as cleaned up, removed from status
	// - negGetErr: non-404 error on GetNetworkEndpointGroup -> returns error, endpoints untouched, kept in status
	if err := syncer.cleanUp(); err == nil {
		t.Fatalf("cleanUp() on first iteration expected error due to GetNetworkEndpointGroup failure, got nil")
	}

	// Verify endpoints in GCE after first iteration
	for _, tc := range []struct {
		name      string
		zone      string
		wantCount int
	}{
		{name: negValid, zone: zone1, wantCount: 0},
		{name: negWrongDesc, zone: zone2, wantCount: 1},
		{name: negWrongSubnet, zone: zone3, wantCount: 1},
		{name: negGetErr, zone: zone5, wantCount: 1},
	} {
		eps, err := fakeCloud.ListNetworkEndpoints(tc.name, tc.zone, false, meta.VersionGA, klog.TODO())
		if err != nil {
			t.Fatalf("failed to list endpoints for %s/%s: %v", tc.zone, tc.name, err)
		}
		if len(eps) != tc.wantCount {
			t.Errorf("endpoints count for %s/%s = %d, want %d", tc.zone, tc.name, len(eps), tc.wantCount)
		}
	}

	// Verify only negGetErr remains in binding status after first iteration
	updatedBinding, err := fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), bindingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding: %v", err)
	}
	wantStatusRefs := []negbindingv1beta1.StatusNegRef{
		{ResourceURL: negGetErrURL, SubnetURL: subnetURL},
	}
	if diff := cmp.Diff(wantStatusRefs, updatedBinding.Status.NetworkEndpointGroups); diff != "" {
		t.Errorf("unexpected status NEGs after first iteration (-want +got):\n%s", diff)
	}
	bindingLister.Update(updatedBinding)

	// Second iteration: clear GetNetworkEndpointGroup error on zone5
	errCloud.getNEGErrZones = sets.New[string]()
	if err := syncer.cleanUp(); err != nil {
		t.Fatalf("cleanUp() on second iteration unexpected error: %v", err)
	}

	eps, err := fakeCloud.ListNetworkEndpoints(negGetErr, zone5, false, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("failed to list endpoints for %s/%s: %v", zone5, negGetErr, err)
	}
	if len(eps) != 0 {
		t.Errorf("expected negGetErr to be drained on second iteration, got %d endpoints", len(eps))
	}

	updatedBinding, err = fakeNBClient.NetworkingV1beta1().NetworkEndpointGroupBindings(namespace).Get(context.TODO(), bindingName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get updated binding after second iteration: %v", err)
	}
	if len(updatedBinding.Status.NetworkEndpointGroups) != 0 {
		t.Errorf("expected empty status NEGs after second iteration, got %v", updatedBinding.Status.NetworkEndpointGroups)
	}
}
