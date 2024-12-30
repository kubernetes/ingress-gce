package loadbalancers

import (
	"testing"

	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/loadbalancers/mixedprotocoltest"
	"k8s.io/ingress-gce/pkg/loadbalancers/mixedprotocoltest/mixedprotocolnetlbtest"
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
			desc: "ipv4 tcp",
			resources: mixedprotocolnetlbtest.TCPResources(),
			annotations: mixedprotocolnetlbtest.,
		},
		{
			desc: "ipv4 udp",
			resources: mixedprotocolnetlbtest.UDPResources(),
		},
		{
			desc: "ipv4 mixed",
			resources: mixedprotocolnetlbtest.MixedResources(),
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
			desc: "ipv4 tcp",
		},
		{
			desc: "ipv4 udp",
		},
		{
			desc: "ipv4 mixed",
		},
	}

	for _, s := range startState {
		for _, e := range endState {
			desc := s.desc + " -> " + e.desc
			t.Run(desc, func(t *testing.T) {
				t.Parallel()
				svc := &api_v1.Service{
					ObjectMeta: meta_v1.ObjectMeta{
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

				result := l4netlb.EnsureFrontend([]string{mixedprotocoltest.TestNode}, svc)

				wantResult := &L4NetLBSyncResult{
					Annotations: e.annotations,
					SyncType:    "create",
				}
				if s.resources.BS != nil {
					wantResult.SyncType = "update"
				}

				assertNetLBResult(t, result, wantResult)
				assertResources(t, fakeGCE, e.resources, s.resources)
			})
		}
	}
}

func assertNetLBResult(t *testing.T, result *L4NetLBSyncResult, wantResult *L4NetLBSyncResult) {
	t.Helper()
	panic("unimplemented")
}

func arrangeNetLB(t *testing.T, gCEResources mixedprotocoltest.GCEResources, svc *api_v1.Service) (*L4NetLB, *gce.Cloud) {
	t.Helper()
	panic("unimplemented")
}

func TestDeleteMixedNetLB(t *testing.T) {
	testCases := []struct {
		desc        string
		resources   mixedprotocoltest.GCEResources
		spec        api_v1.ServiceSpec
		annotations map[string]string
		ingress     []api_v1.LoadBalancerIngress
	}{}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
		})
	}
}
