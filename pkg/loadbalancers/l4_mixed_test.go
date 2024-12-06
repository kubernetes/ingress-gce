package loadbalancers

import (
	"context"
	"slices"
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
	"k8s.io/ingress-gce/pkg/loadbalancers/mixedprotocoltest"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
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
			resources:   mixedprotocoltest.TCPResources(),
			annotations: mixedprotocoltest.AnnotationsTCP(),
			ingress:     mixedprotocoltest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 udp",
			resources:   mixedprotocoltest.UDPResources(),
			annotations: mixedprotocoltest.AnnotationsUDP(),
			ingress:     mixedprotocoltest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 mixed",
			resources:   mixedprotocoltest.L3Resources(),
			annotations: mixedprotocoltest.AnnotationsL3(),
			ingress:     mixedprotocoltest.IPv4Ingress(),
		},
		{
			desc:        "ipv6 tcp",
			resources:   mixedprotocoltest.TCPResourcesIPv6(),
			annotations: mixedprotocoltest.AnnotationsTCPIPv6(),
			ingress:     mixedprotocoltest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 udp",
			resources:   mixedprotocoltest.UDPResourcesIPv6(),
			annotations: mixedprotocoltest.AnnotationsUDPIPv6(),
			ingress:     mixedprotocoltest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 mixed",
			resources:   mixedprotocoltest.L3ResourcesIPv6(),
			annotations: mixedprotocoltest.AnnotationsL3IPv6(),
			ingress:     mixedprotocoltest.IPv6Ingress(),
		},
		{
			desc:        "dual stack tcp",
			resources:   mixedprotocoltest.TCPResourcesDualStack(),
			annotations: mixedprotocoltest.AnnotationsTCPDualStack(),
			ingress:     mixedprotocoltest.DualStackIngress(),
		},
		{
			desc:        "dual stack udp",
			resources:   mixedprotocoltest.UDPResourcesDualStack(),
			annotations: mixedprotocoltest.AnnotationsUDPDualStack(),
			ingress:     mixedprotocoltest.DualStackIngress(),
		},
		{
			desc:        "dual stack mixed",
			resources:   mixedprotocoltest.L3ResourcesDualStack(),
			annotations: mixedprotocoltest.AnnotationsL3DualStack(),
			ingress:     mixedprotocoltest.DualStackIngress(),
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
			annotations: mixedprotocoltest.AnnotationsTCP(),
			resources:   mixedprotocoltest.TCPResources(),
		},
		{
			desc:        "ipv4 udp",
			spec:        mixedprotocoltest.SpecIPv4(nil, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsUDP(),
			resources:   mixedprotocoltest.UDPResources(),
		},
		{
			desc:        "ipv4 mixed",
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsL3(),
			resources:   mixedprotocoltest.L3Resources(),
		},
		{
			desc:        "ipv6 tcp",
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, nil),
			annotations: mixedprotocoltest.AnnotationsTCPIPv6(),
			resources:   mixedprotocoltest.TCPResourcesIPv6(),
		},
		{
			desc:        "ipv6 udp",
			spec:        mixedprotocoltest.SpecIPv6(nil, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsUDPIPv6(),
			resources:   mixedprotocoltest.UDPResourcesIPv6(),
		},
		{
			desc:        "ipv6 mixed",
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsL3IPv6(),
			resources:   mixedprotocoltest.L3ResourcesIPv6(),
		},
		{
			desc:        "dual stack tcp",
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, nil),
			annotations: mixedprotocoltest.AnnotationsTCPDualStack(),
			resources:   mixedprotocoltest.TCPResourcesDualStack(),
		},
		{
			desc:        "dual stack udp",
			spec:        mixedprotocoltest.SpecDualStack(nil, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsUDPDualStack(),
			resources:   mixedprotocoltest.UDPResourcesDualStack(),
		},
		{
			desc:        "dual stack mixed",
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsL3DualStack(),
			resources:   mixedprotocoltest.L3ResourcesDualStack(),
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
				l4, fakeGCE := arrange(t, s.resources, svc)

				result := l4.EnsureInternalLoadBalancer([]string{mixedprotocoltest.TestNode}, svc)

				wantResult := &L4ILBSyncResult{
					Annotations: e.annotations,
					SyncType:    "create",
				}
				if s.resources.BS != nil {
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
			resources:   mixedprotocoltest.TCPResources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, nil),
			annotations: mixedprotocoltest.AnnotationsTCP(),
			ingress:     mixedprotocoltest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 udp",
			resources:   mixedprotocoltest.UDPResources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, nil),
			annotations: mixedprotocoltest.AnnotationsUDP(),
			ingress:     mixedprotocoltest.IPv4Ingress(),
		},
		{
			desc:        "ipv4 mixed",
			resources:   mixedprotocoltest.L3Resources(),
			spec:        mixedprotocoltest.SpecIPv4([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsL3(),
			ingress:     mixedprotocoltest.IPv4Ingress(),
		},
		{
			desc:        "ipv6 tcp",
			resources:   mixedprotocoltest.TCPResourcesIPv6(),
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, nil),
			annotations: mixedprotocoltest.AnnotationsTCPIPv6(),
			ingress:     mixedprotocoltest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 udp",
			resources:   mixedprotocoltest.UDPResourcesIPv6(),
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, nil),
			annotations: mixedprotocoltest.AnnotationsUDPIPv6(),
			ingress:     mixedprotocoltest.IPv6Ingress(),
		},
		{
			desc:        "ipv6 mixed",
			resources:   mixedprotocoltest.L3ResourcesIPv6(),
			spec:        mixedprotocoltest.SpecIPv6([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsL3IPv6(),
			ingress:     mixedprotocoltest.IPv6Ingress(),
		},
		{
			desc:        "dual stack tcp",
			resources:   mixedprotocoltest.TCPResourcesDualStack(),
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, nil),
			annotations: mixedprotocoltest.AnnotationsTCPDualStack(),
			ingress:     mixedprotocoltest.DualStackIngress(),
		},
		{
			desc:        "dual stack udp",
			resources:   mixedprotocoltest.UDPResourcesDualStack(),
			spec:        mixedprotocoltest.SpecDualStack(nil, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsUDPDualStack(),
			ingress:     mixedprotocoltest.DualStackIngress(),
		},
		{
			desc:        "dual stack mixed",
			resources:   mixedprotocoltest.L3ResourcesDualStack(),
			spec:        mixedprotocoltest.SpecDualStack([]int32{80, 443}, []int32{53}),
			annotations: mixedprotocoltest.AnnotationsL3DualStack(),
			ingress:     mixedprotocoltest.DualStackIngress(),
		},
	}

	for _, tc := range testCases {
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
			assertCleanedUpResources(t, fakeGCE, mixedprotocoltest.GCEResources{}, tc.resources)
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

	if existing.BS != nil {
		if err := fakeGCE.CreateRegionBackendService(existing.BS, vals.Region); err != nil {
			t.Errorf("fakeGCE.CreateBackendService() returned error %v", err)
		}
	}
	if existing.FirewallIPv4 != nil {
		if err := fakeGCE.CreateFirewall(existing.FirewallIPv4); err != nil {
			t.Errorf("fakeGCE.CreateFirewall() returned error %v", err)
		}
	}
	if existing.FwdRuleIPv4 != nil {
		if err := fakeGCE.CreateRegionForwardingRule(existing.FwdRuleIPv4, vals.Region); err != nil {
			t.Errorf("fakeGCE.CreateRegionForwardingRule() returned error %v", err)
		}
	}
	if existing.FirewallIPv6 != nil {
		if err := fakeGCE.CreateFirewall(existing.FirewallIPv6); err != nil {
			t.Errorf("fakeGCE.CreateFirewall() returned error %v", err)
		}
	}
	if existing.FwdRuleIPv6 != nil {
		if err := fakeGCE.CreateRegionForwardingRule(existing.FwdRuleIPv6, vals.Region); err != nil {
			t.Errorf("fakeGCE.CreateRegionForwardingRule() returned error %v", err)
		}
	}
	if existing.HC != nil {
		if err := fakeGCE.CreateHealthCheck(existing.HC); err != nil {
			t.Errorf("fakeGCE.CreateHealthCheck() returned error %v", err)
		}
	}
	if existing.HCFirewallIPv4 != nil {
		if err := fakeGCE.CreateFirewall(existing.HCFirewallIPv4); err != nil {
			t.Errorf("fakeGCE.CreateFirewall() returned error %v", err)
		}
	}
	if existing.HCFirewallIPv6 != nil {
		if err := fakeGCE.CreateFirewall(existing.HCFirewallIPv6); err != nil {
			t.Errorf("fakeGCE.CreateFirewall() returned error %v", err)
		}
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
}

// assertResources checks if resources specified in want are present in fakeGCE and old unused resources are cleaned up
func assertResources(t *testing.T, fakeGCE *gce.Cloud, want, old mixedprotocoltest.GCEResources) {
	t.Helper()

	assertExistingResources(t, fakeGCE, want)
	assertCleanedUpResources(t, fakeGCE, want, old)
}

func assertExistingResources(t *testing.T, fakeGCE *gce.Cloud, want mixedprotocoltest.GCEResources) {
	t.Helper()

	bsIgnoreFields := cmpopts.IgnoreFields(compute.BackendService{}, "SelfLink", "ConnectionDraining", "Region")
	fwdRuleIgnoreFields := cmpopts.IgnoreFields(compute.ForwardingRule{}, "SelfLink")
	hcIgnoreFields := cmpopts.IgnoreFields(compute.HealthCheck{}, "SelfLink")
	firewallIgnoreFields := cmpopts.IgnoreFields(compute.Firewall{}, "SelfLink")

	if want.BS != nil {
		bs, err := fakeGCE.GetRegionBackendService(want.BS.Name, want.BS.Region)
		if err != nil {
			t.Errorf("fakeGCE.GetBackendService() returned error %v", err)
		}
		if diff := cmp.Diff(want.BS, bs, bsIgnoreFields); diff != "" {
			t.Errorf("Backend service mismatch (-want +got):\n%s", diff)
		}
	}

	if want.FwdRuleIPv4 != nil {
		fwdRule, err := fakeGCE.GetRegionForwardingRule(want.FwdRuleIPv4.Name, want.FwdRuleIPv4.Region)
		if err != nil {
			t.Errorf("fakeGCE.GetForwardingRule() returned error %v", err)
		}
		if diff := cmp.Diff(want.FwdRuleIPv4, fwdRule, fwdRuleIgnoreFields); diff != "" {
			t.Errorf("Forwarding rule mismatch (-want +got):\n%s", diff)
		}
	}

	if want.FirewallIPv4 != nil {
		firewall, err := fakeGCE.GetFirewall(want.FirewallIPv4.Name)
		if err != nil {
			t.Errorf("fakeGCE.GetFirewall() returned error %v", err)
		}
		if diff := cmp.Diff(want.FirewallIPv4, firewall, firewallIgnoreFields); diff != "" {
			t.Errorf("Firewall mismatch (-want +got):\n%s", diff)
		}
	}

	if want.FwdRuleIPv6 != nil {
		fwdRule, err := fakeGCE.GetRegionForwardingRule(want.FwdRuleIPv6.Name, want.FwdRuleIPv6.Region)
		if err != nil {
			t.Errorf("fakeGCE.GetForwardingRule() returned error %v", err)
		}
		if diff := cmp.Diff(want.FwdRuleIPv6, fwdRule, fwdRuleIgnoreFields); diff != "" {
			t.Errorf("Forwarding rule mismatch (-want +got):\n%s", diff)
		}
	}

	if want.FirewallIPv6 != nil {
		firewall, err := fakeGCE.GetFirewall(want.FirewallIPv6.Name)
		if err != nil {
			t.Errorf("fakeGCE.GetFirewall() returned error %v", err)
		}
		if diff := cmp.Diff(want.FirewallIPv6, firewall, firewallIgnoreFields); diff != "" {
			t.Errorf("Firewall mismatch (-want +got):\n%s", diff)
		}
	}

	if want.HC != nil {
		hc, err := fakeGCE.GetHealthCheck(want.HC.Name)
		if err != nil {
			t.Errorf("fakeGCE.GetHealthCheck() returned error %v", err)
		}
		if diff := cmp.Diff(want.HC, hc, hcIgnoreFields); diff != "" {
			t.Errorf("Health check mismatch (-want +got):\n%s", diff)
		}
	}

	if want.HCFirewallIPv4 != nil {
		hcFirewall, err := fakeGCE.GetFirewall(want.HCFirewallIPv4.Name)
		if err != nil {
			t.Errorf("fakeGCE.GetFirewall() returned error %v", err)
		}
		slices.Sort(hcFirewall.SourceRanges)
		slices.Sort(want.HCFirewallIPv4.SourceRanges)
		if diff := cmp.Diff(want.HCFirewallIPv4, hcFirewall, firewallIgnoreFields); diff != "" {
			t.Errorf("Health check firewall mismatch (-want +got):\n%s", diff)
		}
	}

	if want.HCFirewallIPv6 != nil {
		hcFirewall, err := fakeGCE.GetFirewall(want.HCFirewallIPv6.Name)
		if err != nil {
			t.Errorf("fakeGCE.GetFirewall() returned error %v", err)
		}
		slices.Sort(hcFirewall.SourceRanges)
		slices.Sort(want.HCFirewallIPv6.SourceRanges)
		if diff := cmp.Diff(want.HCFirewallIPv6, hcFirewall, firewallIgnoreFields); diff != "" {
			t.Errorf("Health check firewall mismatch (-want +got):\n%s", diff)
		}
	}
}

func assertCleanedUpResources(t *testing.T, fakeGCE *gce.Cloud, want, old mixedprotocoltest.GCEResources) {
	t.Helper()

	if old.BS != nil && (want.BS == nil || want.BS.Name != old.BS.Name) {
		bs, err := fakeGCE.GetRegionBackendService(old.BS.Name, old.BS.Region)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("fakeGCE.GetBackendService() returned error %v", err)
		}
		if bs != nil {
			t.Errorf("found backend service %v, which should have been deleted", bs.Name)
		}
	}

	if old.FwdRuleIPv4 != nil && (want.FwdRuleIPv4 == nil || want.FwdRuleIPv4.Name != old.FwdRuleIPv4.Name) {
		fwdRule, err := fakeGCE.GetRegionForwardingRule(old.FwdRuleIPv4.Name, old.FwdRuleIPv4.Region)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("fakeGCE.GetForwardingRule() returned error %v", err)
		}
		if fwdRule != nil {
			t.Errorf("found forwarding rule %v, which should have been deleted", fwdRule.Name)
		}
	}

	if old.FirewallIPv4 != nil && (want.FwdRuleIPv4 == nil || want.FirewallIPv4.Name != old.FirewallIPv4.Name) {
		firewall, err := fakeGCE.GetFirewall(old.FirewallIPv4.Name)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("fakeGCE.GetFirewall() returned error %v", err)
		}
		if firewall != nil {
			t.Errorf("found firewall %v, which should have been deleted", firewall.Name)
		}
	}

	if old.FwdRuleIPv6 != nil && (want.FwdRuleIPv6 == nil || want.FwdRuleIPv6.Name != old.FwdRuleIPv6.Name) {
		fwdRule, err := fakeGCE.GetRegionForwardingRule(old.FwdRuleIPv6.Name, old.FwdRuleIPv6.Region)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("fakeGCE.GetForwardingRule() returned error %v", err)
		}
		if fwdRule != nil {
			t.Errorf("found forwarding rule %v, which should have been deleted", fwdRule.Name)
		}
	}

	if old.FirewallIPv6 != nil && (want.FirewallIPv6 == nil || want.FirewallIPv6.Name != old.FirewallIPv6.Name) {
		firewall, err := fakeGCE.GetFirewall(old.FirewallIPv6.Name)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("fakeGCE.GetFirewall() returned error %v", err)
		}
		if firewall != nil {
			t.Errorf("found firewall %v, which should have been deleted", firewall.Name)
		}
	}

	if old.HC != nil && (want.HC == nil || want.HC.Name != old.HC.Name) {
		hc, err := fakeGCE.GetHealthCheck(old.HC.Name)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("fakeGCE.GetHealthCheck() returned error %v", err)
		}
		if hc != nil {
			t.Errorf("found health check %v, which should have been deleted", hc.Name)
		}
	}

	// We don't need to clean up firewall for health checks, since they are shared
}
