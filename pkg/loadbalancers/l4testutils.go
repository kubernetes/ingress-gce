package loadbalancers

import (
	"fmt"
	"sort"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func verifyFirewallNotExists(cloud *gce.Cloud, fwName string) error {
	firewall, err := cloud.GetFirewall(fwName)
	if !utils.IsNotFoundError(err) || firewall != nil {
		return fmt.Errorf("firewall %s exists, expected not", fwName)
	}
	return nil
}

func verifyForwardingRuleNotExists(cloud *gce.Cloud, frName string) error {
	fwdRule, err := cloud.GetRegionForwardingRule(frName, cloud.Region())
	if !utils.IsNotFoundError(err) || fwdRule != nil {
		return fmt.Errorf("forwarding rule %s exists, expected not", frName)
	}
	return nil
}

func verifyHealthCheckNotExists(cloud *gce.Cloud, hcName string, scope meta.KeyType) error {
	key := meta.GlobalKey(hcName)
	if scope == meta.Regional {
		key = meta.RegionalKey(hcName, cloud.Region())
	}
	healthCheck, err := composite.GetHealthCheck(cloud, key, meta.VersionGA)
	if !utils.IsNotFoundError(err) || healthCheck != nil {
		return fmt.Errorf("health check %s exists, expected not", hcName)
	}
	return nil
}

func verifyBackendServiceNotExists(cloud *gce.Cloud, bsName string) error {
	bs, err := cloud.GetRegionBackendService(bsName, cloud.Region())
	if !utils.IsNotFoundError(err) || bs != nil {
		return fmt.Errorf("backend service %s exists, expected not", bsName)
	}
	return nil
}

func verifyAddressNotExists(cloud *gce.Cloud, addressName string) error {
	addr, err := cloud.GetRegionAddress(addressName, cloud.Region())
	if !utils.IsNotFoundError(err) || addr != nil {
		return fmt.Errorf("address %s exists, expected not", addressName)
	}
	return nil
}

func verifyFirewall(cloud *gce.Cloud, nodeNames []string, firewallName, expectedDescription string, expectedSourceRanges []string) error {
	firewall, err := cloud.GetFirewall(firewallName)
	if err != nil {
		return fmt.Errorf("failed to fetch firewall rule %q - err %w", firewallName, err)
	}
	if !utils.EqualStringSets(nodeNames, firewall.TargetTags) {
		return fmt.Errorf("expected firewall rule target tags '%v', Got '%v'", nodeNames, firewall.TargetTags)
	}
	sort.Strings(expectedSourceRanges)
	sort.Strings(firewall.SourceRanges)
	if diff := cmp.Diff(expectedSourceRanges, firewall.SourceRanges); diff != "" {
		return fmt.Errorf("expected firewall source ranges %v, got %v, diff %v", expectedSourceRanges, firewall.SourceRanges, diff)
	}
	if firewall.Description != expectedDescription {
		return fmt.Errorf("unexpected description in firewall %q - Expected %s, Got %s", firewallName, expectedDescription, firewall.Description)
	}
	return nil
}
