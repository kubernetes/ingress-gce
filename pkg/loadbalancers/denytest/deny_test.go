package denytest

import (
	"context"
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"google.golang.org/api/compute/v1"
)

const (
	denyFirewallDisabled = false
	denyFirewallEnabled  = true
	nodeName             = "kluster-nodepool-node-123"
	denyIPv4Name         = ""
	denyIPv6Name         = denyIPv4Name + "-ipv6"
	ipv4                 = "1.2.3.4"
	ipv6                 = "1:2:3:4:5:6::"
	ipv6Range            = "1:2:3:4:5:6::/96"
)

func TestDenyFirewall(t *testing.T) {
	startStates := []struct {
		desc string
		// svc  *api_v1.Service
	}{
		{
			desc: "nothing",
		},
	}
	endStates := []struct {
		desc string
		// svc  *api_v1.Service
		want map[string]*compute.Firewall
	}{
		{
			desc: "tcp_ipv4",
		},
		{
			desc: "tcp_ipv6",
		},
		{
			desc: "tcp_dual_stack",
		},
		{
			desc: "mixed_ipv4",
		},
		{
			desc: "mixed_ipv6",
		},
		{
			desc: "mixed_dual_stack",
		},
	}

	for _, start := range startStates {
		for _, end := range endStates {
			t.Run(start.desc+"_to_"+end.desc, func(t *testing.T) {

			})
		}
	}
}

// TestDenyRollforwardDoesNotBlockTraffic verifies that the deny firewall is created only
// after the allow has already been moved to different priority. If the order is different,
// specifically both allow and deny firewalls exist at the same priority at the same time,
// this would cause all traffic on the IP to be blocked.
func TestDenyRollforwardDoesNotBlockTraffi(t *testing.T) {
	// Arrange
	// Provision a LB without deny rules
	// We use dual stack as it's testing both IP stacks at the same time
	ctx := t.Context()
	svc := helperService()
	cloud, err := helperCloud(ctx)
	if err != nil {
		t.Fatal(err)
	}
	log := klog.TODO()
	l4netlbDenyDisabled := helperL4NetLB(cloud, log, svc, denyFirewallDisabled)

	res := l4netlbDenyDisabled.EnsureFrontend([]string{nodeName}, svc)
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// Assert
	// With flag disabled there should not be any deny rules
	for annotation, name := range map[string]string{
		annotations.FirewallRuleDenyKey:     denyIPv4Name,
		annotations.FirewallRuleDenyIPv6Key: denyIPv6Name,
	} {
		if _, ok := res.Annotations[annotation]; ok {
			t.Fatalf("want no deny firewall annotations, but got %+v", res.Annotations)
		}
		// we don't want to rely on annotation to check for resource
		if _, err := cloud.GetFirewall(name); err == nil || !utils.IsNotFoundError(err) {
			t.Fatalf("want no deny firewall resource, but found %s, or error %v", name, err)
		}
	}

	// Check that the default firewalls are at priority 1000
	for _, key := range []string{annotations.FirewallRuleIPv6Key, annotations.FirewallRuleKey} {
		name, ok := res.Annotations[key]
		if !ok {
			t.Fatalf("want allow firewall annotation, but got %+v", res.Annotations)
		}
		fw, err := cloud.GetFirewall(name)
		if err != nil {
			t.Fatal(err)
		}
		if fw.Priority != 1000 {
			t.Fatalf("want allow priority 1000 before fix, but got %d", fw.Priority)
		}
	}

	// Act
	l4netlbDenyEnabled := helperL4NetLB(cloud, log, svc, denyFirewallEnabled)
	res = l4netlbDenyEnabled.EnsureFrontend([]string{nodeName}, svc)
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// Assert
	// Deny rules are created at priority 1000
	for _, key := range []string{annotations.FirewallRuleDenyKey, annotations.FirewallRuleDenyIPv6Key} {
		name, ok := res.Annotations[key]
		if !ok {
			t.Fatalf("want deny firewall annotation, but got %+v", res.Annotations)
		}
		fw, err := cloud.GetFirewall(name)
		if err != nil {
			t.Fatal(err)
		}
		if fw.Priority != 1000 {
			t.Fatalf("want deny priority 1000 after fix, but got %d", fw.Priority)
		}
	}

	// Allow rules were moved to priority 999
	for _, key := range []string{annotations.FirewallRuleIPv6Key, annotations.FirewallRuleKey} {
		name, ok := res.Annotations[key]
		if !ok {
			t.Fatalf("want allow firewall annotation, but got %+v", res.Annotations)
		}
		fw, err := cloud.GetFirewall(name)
		if err != nil {
			t.Fatal(err)
		}
		if fw.Priority != 999 {
			t.Fatalf("want allow priority 999 after fix, but got %d", fw.Priority)
		}
	}
}

// TestDenyRollback verifies that the firewalls are cleaned up and modified in the correct order.
// The worst case scenario is when the deny and allow firewalls both exist at the same priority.
func TestDenyRollback(t *testing.T) {

}

func helperL4NetLB(cloud *gce.Cloud, log klog.Logger, svc *v1.Service, denyFirewall bool) *loadbalancers.L4NetLB {
	return loadbalancers.NewL4NetLB(&loadbalancers.L4NetLBParams{
		Service:          svc,
		UseDenyFirewalls: denyFirewall,
		// other parameters
		Cloud:               cloud,
		Namer:               namer.NewL4Namer("ks123", namer.NewNamer("", "", log)),
		Recorder:            record.NewFakeRecorder(100),
		NetworkResolver:     network.NewFakeResolver(network.DefaultNetwork(cloud)),
		DualStackEnabled:    true,
		EnableMixedProtocol: true,
	}, log)
}

func helperService() *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-lb",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			LoadBalancerClass: ptr.To(annotations.RegionalExternalLoadBalancerClass),
			Ports: []v1.ServicePort{
				{Name: "tcp-80", Protocol: v1.ProtocolTCP, Port: 80},
				{Name: "udp-1000", Protocol: v1.ProtocolUDP, Port: 1000},
			},
			Type:           v1.ServiceTypeLoadBalancer,
			IPFamilies:     []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			IPFamilyPolicy: ptr.To(v1.IPFamilyPolicyRequireDualStack),
		},
	}
}

func helperCloud(ctx context.Context) (*gce.Cloud, error) {
	vals := gce.DefaultTestClusterValues()
	gce := gce.NewFakeGCECloud(vals)

	firewallTracker := &firewallTracker{}
	mockGCE := gce.Compute().(*cloud.MockGCE)

	mockGCE.MockFirewalls.DeleteHook = func(ctx context.Context, key *meta.Key, m *cloud.MockFirewalls, options ...cloud.Option) (bool, error) {
		firewallTracker.delete(key.Name)
		return false, nil
	}
	mockGCE.MockFirewalls.PatchHook = func(ctx context.Context, key *meta.Key, obj *compute.Firewall, m *cloud.MockFirewalls, options ...cloud.Option) error {
		return errors.Join(
			mock.UpdateFirewallHook(ctx, key, obj, m, options...),
			firewallTracker.patch(obj),
		)
	}
	mockGCE.MockFirewalls.UpdateHook = mockGCE.MockFirewalls.PatchHook
	mockGCE.MockFirewalls.InsertHook = func(ctx context.Context, key *meta.Key, obj *compute.Firewall, m *cloud.MockFirewalls, options ...cloud.Option) (bool, error) {
		return false, firewallTracker.patch(obj)
	}

	mockGCE.MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	mockGCE.MockRegionBackendServices.PatchHook = mock.UpdateRegionBackendServiceHook

	// Mocks by default don't add addresses like real GCE API
	mockGCE.MockForwardingRules.InsertHook = func(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules, options ...cloud.Option) (bool, error) {
		m.Lock.Lock()
		defer m.Lock.Unlock()

		if obj.IPAddress != "" {
			return false, nil
		}

		obj.IPAddress = ipv4
		if obj.IpVersion == "IPV6" {
			obj.IPAddress = ipv6Range
		}

		return false, nil
	}

	if err := gce.InsertInstance(vals.ProjectID, vals.ZoneName, &compute.Instance{
		Name: nodeName,
		Tags: &compute.Tags{Items: []string{nodeName}},
	}); err != nil {
		return nil, err
	}

	dualStackSubnetwork := &compute.Subnetwork{
		StackType:      "IPV4_IPV6",
		Ipv6AccessType: "EXTERNAL",
	}
	if err := gce.Compute().Subnetworks().Insert(ctx, meta.RegionalKey("", vals.Region), dualStackSubnetwork); err != nil {
		return nil, err
	}

	return gce, nil
}
