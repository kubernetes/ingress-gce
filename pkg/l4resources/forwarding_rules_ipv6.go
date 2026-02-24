/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package l4resources

import (
	"fmt"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
	"k8s.io/ingress-gce/pkg/l4annotations"
	"k8s.io/ingress-gce/pkg/utils"

	"k8s.io/cloud-provider-gcp/providers/gce"
)

const (
	IPVersionIPv6 = "IPV6"
	prefix96range = "/96"
)

func (l4 *L4) ensureIPv6ForwardingRule(bsLink string, options gce.ILBOptions, existingIPv6FwdRule *composite.ForwardingRule, ipv6AddressToUse string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	start := time.Now()

	expectedIPv6FwdRule, err := l4.buildExpectedIPv6ForwardingRule(bsLink, options, ipv6AddressToUse)
	if err != nil {
		return nil, utils.ResourceResync, fmt.Errorf("l4.buildExpectedIPv6ForwardingRule(%s, %v, %s) returned error %w, want nil", bsLink, options, ipv6AddressToUse, err)
	}

	frLogger := l4.svcLogger.WithValues("forwardingRuleName", expectedIPv6FwdRule.Name)
	frLogger.V(2).Info("Ensuring internal ipv6 forwarding rule for L4 ILB Service", "backendServiceLink", bsLink)
	defer func() {
		frLogger.V(2).Info("Finished ensuring internal ipv6 forwarding rule for L4 ILB Service", "timeTaken", time.Since(start))
	}()

	if existingIPv6FwdRule != nil {
		equal, err := forwardingrules.EqualIPv6(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return existingIPv6FwdRule, utils.ResourceResync, err
		}
		if equal {
			frLogger.V(2).Info("ensureIPv6ForwardingRule: Skipping update of unchanged ipv6 forwarding rule")
			return existingIPv6FwdRule, utils.ResourceResync, nil
		}
		err = l4.deleteChangedIPv6ForwardingRule(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return nil, utils.ResourceUpdate, err
		}
	}

	frLogger.V(2).Info("ensureIPv6ForwardingRule: Creating/Recreating forwarding rule")
	err = l4.forwardingRules.Create(expectedIPv6FwdRule)
	if err != nil {
		return nil, utils.ResourceUpdate, err
	}

	createdFr, err := l4.forwardingRules.Get(expectedIPv6FwdRule.Name)
	return createdFr, utils.ResourceUpdate, err
}

func (l4 *L4) buildExpectedIPv6ForwardingRule(bsLink string, options gce.ILBOptions, ipv6AddressToUse string) (*composite.ForwardingRule, error) {
	frName := l4.getIPv6FRName()

	frDesc, err := utils.MakeL4IPv6ForwardingRuleDescription(l4.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to compute description for forwarding rule %s, err: %w", frName, err)
	}

	subnetworkURL := l4.cloud.SubnetworkURL()

	if options.SubnetName != "" {
		subnetworkURL, err = l4.getSubnetworkURLByName(options.SubnetName)
		if err != nil {
			return nil, err
		}
	}

	svcPorts := l4.Service.Spec.Ports
	ports := utils.GetPorts(svcPorts)
	protocol := string(utils.GetProtocol(svcPorts))
	allPorts := false
	if l4.enableMixedProtocol {
		protocol = forwardingrules.GetILBProtocol(svcPorts)
		if protocol == forwardingrules.ProtocolL3 {
			allPorts = true
			ports = nil
		}
	}
	if len(ports) > maxForwardedPorts {
		ports = nil
		allPorts = true
	}

	fr := &composite.ForwardingRule{
		Name:                frName,
		Description:         frDesc,
		IPAddress:           ipv6AddressToUse,
		IPProtocol:          protocol,
		AllPorts:            allPorts,
		Ports:               ports,
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      bsLink,
		IpVersion:           IPVersionIPv6,
		Network:             l4.cloud.NetworkURL(),
		Subnetwork:          subnetworkURL,
		AllowGlobalAccess:   options.AllowGlobalAccess,
		NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
	}

	return fr, nil
}

func (l4 *L4) deleteChangedIPv6ForwardingRule(existingFwdRule *composite.ForwardingRule, expectedFwdRule *composite.ForwardingRule) error {
	frDiff := cmp.Diff(existingFwdRule, expectedFwdRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "IPAddress"))
	l4.svcLogger.V(2).Info("IPv6 forwarding rule changed. Deleting existing ipv6 forwarding rule.",
		"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", expectedFwdRule), "diff", frDiff)

	err := l4.forwardingRules.Delete(existingFwdRule.Name)
	if err != nil {
		return err
	}
	l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", existingFwdRule.Name)
	return nil
}

func (l4netlb *L4NetLB) ensureIPv6ForwardingRule(bsLink string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	start := time.Now()
	expectedIPv6FrName := l4netlb.ipv6FRName()

	frLogger := l4netlb.svcLogger.WithValues("forwardingRuleName", expectedIPv6FrName)
	frLogger.V(2).Info("Ensuring external ipv6 forwarding rule for L4 NetLB Service", "backendServiceLink", bsLink)
	defer func() {
		frLogger.V(2).Info("Finished ensuring external ipv6 forwarding rule for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	existingIPv6FwdRule, err := l4netlb.forwardingRules.Get(expectedIPv6FrName)
	if err != nil {
		frLogger.Error(err, "l4netlb.forwardingRules.Get returned error")
		return nil, utils.ResourceResync, err
	}

	subnetworkURL, err := l4netlb.ipv6SubnetURL()
	if err != nil {
		return nil, utils.ResourceResync, fmt.Errorf("error getting ipv6 forwarding rule subnet: %w", err)
	}
	frLogger.V(2).Info("subnetworkURL for service", "subnetworkURL", subnetworkURL)

	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then requestedIP = "".
	ipv6AddrToUse, ipv6AddressName, err := address.IPv6ToUse(l4netlb.cloud, l4netlb.Service, existingIPv6FwdRule, subnetworkURL, frLogger)
	if err != nil {
		frLogger.Error(err, "address.IPv6ToUse for service returned error")
		return nil, utils.ResourceResync, err
	}
	frLogger.V(2).Info("ipv6AddressToUse for service", "ipv6AddressToUse", ipv6AddrToUse)

	netTier, isFromAnnotation := l4annotations.NetworkTier(l4netlb.Service)
	frLogger.V(2).Info("network tier for service", "networkTier", netTier, "isFromAnnotation", isFromAnnotation)

	// IPv6 address is not supported for External Regional Network Load Balancing with Standard network tier.
	if netTier == cloud.NetworkTierStandard {
		resourceErr := "IPv6 External Load Balancer"
		return nil, utils.ResourceResync, utils.NewUnsupportedNetworkTierErr(resourceErr, string(cloud.NetworkTierStandard))
	}

	// Only for IPv6, address reservation is not supported on Standard Tier
	if !l4netlb.cloud.IsLegacyNetwork() && netTier == cloud.NetworkTierPremium {
		nm := types.NamespacedName{Namespace: l4netlb.Service.Namespace, Name: l4netlb.Service.Name}.String()
		addrMgr := address.NewManager(l4netlb.cloud, nm, l4netlb.cloud.Region(), subnetworkURL, expectedIPv6FrName, ipv6AddressName, ipv6AddrToUse, cloud.SchemeExternal, netTier, address.IPv6Version, frLogger)

		// If network tier annotation in Service Spec is present
		// check if it matches network tiers from forwarding rule and external ip Address.
		// If they do not match, tear down the existing resources with the wrong tier.
		if isFromAnnotation {
			if err := l4netlb.tearDownResourcesWithWrongNetworkTier(existingIPv6FwdRule, netTier, addrMgr, frLogger); err != nil {
				return nil, utils.ResourceResync, err
			}
		}

		ipv6AddrToUse, _, err = addrMgr.HoldAddress()
		if err != nil {
			return nil, utils.ResourceResync, err
		}
		frLogger.V(2).Info("ensureIPv6ForwardingRule: reserved IP for the forwarding rule", "ip", ipv6AddrToUse)
		defer func() {
			// Release the address that was reserved, in all cases. If the forwarding rule was successfully created,
			// the ephemeral IP is not needed anymore. If it was not created, the address should be released to prevent leaks.
			if err := addrMgr.ReleaseAddress(); err != nil {
				frLogger.Error(err, "ensureIPv6ForwardingRule: failed to release address reservation, possibly causing an orphan")
			}
		}()
	} else if existingIPv6FwdRule != nil && existingIPv6FwdRule.NetworkTier != netTier.ToGCEValue() {
		frLogger.V(2).Info("deleting forwarding rule for service due to network tier mismatch", "existingTier", existingIPv6FwdRule.NetworkTier, "expectedTier", netTier)
		err := l4netlb.forwardingRules.Delete(existingIPv6FwdRule.Name)
		if err != nil {
			frLogger.Error(err, "l4netlb.forwardingRules.Delete returned error, want nil")
		}
	}

	expectedIPv6FwdRule, err := l4netlb.buildExpectedIPv6ForwardingRule(bsLink, ipv6AddrToUse, subnetworkURL, netTier)
	if err != nil {
		return nil, utils.ResourceResync, fmt.Errorf("l4netlb.buildExpectedIPv6ForwardingRule(%s, %s, %s, %v) returned error %w, want nil", bsLink, ipv6AddrToUse, subnetworkURL, netTier, err)
	}
	frLogger.V(2).Info("l4netlb.buildExpectedIPv6ForwardingRule(_,_,_,_) for service", "expectedIPv6FwdRule", expectedIPv6FwdRule)

	if existingIPv6FwdRule != nil {
		if existingIPv6FwdRule.NetworkTier != expectedIPv6FwdRule.NetworkTier {
			resource := fmt.Sprintf("Forwarding rule (%v)", existingIPv6FwdRule.Name)
			networkTierMismatchError := utils.NewNetworkTierErr(resource, existingIPv6FwdRule.NetworkTier, expectedIPv6FwdRule.NetworkTier)
			return nil, utils.ResourceResync, networkTierMismatchError
		}

		equal, err := forwardingrules.EqualIPv6(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return existingIPv6FwdRule, utils.ResourceResync, err
		}
		if equal {
			frLogger.V(2).Info("ensureIPv6ForwardingRule: Skipping update of unchanged ipv6 forwarding rule")
			return existingIPv6FwdRule, utils.ResourceResync, nil
		}
		err = l4netlb.deleteChangedIPv6ForwardingRule(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return nil, utils.ResourceUpdate, err
		}
	}
	frLogger.V(2).Info("ensureIPv6ForwardingRule: Creating/Recreating forwarding rule")
	err = l4netlb.forwardingRules.Create(expectedIPv6FwdRule)
	if err != nil {
		return nil, utils.ResourceUpdate, err
	}

	createdFr, err := l4netlb.forwardingRules.Get(expectedIPv6FwdRule.Name)
	return createdFr, utils.ResourceUpdate, err
}

func (l4netlb *L4NetLB) buildExpectedIPv6ForwardingRule(bsLink, ipv6AddressToUse, subnetworkURL string, netTier cloud.NetworkTier) (*composite.ForwardingRule, error) {
	frName := l4netlb.ipv6FRName()

	frDesc, err := utils.MakeL4IPv6ForwardingRuleDescription(l4netlb.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to compute description for forwarding rule %s, err: %w", frName, err)
	}

	// ipv6AddressToUse will be returned from address manager without /96 prefix.
	// for creating external IPv6 forwarding rule, address has to be specified with /96 prefix, or API will return error.
	// This applies only to IPv6 External Forwarding rules,
	// there is no such requirement for internal IPv6 forwarding rules.
	if ipv6AddressToUse != "" && !strings.HasSuffix(ipv6AddressToUse, prefix96range) {
		ipv6AddressToUse += prefix96range
	}

	svcPorts := l4netlb.Service.Spec.Ports
	ports := utils.GetPorts(svcPorts)
	portRange := utils.MinMaxPortRange(svcPorts)
	protocol := utils.GetProtocol(svcPorts)
	fr := &composite.ForwardingRule{
		Name:                frName,
		Description:         frDesc,
		IPAddress:           ipv6AddressToUse,
		IPProtocol:          string(protocol),
		PortRange:           portRange,
		LoadBalancingScheme: string(cloud.SchemeExternal),
		BackendService:      bsLink,
		IpVersion:           IPVersionIPv6,
		NetworkTier:         netTier.ToGCEValue(),
		Subnetwork:          subnetworkURL,
	}
	if len(ports) <= maxForwardedPorts && flags.F.EnableDiscretePortForwarding {
		fr.Ports = utils.GetPorts(svcPorts)
		fr.PortRange = ""
	}

	return fr, nil
}

func (l4netlb *L4NetLB) deleteChangedIPv6ForwardingRule(existingFwdRule *composite.ForwardingRule, expectedFwdRule *composite.ForwardingRule) error {
	frDiff := cmp.Diff(existingFwdRule, expectedFwdRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "IPAddress"))
	l4netlb.svcLogger.V(2).Info("IPv6 External forwarding rule changed. Deleting existing ipv6 forwarding rule.",
		"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", expectedFwdRule), "diff", frDiff)

	err := l4netlb.forwardingRules.Delete(existingFwdRule.Name)
	if err != nil {
		return err
	}
	l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "External ForwardingRule %q deleted", existingFwdRule.Name)
	return nil
}
