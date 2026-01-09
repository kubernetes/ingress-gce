// Package mixedprotocoltest contains common helpers for testing mixed protocol L4 load balancers
package mixedprotocoltest

import (
	"fmt"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/healthchecksprovider"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// Name for fake GCE resource
const (
	ForwardingRuleTCPIPv4Name    = "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i"
	ForwardingRuleTCPIPv6Name    = "k8s2-tcp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"
	ForwardingRuleUDPIPv4Name    = "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i"
	ForwardingRuleUDPIPv6Name    = "k8s2-udp-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"
	ForwardingRuleL3IPv4Name     = "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i"
	ForwardingRuleL3IPv6Name     = "k8s2-l3-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"
	ForwardingRuleLegacyName     = "a" + TestUID
	ForwardingRuleLegacyIPv6Name = "a" + TestUID + "-ipv6"
	BackendServiceName           = "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"
	FirewallIPv4Name             = "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i"
	FirewallIPv6Name             = "k8s2-axyqjz2d-test-namespace-test-name-yuvhdy7i-ipv6"
	HealthCheckFirewallIPv4Name  = "k8s2-axyqjz2d-l4-shared-hc-fw"
	HealthCheckFirewallIPv6Name  = "k8s2-axyqjz2d-l4-shared-hc-fw-ipv6"
	HealthCheckName              = "k8s2-axyqjz2d-l4-shared-hc"
)

// GCEResources collects all GCE resources that are managed by the mixed protocol LBs
type GCEResources struct {
	ForwardingRules map[string]*compute.ForwardingRule
	Firewalls       map[string]*compute.Firewall
	HealthCheck     *composite.HealthCheck
	BackendService  *compute.BackendService
}

// Create creates resources in the specified cloud
func (r GCEResources) Create(cloud *gce.Cloud) error {
	if r.BackendService != nil {
		if err := cloud.CreateRegionBackendService(r.BackendService, cloud.Region()); err != nil {
			return fmt.Errorf("fakeGCE.CreateBackendService() returned error %w", err)
		}
	}

	if r.HealthCheck != nil {
		provider := healthchecksprovider.NewHealthChecks(cloud, meta.VersionGA, klog.TODO())
		if err := provider.Create(r.HealthCheck); err != nil {
			return fmt.Errorf("healthCheckProvider.Create() returned error %w", err)
		}
	}

	for _, fw := range r.Firewalls {
		if err := cloud.CreateFirewall(fw); err != nil {
			return fmt.Errorf("fakeGCE.CreateFirewall() returned error %w", err)
		}
	}

	for _, fr := range r.ForwardingRules {
		if err := cloud.CreateRegionForwardingRule(fr, cloud.Region()); err != nil {
			return fmt.Errorf("fakeGCE.CreateForwardingRule() returned error %w", err)
		}
	}

	return nil
}

// VerifyResourcesExist checks if the resources are in the cloud.
// We use testing.T to return multiple nicely formatted errors.
func VerifyResourcesExist(t *testing.T, cloud *gce.Cloud, want GCEResources) {
	t.Helper()

	for frName, wantFr := range want.ForwardingRules {
		fr, err := cloud.GetRegionForwardingRule(frName, wantFr.Region)
		if err != nil {
			t.Errorf("cloud.GetForwardingRule(%v) returned error %v", frName, err)
		}
		ignoreFields := cmpopts.IgnoreFields(compute.ForwardingRule{}, "SelfLink")
		if diff := cmp.Diff(wantFr, fr, ignoreFields); diff != "" {
			t.Errorf("Forwarding rule mismatch (-want +got):\n%s", diff)
		}
	}

	for fwName, wantFw := range want.Firewalls {
		fw, err := cloud.GetFirewall(fwName)
		if err != nil {
			t.Errorf("cloud.GetFirewall(%v) returned error %v", fwName, err)
		}
		ignoreFields := cmpopts.IgnoreFields(compute.Firewall{}, "SelfLink")
		sortSourceRanges := cmpopts.SortSlices(func(x, y string) bool { return x < y })
		if diff := cmp.Diff(wantFw, fw, ignoreFields, sortSourceRanges); diff != "" {
			t.Errorf("Firewall mismatch (-want +got):\n%s", diff)
		}
	}

	if want.HealthCheck != nil {
		provider := healthchecksprovider.NewHealthChecks(cloud, meta.VersionGA, klog.TODO())
		hc, err := provider.Get(want.HealthCheck.Name, want.HealthCheck.Scope)
		if err != nil {
			t.Errorf("healthCheckProvider.Get(%v) got err: %v", want.HealthCheck.Name, err)
		}
		ignoreFields := cmpopts.IgnoreFields(composite.HealthCheck{}, "SelfLink", "Scope")
		if diff := cmp.Diff(want.HealthCheck, hc, ignoreFields); diff != "" {
			t.Errorf("Health check mismatch (-want +got):\n%s", diff)
		}
	}

	if want.BackendService != nil {
		bs, err := cloud.GetRegionBackendService(want.BackendService.Name, want.BackendService.Region)
		if err != nil {
			t.Errorf("cloud.GetBackendService() returned error %v", err)
		}
		ignoreFields := cmpopts.IgnoreFields(compute.BackendService{}, "SelfLink", "ConnectionDraining", "Region")
		if diff := cmp.Diff(want.BackendService, bs, ignoreFields); diff != "" {
			t.Errorf("Backend service mismatch (-want +got):\n%s", diff)
		}
	}
}

// VerifyResourcesCleanedUp checks if the old resources are removed from the cloud.
// We use testing.T to return multiple nicely formatted errors.
// We don't need to clean up firewall for health checks, since they are shared
func VerifyResourcesCleanedUp(t *testing.T, cloud *gce.Cloud, old, want GCEResources) {
	t.Helper()

	for frName := range old.ForwardingRules {
		if _, ok := want.ForwardingRules[frName]; ok {
			continue
		}
		fr, err := cloud.GetRegionForwardingRule(frName, cloud.Region())
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("cloud.GetForwardingRule() returned error %v", err)
		}
		if fr != nil {
			t.Errorf("found forwarding rule %v, which should have been deleted", fr.Name)
		}
	}

	for fwName := range old.Firewalls {
		isShared := fwName == HealthCheckFirewallIPv4Name || fwName == HealthCheckFirewallIPv6Name
		if isShared {
			continue
		}
		if _, ok := want.Firewalls[fwName]; ok {
			continue
		}
		fw, err := cloud.GetFirewall(fwName)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("cloud.GetFirewall() returned error %v", err)
		}
		if fw != nil {
			t.Errorf("found firewall %v, which should have been deleted", fw.Name)
		}
	}

	if old.HealthCheck != nil && (want.HealthCheck == nil || want.HealthCheck.Name != old.HealthCheck.Name) {
		hc, err := cloud.GetHealthCheck(old.HealthCheck.Name)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("cloud.GetHealthCheck() returned error %v", err)
		}
		if hc != nil {
			t.Errorf("found health check %v, which should have been deleted", hc.Name)
		}
	}

	if old.BackendService != nil && (want.BackendService == nil || want.BackendService.Name != old.BackendService.Name) {
		bs, err := cloud.GetRegionBackendService(old.BackendService.Name, old.BackendService.Region)
		if utils.IgnoreHTTPNotFound(err) != nil {
			t.Errorf("cloud.GetBackendService() returned error %v", err)
		}
		if bs != nil {
			t.Errorf("found backend service %v, which should have been deleted", bs.Name)
		}
	}
}

// HealthCheckFirewall returns Firewall for HealthCheck
func HealthCheckFirewall() *compute.Firewall {
	return &compute.Firewall{
		Name:    HealthCheckFirewallIPv4Name,
		Allowed: []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{"10256"}}},
		// GCE defined health check ranges
		SourceRanges: []string{"130.211.0.0/22", "35.191.0.0/16", "209.85.152.0/22", "209.85.204.0/22"},
		TargetTags:   []string{TestNode},
		Description:  `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4  Services using ExternalTrafficPolicy: Cluster."}`,
		Priority:     999,
	}
}

// HealthCheckFirewallIPv6 returns Firewall for HealthCheck
func HealthCheckFirewallIPv6() *compute.Firewall {
	return &compute.Firewall{
		Name:    HealthCheckFirewallIPv6Name,
		Allowed: []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{"10256"}}},
		// GCE defined health check ranges
		SourceRanges: []string{"2600:2d00:1:b029::/64"},
		TargetTags:   []string{TestNode},
		Description:  `{"networking.gke.io/service-name":"","networking.gke.io/api-version":"ga","networking.gke.io/resource-description":"This resource is shared by all L4  Services using ExternalTrafficPolicy: Cluster."}`,
		Priority:     999,
	}
}

// Firewall returns ILB Firewall with specified allowed rules
func Firewall(allowed []*compute.FirewallAllowed) *compute.Firewall {
	return &compute.Firewall{
		Name:         FirewallIPv4Name,
		Allowed:      allowed,
		Description:  `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
		SourceRanges: []string{"0.0.0.0/0"},
		TargetTags:   []string{TestNode},
		Priority:     1000,
	}
}

// FirewallIPv6 returns ILB Firewall with specified allowed rules
func FirewallIPv6(allowed []*compute.FirewallAllowed) *compute.Firewall {
	return &compute.Firewall{
		Name:         FirewallIPv6Name,
		Allowed:      allowed,
		Description:  `{"networking.gke.io/service-name":"test-namespace/test-name","networking.gke.io/api-version":"ga"}`,
		SourceRanges: []string{"0::0/0"},
		TargetTags:   []string{TestNode},
		Priority:     1000,
	}
}
