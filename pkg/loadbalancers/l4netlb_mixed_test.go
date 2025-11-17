package loadbalancers

import (
	"context"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/healthchecksl4"
	"k8s.io/ingress-gce/pkg/loadbalancers/mixedprotocoltest"
	"k8s.io/ingress-gce/pkg/loadbalancers/mixedprotocoltest/mixedprotocolnetlbtest"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

// TestEnsureMixedNetLB tests transitions between mixed and single protocol ILBs
// Steps:
//  1. Arrange:
//     a) Create existing GCE resources with fakeGCE based on starting state.
//     b) Create L4 struct with mixed protocol feature flag enabled
//  2. Act:
//     a) Run l4.Ensure(service)
//  3. Assert:
//     a) Verify result
//     b) Verify resources from fakeGCE with resources in the end state
//     c) Verify resources from starting state not used in end state are cleaned up
func TestEnsureMixedNetLB(t *testing.T) {
	startState := []struct {
		desc string
		// have
		resources   mixedprotocoltest.GCEResources
		annotations map[string]string
		ingress     []api_v1.LoadBalancerIngress
	}{
		{
			desc: "nothing",
		},
		{
			desc:        "ipv4 tcp",
			resources:   mixedprotocolnetlbtest.TCPResources(),
			annotations: mixedprotocolnetlbtest.AnnotationsTCP(),
			ingress:     mixedprotocolnetlbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 udp",
			resources:   mixedprotocolnetlbtest.UDPResources(),
			annotations: mixedprotocolnetlbtest.AnnotationsTCP(),
			ingress:     mixedprotocolnetlbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 mixed",
			resources:   mixedprotocolnetlbtest.MixedResources(),
			annotations: mixedprotocolnetlbtest.AnnotationsMixed(),
			ingress:     mixedprotocolnetlbtest.IPv4Ingress(),
		},
	}

	endState := []struct {
		desc string
		// have
		spec api_v1.ServiceSpec
		// want
		resources   mixedprotocoltest.GCEResources
		annotations map[string]string
	}{
		{
			desc:        "ipv4 tcp",
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, nil),
			annotations: mixedprotocolnetlbtest.AnnotationsTCP(),
			resources:   mixedprotocolnetlbtest.TCPResources(),
		},
		{
			desc:        "ipv4 udp",
			spec:        mixedprotocoltest.SpecIPv4(nil, []int32{53}),
			annotations: mixedprotocolnetlbtest.AnnotationsUDP(),
			resources:   mixedprotocolnetlbtest.UDPResources(),
		},
		{
			desc:        "ipv4 mixed",
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolnetlbtest.AnnotationsMixed(),
			resources:   mixedprotocolnetlbtest.MixedResources(),
		},
	}

	// this flag is for single protocol only, mixed protocol use DiscretePortForwarding by default
	flags.F.EnableDiscretePortForwarding = true
	for _, s := range startState {
		for _, e := range endState {
			desc := s.desc + " -> " + e.desc
			s, e := s, e
			t.Run(desc, func(t *testing.T) {
				t.Parallel()
				svc := &api_v1.Service{
					ObjectMeta: meta_v1.ObjectMeta{
						UID:         mixedprotocoltest.TestUID,
						Name:        mixedprotocoltest.TestName,
						Namespace:   mixedprotocoltest.TestNamespace,
						Annotations: s.annotations,
					},
					Spec: e.spec,
					Status: api_v1.ServiceStatus{
						LoadBalancer: api_v1.LoadBalancerStatus{
							Ingress: s.ingress,
						},
					},
				}
				l4netlb, fakeGCE := arrangeNetLB(t, s.resources, svc)

				result := l4netlb.EnsureFrontend([]string{mixedprotocoltest.TestNode}, svc, time.Now())

				wantResult := &L4NetLBSyncResult{
					Annotations: e.annotations,
					SyncType:    "create",
				}
				if s.resources.BackendService != nil {
					wantResult.SyncType = "update"
				}

				assertNetLBResult(t, result, wantResult)
				assertResources(t, fakeGCE, e.resources, s.resources)
			})
		}
	}
}

func assertNetLBResult(t *testing.T, got, want *L4NetLBSyncResult) {
	t.Helper()

	if got.Error != want.Error {
		t.Errorf("got.Error != want.Error: got %v, want %v", got.Error, want.Error)
	}
	if got.SyncType != want.SyncType {
		t.Errorf("got.SyncType != want.SyncType: got %v, want %v", got.SyncType, want.SyncType)
	}

	if diff := cmp.Diff(got.Annotations, want.Annotations); diff != "" {
		t.Errorf("got.Annotations != want.Annotations: (-got +want):\n%s", diff)
	}
}

func arrangeNetLB(t *testing.T, existing mixedprotocoltest.GCEResources, svc *api_v1.Service) (*L4NetLB, *gce.Cloud) {
	t.Helper()
	vals := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)

	// Update hooks to be able to perform updates
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	mockGCE.MockRegionBackendServices.PatchHook = mock.UpdateRegionBackendServiceHook
	mockGCE.MockFirewalls.UpdateHook = mock.UpdateFirewallHook
	mockGCE.MockFirewalls.PatchHook = mock.UpdateFirewallHook

	namer := namer.NewL4Namer(kubeSystemUID, nil)
	params := &L4NetLBParams{
		Service:             svc,
		Cloud:               fakeGCE,
		Namer:               namer,
		Recorder:            &record.FakeRecorder{},
		NetworkResolver:     network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
		EnableMixedProtocol: true,
		DualStackEnabled:    true,
	}
	l4NetLB := NewL4NetLB(params, klog.TODO())
	l4NetLB.healthChecks = healthchecksl4.Fake(fakeGCE, params.Recorder)

	if err := fakeGCE.InsertInstance(vals.ProjectID, vals.ZoneName, &compute.Instance{
		Name: mixedprotocoltest.TestNode,
		Tags: &compute.Tags{Items: []string{mixedprotocoltest.TestNode}},
	}); err != nil {
		t.Errorf("fakeGCE.InsertInstance() returned error %v", err)
	}

	dualStackSubnetwork := &compute.Subnetwork{
		StackType:      "IPV4_IPV6",
		Ipv6AccessType: "EXTERNAL",
	}
	if err := fakeGCE.Compute().Subnetworks().Insert(context.TODO(), meta.RegionalKey("", vals.Region), dualStackSubnetwork); err != nil {
		t.Errorf("fakeGCE.Compute().Subnetworks().Insert() returned error %v", err)
	}

	if err := existing.Create(fakeGCE); err != nil {
		t.Errorf("existing.Create() returned error %v", err)
	}

	return l4NetLB, fakeGCE
}

func TestDeleteMixedNetLB(t *testing.T) {
	testCases := []struct {
		desc        string
		resources   mixedprotocoltest.GCEResources
		spec        api_v1.ServiceSpec
		annotations map[string]string
		ingress     []api_v1.LoadBalancerIngress
	}{
		{
			desc:        "ipv4 tcp",
			resources:   mixedprotocolnetlbtest.TCPResources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, nil),
			annotations: mixedprotocolnetlbtest.AnnotationsTCP(),
			ingress:     mixedprotocolnetlbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 udp",
			resources:   mixedprotocolnetlbtest.UDPResources(),
			spec:        mixedprotocoltest.SpecIPv4(nil, []int32{53}),
			annotations: mixedprotocolnetlbtest.AnnotationsTCP(),
			ingress:     mixedprotocolnetlbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 mixed",
			resources:   mixedprotocolnetlbtest.MixedResources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolnetlbtest.AnnotationsMixed(),
			ingress:     mixedprotocolnetlbtest.IPv4Ingress(),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
					UID:         mixedprotocoltest.TestUID,
					Name:        mixedprotocoltest.TestName,
					Namespace:   mixedprotocoltest.TestNamespace,
					Annotations: tc.annotations,
				},
				Spec: tc.spec,
				Status: api_v1.ServiceStatus{
					LoadBalancer: api_v1.LoadBalancerStatus{
						Ingress: tc.ingress,
					},
				},
			}
			l4NetLB, fakeGCE := arrangeNetLB(t, tc.resources, svc)

			result := l4NetLB.EnsureLoadBalancerDeleted(svc)

			wantResult := &L4NetLBSyncResult{
				Annotations: map[string]string{},
				SyncType:    "delete",
			}
			assertNetLBResult(t, result, wantResult)
			mixedprotocoltest.VerifyResourcesCleanedUp(t, fakeGCE, tc.resources, mixedprotocoltest.GCEResources{})
		})
	}
}
