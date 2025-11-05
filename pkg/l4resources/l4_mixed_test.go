package l4resources

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/healthchecksl4"
	"k8s.io/ingress-gce/pkg/l4resources/mixedprotocoltest"
	"k8s.io/ingress-gce/pkg/l4resources/mixedprotocoltest/mixedprotocolilbtest"
	"k8s.io/ingress-gce/pkg/network"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

// TestEnsureMixedILB tests transitions between mixed and single protocol ILBs
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
func TestEnsureMixedILB(t *testing.T) {
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
			resources:   mixedprotocolilbtest.TCPResources(),
			annotations: mixedprotocolilbtest.AnnotationsTCP(),
			ingress:     mixedprotocolilbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 udp",
			resources:   mixedprotocolilbtest.UDPResources(),
			annotations: mixedprotocolilbtest.AnnotationsUDP(),
			ingress:     mixedprotocolilbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 mixed",
			resources:   mixedprotocolilbtest.L3Resources(),
			annotations: mixedprotocolilbtest.AnnotationsL3(),
			ingress:     mixedprotocolilbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv6 tcp",
			resources:   mixedprotocolilbtest.TCPResourcesIPv6(),
			annotations: mixedprotocolilbtest.AnnotationsTCPIPv6(),
			ingress:     mixedprotocolilbtest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 udp",
			resources:   mixedprotocolilbtest.UDPResourcesIPv6(),
			annotations: mixedprotocolilbtest.AnnotationsUDPIPv6(),
			ingress:     mixedprotocolilbtest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 mixed",
			resources:   mixedprotocolilbtest.L3ResourcesIPv6(),
			annotations: mixedprotocolilbtest.AnnotationsL3IPv6(),
			ingress:     mixedprotocolilbtest.IPv6Ingress(),
		},
		{
			desc:        "dual stack tcp",
			resources:   mixedprotocolilbtest.TCPResourcesDualStack(),
			annotations: mixedprotocolilbtest.AnnotationsTCPDualStack(),
			ingress:     mixedprotocolilbtest.DualStackIngress(),
		},
		{
			desc:        "dual stack udp",
			resources:   mixedprotocolilbtest.UDPResourcesDualStack(),
			annotations: mixedprotocolilbtest.AnnotationsUDPDualStack(),
			ingress:     mixedprotocolilbtest.DualStackIngress(),
		},
		{
			desc:        "dual stack mixed",
			resources:   mixedprotocolilbtest.L3ResourcesDualStack(),
			annotations: mixedprotocolilbtest.AnnotationsL3DualStack(),
			ingress:     mixedprotocolilbtest.DualStackIngress(),
		},
	}

	endState := []struct {
		desc string
		// have
		spec api_v1.ServiceSpec
		// want
		resources   mixedprotocoltest.GCEResources
		annotations map[string]string
		conditions  []meta_v1.Condition // Added conditions field
	}{
		{
			desc:        "ipv4 tcp",
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsTCP(),
			resources:   mixedprotocolilbtest.TCPResources(),
			conditions:  mixedprotocolilbtest.ConditionsTCP(),
		},
		{
			desc:        "ipv4 udp",
			spec:        mixedprotocoltest.SpecIPv4(nil, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsUDP(),
			resources:   mixedprotocolilbtest.UDPResources(),
			conditions:  mixedprotocolilbtest.ConditionsUDP(),
		},
		{
			desc:        "ipv4 mixed",
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsL3(),
			resources:   mixedprotocolilbtest.L3Resources(),
			conditions:  mixedprotocolilbtest.ConditionsL3(),
		},
		{
			desc:        "ipv6 tcp",
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsTCPIPv6(),
			resources:   mixedprotocolilbtest.TCPResourcesIPv6(),
			conditions:  mixedprotocolilbtest.ConditionsTCPIPv6(),
		},
		{
			desc:        "ipv6 udp",
			spec:        mixedprotocoltest.SpecIPv6(nil, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsUDPIPv6(),
			resources:   mixedprotocolilbtest.UDPResourcesIPv6(),
			conditions:  mixedprotocolilbtest.ConditionsUDPIPv6(),
		},
		{
			desc:        "ipv6 mixed",
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsL3IPv6(),
			resources:   mixedprotocolilbtest.L3ResourcesIPv6(),
			conditions:  mixedprotocolilbtest.ConditionsL3IPv6(),
		},
		{
			desc:        "dual stack tcp",
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsTCPDualStack(),
			resources:   mixedprotocolilbtest.TCPResourcesDualStack(),
			conditions:  mixedprotocolilbtest.ConditionsTCPDualStack(),
		},
		{
			desc:        "dual stack udp",
			spec:        mixedprotocoltest.SpecDualStack(nil, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsUDPDualStack(),
			resources:   mixedprotocolilbtest.UDPResourcesDualStack(),
			conditions:  mixedprotocolilbtest.ConditionsUDPDualStack(),
		},
		{
			desc:        "dual stack mixed",
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsL3DualStack(),
			resources:   mixedprotocolilbtest.L3ResourcesDualStack(),
			conditions:  mixedprotocolilbtest.ConditionsL3DualStack(),
		},
	}

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
				l4, fakeGCE := arrange(t, s.resources, svc)

				result := l4.EnsureInternalLoadBalancer([]string{mixedprotocoltest.TestNode}, svc)

				wantResult := &L4ILBSyncResult{
					Annotations: e.annotations,
					SyncType:    "create",
					Conditions:  e.conditions, // Populate conditions
				}
				if s.resources.BackendService != nil {
					wantResult.SyncType = "update"
				}
				assertResult(t, result, wantResult)
				assertResources(t, fakeGCE, e.resources, s.resources)
			})
		}
	}
}

func TestDeleteMixedILB(t *testing.T) {
	testCases := []struct {
		desc        string
		resources   mixedprotocoltest.GCEResources
		spec        api_v1.ServiceSpec
		annotations map[string]string
		ingress     []api_v1.LoadBalancerIngress
	}{
		{
			desc:        "ipv4 tcp",
			resources:   mixedprotocolilbtest.TCPResources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsTCP(),
			ingress:     mixedprotocolilbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 udp",
			resources:   mixedprotocolilbtest.UDPResources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsUDP(),
			ingress:     mixedprotocolilbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 mixed",
			resources:   mixedprotocolilbtest.L3Resources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsL3(),
			ingress:     mixedprotocolilbtest.IPv4Ingress(),
		},
		{
			desc:        "ipv6 tcp",
			resources:   mixedprotocolilbtest.TCPResourcesIPv6(),
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsTCPIPv6(),
			ingress:     mixedprotocolilbtest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 udp",
			resources:   mixedprotocolilbtest.UDPResourcesIPv6(),
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsUDPIPv6(),
			ingress:     mixedprotocolilbtest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 mixed",
			resources:   mixedprotocolilbtest.L3ResourcesIPv6(),
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsL3IPv6(),
			ingress:     mixedprotocolilbtest.IPv6Ingress(),
		},
		{
			desc:        "dual stack tcp",
			resources:   mixedprotocolilbtest.TCPResourcesDualStack(),
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, nil),
			annotations: mixedprotocolilbtest.AnnotationsTCPDualStack(),
			ingress:     mixedprotocolilbtest.DualStackIngress(),
		},
		{
			desc:        "dual stack udp",
			resources:   mixedprotocolilbtest.UDPResourcesDualStack(),
			spec:        mixedprotocoltest.SpecDualStack(nil, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsUDPDualStack(),
			ingress:     mixedprotocolilbtest.DualStackIngress(),
		},
		{
			desc:        "dual stack mixed",
			resources:   mixedprotocolilbtest.L3ResourcesDualStack(),
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocolilbtest.AnnotationsL3DualStack(),
			ingress:     mixedprotocolilbtest.DualStackIngress(),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := &api_v1.Service{
				ObjectMeta: meta_v1.ObjectMeta{
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
			l4, fakeGCE := arrange(t, tc.resources, svc)

			result := l4.EnsureInternalLoadBalancerDeleted(svc)

			wantResult := &L4ILBSyncResult{
				Annotations: map[string]string{},
				SyncType:    "delete",
			}
			assertResult(t, result, wantResult)
			mixedprotocoltest.VerifyResourcesCleanedUp(t, fakeGCE, tc.resources, mixedprotocoltest.GCEResources{})
		})
	}
}

// arrange creates necessary mocks and services for mixed protocol ilb tests
func arrange(t *testing.T, existing mixedprotocoltest.GCEResources, svc *api_v1.Service) (*L4, *gce.Cloud) {
	t.Helper()
	vals := gce.DefaultTestClusterValues()
	fakeGCE := gce.NewFakeGCECloud(vals)
	// Update hooks to be able to perform updates
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	mockGCE.MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	mockGCE.MockRegionBackendServices.PatchHook = mock.UpdateRegionBackendServiceHook
	mockGCE.MockFirewalls.UpdateHook = mock.UpdateFirewallHook
	mockGCE.MockFirewalls.PatchHook = mock.UpdateFirewallHook

	namer := namer_util.NewL4Namer(kubeSystemUID, nil)
	l4ILBParams := &L4ILBParams{
		Service:             svc,
		Cloud:               fakeGCE,
		Namer:               namer,
		Recorder:            record.NewFakeRecorder(100),
		NetworkResolver:     network.NewFakeResolver(network.DefaultNetwork(fakeGCE)),
		EnableMixedProtocol: true,
		DualStackEnabled:    true,
	}
	l4 := NewL4Handler(l4ILBParams, klog.TODO())
	// For testing use Fake
	l4.healthChecks = healthchecksl4.Fake(fakeGCE, l4ILBParams.Recorder)

	if err := fakeGCE.InsertInstance(vals.ProjectID, vals.ZoneName, &compute.Instance{
		Name: mixedprotocoltest.TestNode,
		Tags: &compute.Tags{Items: []string{mixedprotocoltest.TestNode}},
	}); err != nil {
		t.Errorf("fakeGCE.InsertInstance() returned error %v", err)
	}

	dualStackSubnetwork := &compute.Subnetwork{
		StackType:      "IPV4_IPV6",
		Ipv6AccessType: "INTERNAL",
	}
	if err := fakeGCE.Compute().Subnetworks().Insert(context.TODO(), meta.RegionalKey("", vals.Region), dualStackSubnetwork); err != nil {
		t.Errorf("fakeGCE.Compute().Subnetworks().Insert() returned error %v", err)
	}

	if err := existing.Create(fakeGCE); err != nil {
		t.Errorf("existing.Create() returned error %v", err)
	}

	return l4, fakeGCE
}

// assertResult compares received result to the wanted
func assertResult(t *testing.T, got, want *L4ILBSyncResult) {
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
	diff := cmp.Diff(want.Conditions, got.Conditions,
		cmpopts.IgnoreFields(meta_v1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
		cmpopts.SortSlices(func(a, b meta_v1.Condition) bool { return a.Type < b.Type }),
	)
	if diff != "" {
		t.Errorf("got.Conditions != want.Conditions (-want +got):\n%s", diff)
	}
}

// assertResources checks if resources specified in want are present in fakeGCE and old unused resources are cleaned up
func assertResources(t *testing.T, fakeGCE *gce.Cloud, want, old mixedprotocoltest.GCEResources) {
	t.Helper()

	mixedprotocoltest.VerifyResourcesExist(t, fakeGCE, want)
	mixedprotocoltest.VerifyResourcesCleanedUp(t, fakeGCE, old, want)
}
