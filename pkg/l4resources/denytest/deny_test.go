// Package denytest verifies deny firewall functionality
package denytest

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4annotations"
	"k8s.io/ingress-gce/pkg/l4lb/metrics"
	"k8s.io/ingress-gce/pkg/l4resources"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
)

const (
	denyFirewallDisabled = false
	denyFirewallEnabled  = true
	nodeName             = "kluster-nodepool-node-123"
	allowIPv4Name        = "k8s2-h0zmu0xg-default-external-lb-2dkyewnt"
	allowIPv6Name        = allowIPv4Name + "-ipv6"
	denyIPv4Name         = "k8s2-h0zmu0xg-default-external-lb-2dkyewnt-deny"
	denyIPv6Name         = denyIPv4Name + "-ipv6"
	ipv4Address          = "1.2.3.4"
	ipv6Address          = "1:2:3:4:5:6::"
	ipv6Range            = "1:2:3:4:5:6::/96"
)

func TestDenyFirewall(t *testing.T) {
	var oldFlag bool
	flags.F.EnablePinhole, oldFlag = true, flags.F.EnablePinhole
	defer func() { flags.F.EnablePinhole = oldFlag }()

	fwCmpOpt := cmpopts.IgnoreFields(compute.Firewall{}, "SelfLink")

	type annotationKey string
	type resourceName string

	ipv4AllowFirewall := &compute.Firewall{
		Name: allowIPv4Name,
		Allowed: []*compute.FirewallAllowed{
			{IPProtocol: "udp", Ports: []string{"1000"}},
			{IPProtocol: "tcp", Ports: []string{"80"}},
		},
		Description:       `{"networking.gke.io/service-name":"default/external-lb","networking.gke.io/api-version":"ga"}`,
		DestinationRanges: []string{ipv4Address},
		SourceRanges:      []string{"0.0.0.0/0"},
		TargetTags:        []string{nodeName},
		Priority:          999,
	}
	ipv4DenyFirewall := &compute.Firewall{
		Name:              denyIPv4Name,
		Denied:            []*compute.FirewallDenied{{IPProtocol: "all"}},
		Description:       `{"networking.gke.io/service-name":"default/external-lb","networking.gke.io/api-version":"ga"}`,
		DestinationRanges: []string{ipv4Address},
		SourceRanges:      []string{"0.0.0.0/0"},
		TargetTags:        []string{nodeName},
		Priority:          1000,
	}
	ipv6AllowFirewall := &compute.Firewall{
		Name: allowIPv6Name,
		Allowed: []*compute.FirewallAllowed{
			// {IPProtocol: "udp", Ports: []string{"1000"}},
			{IPProtocol: "TCP", Ports: []string{"80", "1000"}}, // mixed protocol is not yet supported on IPv6
		},
		Description:       `{"networking.gke.io/service-name":"default/external-lb","networking.gke.io/api-version":"ga"}`,
		DestinationRanges: []string{ipv6Address},
		SourceRanges:      []string{"0::0/0"},
		TargetTags:        []string{nodeName},
		Priority:          999,
	}
	ipv6DenyFirewall := &compute.Firewall{
		Name:              denyIPv6Name,
		Denied:            []*compute.FirewallDenied{{IPProtocol: "all"}},
		Description:       `{"networking.gke.io/service-name":"default/external-lb","networking.gke.io/api-version":"ga"}`,
		DestinationRanges: []string{ipv6Range},
		SourceRanges:      []string{"::/0"},
		TargetTags:        []string{nodeName},
		Priority:          1000,
	}

	startStates := []struct {
		desc string
		svc  *v1.Service
	}{
		{
			desc: "nothing",
		},
		{
			desc: "ipv4",
			svc:  helperService([]v1.IPFamily{v1.IPv4Protocol}),
		},
		{
			desc: "ipv6",
			svc:  helperService([]v1.IPFamily{v1.IPv6Protocol}),
		},
		{
			desc: "dual_stack",
			svc: helperService([]v1.IPFamily{
				v1.IPv4Protocol,
				v1.IPv6Protocol,
			}),
		},
	}
	endStates := []struct {
		desc     string
		svc      *v1.Service
		want     map[annotationKey]*compute.Firewall
		dontWant map[annotationKey]resourceName
	}{
		{
			desc: "ipv4",
			svc:  helperService([]v1.IPFamily{v1.IPv4Protocol}),
			want: map[annotationKey]*compute.Firewall{
				l4annotations.FirewallRuleKey:     ipv4AllowFirewall,
				l4annotations.FirewallRuleDenyKey: ipv4DenyFirewall,
			},
			dontWant: map[annotationKey]resourceName{
				l4annotations.FirewallRuleIPv6Key:     allowIPv6Name,
				l4annotations.FirewallRuleDenyIPv6Key: denyIPv6Name,
			},
		},
		{
			desc: "ipv6",
			svc:  helperService([]v1.IPFamily{v1.IPv6Protocol}),
			want: map[annotationKey]*compute.Firewall{
				l4annotations.FirewallRuleIPv6Key:     ipv6AllowFirewall,
				l4annotations.FirewallRuleDenyIPv6Key: ipv6DenyFirewall,
			},
			dontWant: map[annotationKey]resourceName{
				l4annotations.FirewallRuleKey:     allowIPv4Name,
				l4annotations.FirewallRuleDenyKey: denyIPv4Name,
			},
		},
		{
			desc: "dual_stack",
			svc:  helperService([]v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol}),
			want: map[annotationKey]*compute.Firewall{
				l4annotations.FirewallRuleKey:         ipv4AllowFirewall,
				l4annotations.FirewallRuleDenyKey:     ipv4DenyFirewall,
				l4annotations.FirewallRuleIPv6Key:     ipv6AllowFirewall,
				l4annotations.FirewallRuleDenyIPv6Key: ipv6DenyFirewall,
			},
		},
	}

	for _, start := range startStates {
		for _, end := range endStates {
			t.Run(start.desc+"_to_"+end.desc, func(t *testing.T) {
				// Arrange
				ctx := t.Context()
				cloud, err := helperCloud(ctx)
				if err != nil {
					t.Fatal(err)
				}
				log := klog.TODO()
				if start.svc != nil {
					l4netlb := helperL4NetLB(cloud, log, start.svc, denyFirewallEnabled)
					res := l4netlb.EnsureFrontend([]string{nodeName}, start.svc, time.Now())
					if res.Error != nil {
						t.Fatal(res.Error)
					}
				}

				// Act
				l4netlb := helperL4NetLB(cloud, log, end.svc, denyFirewallEnabled)
				res := l4netlb.EnsureFrontend([]string{nodeName}, end.svc, time.Now())

				// Assert
				if res.Error != nil {
					t.Fatal(res.Error)
				}

				fws, err := firewalls(ctx, cloud)
				if err != nil {
					t.Fatal(err)
				}

				for annotation, fw := range end.want {
					if got := res.Annotations[string(annotation)]; got != fw.Name {
						t.Errorf("want annotations[%q] = %q, but got %q", annotation, fw.Name, got)
					}

					got := fws[fw.Name]
					if diff := cmp.Diff(fw, got, fwCmpOpt); diff != "" {
						t.Errorf("got != want (-want, +got)\n%s", diff)
					}
				}

				// Act
				res = l4netlb.EnsureLoadBalancerDeleted(end.svc)

				// Assert
				if res.Error != nil {
					t.Fatal(res.Error)
				}

				fws, err = firewalls(ctx, cloud)
				if err != nil {
					t.Fatal(err)
				}

				for _, fw := range []string{allowIPv4Name, allowIPv6Name, denyIPv4Name, denyIPv6Name} {
					if _, ok := fws[fw]; ok {
						t.Errorf("leaked firewall %q", fw)
					}
				}
			})
		}
	}
}

func firewalls(ctx context.Context, cloud *gce.Cloud) (map[string]*compute.Firewall, error) {
	firewalls, err := cloud.Compute().Firewalls().List(ctx, filter.None)
	if err != nil {
		return nil, err
	}
	firewallsMap := make(map[string]*compute.Firewall)
	for _, fw := range firewalls {
		firewallsMap[fw.Name] = fw
	}
	return firewallsMap, nil

}

func TestDenyIsNotCreatedWhenAllowPriorityUpdateFails(t *testing.T) {
	for _, ipFamily := range []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol} {
		t.Run(string(ipFamily), func(t *testing.T) {
			// Arrange
			ctx := t.Context()
			svc := helperService([]v1.IPFamily{ipFamily})
			gce, err := helperCloud(ctx)
			if err != nil {
				t.Fatal(err)
			}
			log := klog.TODO()

			l4netlb := helperL4NetLB(gce, log, svc, denyFirewallDisabled)
			res := l4netlb.EnsureFrontend([]string{nodeName}, svc, time.Now())
			if res.Error != nil {
				t.Fatal(res.Error)
			}

			// On the next allow call inject an error
			mockGCE := gce.Compute().(*cloud.MockGCE)
			wasGCECalled := false
			injectedError := errors.New("injected error on allow patch")
			prevHook := mockGCE.MockFirewalls.PatchHook
			mockGCE.MockFirewalls.PatchHook = func(ctx context.Context, k *meta.Key, f *compute.Firewall, mf *cloud.MockFirewalls, o ...cloud.Option) error {
				if f.Name == allowIPv6Name || f.Name == allowIPv4Name {
					wasGCECalled = true
					return injectedError
				}
				return prevHook(ctx, k, f, mf, o...)
			}

			// Act
			l4netlb = helperL4NetLB(gce, log, svc, denyFirewallEnabled)
			res = l4netlb.EnsureFrontend([]string{nodeName}, svc, time.Now())

			// Assert
			if wasGCECalled == false {
				t.Fatal("the mocked call was not called, even though there was supposed to be an update")
			}

			if !errors.Is(res.Error, injectedError) {
				t.Errorf("got unexpected err %q, wanted %q", res.Error, injectedError)
			}

			// check that no deny rules were created
			for _, name := range []string{denyIPv4Name, denyIPv6Name} {
				if _, err := gce.GetFirewall(name); err == nil || !utils.IsNotFoundError(err) {
					t.Fatalf("want no deny firewall resource, but found %q or error %q", name, err)
				}
			}
		})
	}
}

func TestDenyRespectsDisableNodeFirewallProvisioning(t *testing.T) {
	// Arrange
	ctx := t.Context()
	svc := helperService([]v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol})
	cloud, err := helperCloud(ctx)
	if err != nil {
		t.Fatal(err)
	}
	log := klog.TODO()
	l4netlb := l4resources.NewL4NetLB(&l4resources.L4NetLBParams{
		Service:                            svc,
		UseDenyFirewalls:                   true,
		EnableDenyFirewallsRollbackCleanup: true,
		DisableNodesFirewallProvisioning:   true,
		// other parameters
		Cloud:               cloud,
		Namer:               namer.NewL4Namer("ks123", namer.NewNamer("", "", log)),
		Recorder:            record.NewFakeRecorder(100),
		NetworkResolver:     network.NewFakeResolver(network.DefaultNetwork(cloud)),
		DualStackEnabled:    true,
		EnableMixedProtocol: true,
	}, log)

	// Act
	res := l4netlb.EnsureFrontend([]string{nodeName}, svc, time.Now())

	// Assert
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// With DisableNodesFirewallProvisioning set to true there should not be any deny rules
	for annotation, name := range map[string]string{
		l4annotations.FirewallRuleDenyKey:     denyIPv4Name,
		l4annotations.FirewallRuleDenyIPv6Key: denyIPv6Name,
	} {
		if _, ok := res.Annotations[annotation]; ok {
			t.Fatalf("want no deny firewall annotations, but got %+v", res.Annotations)
		}
		// we don't want to rely on annotation to check for resource
		if _, err := cloud.GetFirewall(name); err == nil || !utils.IsNotFoundError(err) {
			t.Fatalf("want no deny firewall resource, but found %s, or error %v", name, err)
		}
	}

}

// TestDenyRollforwardDoesNotBlockTraffic verifies that the deny firewall is created only
// after the allow has already been moved to different priority. If the order is different,
// specifically both allow and deny firewalls exist at the same priority at the same time,
// this would cause all traffic on the IP to be blocked.
func TestDenyRollforwardDoesNotBlockTraffic(t *testing.T) {
	// Arrange
	// Provision a LB without deny rules
	// We use dual stack as it's testing both IP stacks at the same time
	ctx := t.Context()
	svc := helperService([]v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol})
	cloud, err := helperCloud(ctx)
	if err != nil {
		t.Fatal(err)
	}
	log := klog.TODO()
	l4netlbDenyDisabled := helperL4NetLB(cloud, log, svc, denyFirewallDisabled)

	var oldFlag bool
	flags.F.EnablePinhole, oldFlag = true, flags.F.EnablePinhole
	defer func() { flags.F.EnablePinhole = oldFlag }()

	// Act
	res := l4netlbDenyDisabled.EnsureFrontend([]string{nodeName}, svc, time.Now())
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// Assert
	// With flag disabled there should not be any deny rules
	for annotation, name := range map[string]string{
		l4annotations.FirewallRuleDenyKey:     denyIPv4Name,
		l4annotations.FirewallRuleDenyIPv6Key: denyIPv6Name,
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
	for _, key := range []string{l4annotations.FirewallRuleIPv6Key, l4annotations.FirewallRuleKey} {
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
	res = l4netlbDenyEnabled.EnsureFrontend([]string{nodeName}, svc, time.Now())
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// Assert
	// Deny rules are created at priority 1000
	for _, key := range []string{l4annotations.FirewallRuleDenyKey, l4annotations.FirewallRuleDenyIPv6Key} {
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
	for _, key := range []string{l4annotations.FirewallRuleIPv6Key, l4annotations.FirewallRuleKey} {
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
	// Arrange
	// Provision a LB with deny rules
	ctx := t.Context()
	svc := helperService([]v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol})
	cloud, err := helperCloud(ctx)
	if err != nil {
		t.Fatal(err)
	}
	log := klog.TODO()
	l4netlbDenyEnabled := helperL4NetLB(cloud, log, svc, denyFirewallEnabled)

	var oldFlag bool
	flags.F.EnablePinhole, oldFlag = true, flags.F.EnablePinhole
	defer func() { flags.F.EnablePinhole = oldFlag }()

	// Act
	res := l4netlbDenyEnabled.EnsureFrontend([]string{nodeName}, svc, time.Now())

	// Assert
	// No errors, including deny firewall blocking whole allow
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// Deny rules are created at priority 1000
	for _, key := range []string{l4annotations.FirewallRuleDenyKey, l4annotations.FirewallRuleDenyIPv6Key} {
		name, ok := res.Annotations[key]
		if !ok {
			t.Fatalf("want deny firewall annotation %v, but got %+v", key, res.Annotations)
		}
		fw, err := cloud.GetFirewall(name)
		if err != nil {
			t.Fatal(err)
		}
		if fw.Priority != 1000 {
			t.Fatalf("want deny priority 1000 after fix, but got %d", fw.Priority)
		}
	}

	// Allow rules are created with priority 999
	for _, key := range []string{l4annotations.FirewallRuleIPv6Key, l4annotations.FirewallRuleKey} {
		name, ok := res.Annotations[key]
		if !ok {
			t.Fatalf("want allow firewall annotation %v, but got %+v", key, res.Annotations)
		}
		fw, err := cloud.GetFirewall(name)
		if err != nil {
			t.Fatal(err)
		}
		if fw.Priority != 999 {
			t.Fatalf("want allow priority 999 after fix, but got %d", fw.Priority)
		}
	}

	// Act
	l4netlbDenyDisabled := helperL4NetLB(cloud, log, svc, denyFirewallDisabled)
	res = l4netlbDenyDisabled.EnsureFrontend([]string{nodeName}, svc, time.Now())

	// Assert
	// No errors, including deny firewall blocking whole allow
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// Deny rules are cleaned up
	for annotation, name := range map[string]string{
		l4annotations.FirewallRuleDenyKey:     denyIPv4Name,
		l4annotations.FirewallRuleDenyIPv6Key: denyIPv6Name,
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
	for _, key := range []string{l4annotations.FirewallRuleIPv6Key, l4annotations.FirewallRuleKey} {
		name, ok := res.Annotations[key]
		if !ok {
			t.Fatalf("want allow firewall annotation %v, but got %+v", key, res.Annotations)
		}
		fw, err := cloud.GetFirewall(name)
		if err != nil {
			t.Fatal(err)
		}
		if fw.Priority != 1000 {
			t.Fatalf("want allow priority 1000 before fix, but got %d", fw.Priority)
		}
	}
}

// TestExportsCorrectDenyMetrics verifies that correct label are exported given the IP stack, errors and feature being enabled.
func TestExportsCorrectDenyMetrics(t *testing.T) {
	var oldFlag bool
	flags.F.EnablePinhole, oldFlag = true, flags.F.EnablePinhole
	defer func() { flags.F.EnablePinhole = oldFlag }()

	testCases := []struct {
		desc        string
		ipFamilies  []v1.IPFamily
		denyEnabled bool
		fwErrors    bool
		want        metrics.DenyFirewallStatus
	}{
		{
			desc:        "ipv4",
			ipFamilies:  []v1.IPFamily{v1.IPv4Protocol},
			denyEnabled: true,
			want:        metrics.DenyFirewallStatusIPv4,
		},
		{
			desc:        "ipv6",
			ipFamilies:  []v1.IPFamily{v1.IPv6Protocol},
			denyEnabled: true,
			want:        metrics.DenyFirewallStatusIPv6,
		},
		{
			desc:        "ipv4_ipv6",
			ipFamilies:  []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			denyEnabled: true,
			want:        metrics.DenyFirewallStatusDualStack,
		},
		{
			desc:        "ipv4_disabled",
			ipFamilies:  []v1.IPFamily{v1.IPv4Protocol},
			denyEnabled: false,
			want:        metrics.DenyFirewallStatusNone,
		},
		{
			desc:        "ipv6_disabled",
			ipFamilies:  []v1.IPFamily{v1.IPv6Protocol},
			denyEnabled: false,
			want:        metrics.DenyFirewallStatusNone,
		},
		{
			desc:        "fw_error",
			ipFamilies:  []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			denyEnabled: true,
			fwErrors:    true,
			want:        metrics.DenyFirewallStatusNone,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Arrange
			t.Parallel()
			ctx := t.Context()
			svc := helperService(tc.ipFamilies)
			gce, err := helperCloud(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if tc.fwErrors {
				mockGCE := gce.Compute().(*cloud.MockGCE)
				mockGCE.MockFirewalls.InsertError = map[meta.Key]error{
					*meta.GlobalKey(denyIPv4Name): errors.New("injected error ipv4"),
					*meta.GlobalKey(denyIPv6Name): errors.New("injected error ipv6"),
				}
			}

			ensurer := helperL4NetLB(gce, klog.TODO(), svc, tc.denyEnabled)

			// Act
			res := ensurer.EnsureFrontend([]string{nodeName}, svc, time.Now())
			// We expicitly don't care for errors - only for values stored in metrics
			got := res.MetricsState.DenyFirewallStatus

			// Assert
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSkipRollbackCleanupIfDisabled is a complimentary test to TestDenyRollback which verifies
// that the cleanup logic on Ensure is skipped when the ArmDenyFirewallsRollbackCleanup is not enabled.
func TestSkipRollbackCleanupIfDisabled(t *testing.T) {
	// Arrange
	ctx := t.Context()
	svc := helperService([]v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol})

	gce, err := helperCloud(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mockGCE := gce.Compute().(*cloud.MockGCE)
	getCalled := make(map[string]bool)
	// Function that is tested will check for deny firewalls if it is enabled before actually deleting them
	mockGCE.MockFirewalls.GetHook = func(ctx context.Context, key *meta.Key, m *cloud.MockFirewalls, options ...cloud.Option) (bool, *compute.Firewall, error) {
		getCalled[key.Name] = true
		return false, nil, nil // continue with normal hook
	}

	log := klog.TODO()
	ensurer := helperL4NetLBWithoutCleanup(gce, log, svc)
	res := ensurer.EnsureFrontend([]string{nodeName}, svc, time.Now())
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	// Assert
	// No cleanup logic should be called - it is not armed
	for _, name := range []string{denyIPv4Name, denyIPv6Name} {
		if got := getCalled[name]; got {
			t.Errorf("Cleanup or other deny firewall logic for %v was executed even though the ArmDenyFirewallsRollbackCleanup and UseDenyFirewalls flags were set to false", name)
		}
		// cleanup for later test
		getCalled[name] = false
	}

	// Verify that the cleanup logic is actually performed when it needs to be
	ensurer = helperL4NetLB(gce, log, svc, denyFirewallDisabled) // cleanup is armed
	res = ensurer.EnsureFrontend([]string{nodeName}, svc, time.Now())
	if res.Error != nil {
		t.Fatal(res.Error)
	}

	for _, name := range []string{denyIPv4Name, denyIPv6Name} {
		if got := getCalled[name]; !got {
			t.Errorf("Cleanup for deny firewall %v logic has not been executed", name)
		}
	}
}

func helperL4NetLB(cloud *gce.Cloud, log klog.Logger, svc *v1.Service, denyFirewall bool) *l4resources.L4NetLB {
	return l4resources.NewL4NetLB(&l4resources.L4NetLBParams{
		Service:                            svc,
		UseDenyFirewalls:                   denyFirewall,
		EnableDenyFirewallsRollbackCleanup: true,
		// other parameters
		Cloud:               cloud,
		Namer:               namer.NewL4Namer("ks123", namer.NewNamer("", "", log)),
		Recorder:            record.NewFakeRecorder(100),
		NetworkResolver:     network.NewFakeResolver(network.DefaultNetwork(cloud)),
		DualStackEnabled:    true,
		EnableMixedProtocol: true,
	}, log)
}

func helperL4NetLBWithoutCleanup(cloud *gce.Cloud, log klog.Logger, svc *v1.Service) *l4resources.L4NetLB {
	return l4resources.NewL4NetLB(&l4resources.L4NetLBParams{
		Service:                            svc,
		UseDenyFirewalls:                   false,
		EnableDenyFirewallsRollbackCleanup: false,
		// other parameters
		Cloud:               cloud,
		Namer:               namer.NewL4Namer("ks123", namer.NewNamer("", "", log)),
		Recorder:            record.NewFakeRecorder(100),
		NetworkResolver:     network.NewFakeResolver(network.DefaultNetwork(cloud)),
		DualStackEnabled:    true,
		EnableMixedProtocol: true,
	}, log)
}

func helperService(ipFamily []v1.IPFamily) *v1.Service {
	policy := v1.IPFamilyPolicySingleStack
	if len(ipFamily) == 2 {
		policy = v1.IPFamilyPolicyRequireDualStack
	}

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "external-lb",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			LoadBalancerClass: ptr.To(l4annotations.RegionalExternalLoadBalancerClass),
			Ports: []v1.ServicePort{
				{Name: "tcp-80", Protocol: v1.ProtocolTCP, Port: 80},
				{Name: "udp-1000", Protocol: v1.ProtocolUDP, Port: 1000},
			},
			Type:           v1.ServiceTypeLoadBalancer,
			IPFamilies:     ipFamily,
			IPFamilyPolicy: ptr.To(policy),
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
		if err := firewallTracker.patch(obj); err != nil {
			return true, err
		}
		return false, nil
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

		obj.IPAddress = ipv4Address
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
