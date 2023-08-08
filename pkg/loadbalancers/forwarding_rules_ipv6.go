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

package loadbalancers

import (
	"fmt"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	IPVersionIPv6 = "IPV6"
	prefix96range = "/96"
)

func (l4 *L4) ensureIPv6ForwardingRule(bsLink string, options gce.ILBOptions, existingIPv6FwdRule *composite.ForwardingRule, ipv6AddressToUse string) (*composite.ForwardingRule, error) {
	start := time.Now()

	expectedIPv6FwdRule, err := l4.buildExpectedIPv6ForwardingRule(bsLink, options, ipv6AddressToUse)
	if err != nil {
		return nil, fmt.Errorf("l4.buildExpectedIPv6ForwardingRule(%s, %v, %s) returned error %w, want nil", bsLink, options, ipv6AddressToUse, err)
	}

	klog.V(2).Infof("Ensuring internal ipv6 forwarding rule %s for L4 ILB Service %s/%s, backend service link: %s", expectedIPv6FwdRule.Name, l4.Service.Namespace, l4.Service.Name, bsLink)
	defer func() {
		klog.V(2).Infof("Finished ensuring internal ipv6 forwarding rule %s for L4 ILB Service %s/%s, time taken: %v", expectedIPv6FwdRule.Name, l4.Service.Namespace, l4.Service.Name, time.Since(start))
	}()

	if existingIPv6FwdRule != nil {
		equal, err := EqualIPv6ForwardingRules(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return existingIPv6FwdRule, err
		}
		if equal {
			klog.V(2).Infof("ensureIPv6ForwardingRule: Skipping update of unchanged ipv6 forwarding rule - %s", expectedIPv6FwdRule.Name)
			return existingIPv6FwdRule, nil
		}
		err = l4.deleteChangedIPv6ForwardingRule(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return nil, err
		}
	}

	klog.V(2).Infof("ensureIPv6ForwardingRule: Creating/Recreating forwarding rule - %s", expectedIPv6FwdRule.Name)
	err = l4.forwardingRules.Create(expectedIPv6FwdRule)
	if err != nil {
		return nil, err
	}

	createdFr, err := l4.forwardingRules.Get(expectedIPv6FwdRule.Name)
	return createdFr, err
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
	protocol := utils.GetProtocol(svcPorts)

	fr := &composite.ForwardingRule{
		Name:                frName,
		Description:         frDesc,
		IPAddress:           ipv6AddressToUse,
		IPProtocol:          string(protocol),
		Ports:               ports,
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      bsLink,
		IpVersion:           IPVersionIPv6,
		Network:             l4.cloud.NetworkURL(),
		Subnetwork:          subnetworkURL,
		AllowGlobalAccess:   options.AllowGlobalAccess,
		NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
	}
	if len(ports) > maxL4ILBPorts {
		fr.Ports = nil
		fr.AllPorts = true
	}

	return fr, nil
}

func (l4 *L4) deleteChangedIPv6ForwardingRule(existingFwdRule *composite.ForwardingRule, expectedFwdRule *composite.ForwardingRule) error {
	frDiff := cmp.Diff(existingFwdRule, expectedFwdRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "IPAddress"))
	klog.V(2).Infof("IPv6 forwarding rule changed - Existing - %+v\n, New - %+v\n, Diff(-existing, +new) - %s\n. Deleting existing ipv6 forwarding rule.", existingFwdRule, expectedFwdRule, frDiff)

	err := l4.forwardingRules.Delete(existingFwdRule.Name)
	if err != nil {
		return err
	}
	l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", existingFwdRule.Name)
	return nil
}

func (l4netlb *L4NetLB) ensureIPv6ForwardingRule(bsLink string) (*composite.ForwardingRule, error) {
	start := time.Now()

	klog.V(2).Infof("Ensuring external ipv6 forwarding rule for L4 NetLB Service %s/%s, backend service link: %s", l4netlb.Service.Namespace, l4netlb.Service.Name, bsLink)
	defer func() {
		klog.V(2).Infof("Finished ensuring external ipv6 forwarding rule for L4 NetLB Service %s/%s, time taken: %v", l4netlb.Service.Namespace, l4netlb.Service.Name, time.Since(start))
	}()

	expectedIPv6FrName := l4netlb.ipv6FRName()
	existingIPv6FwdRule, err := l4netlb.forwardingRules.Get(expectedIPv6FrName)
	if err != nil {
		klog.Errorf("l4netlb.forwardingRules.Get(%s) returned error %v", expectedIPv6FrName, err)
		return nil, err
	}

	subnetworkURL, err := l4netlb.ipv6SubnetURL()
	if err != nil {
		return nil, fmt.Errorf("error getting ipv6 forwarding rule subnet: %w", err)
	}

	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then requestedIP = "".
	ipv6AddrToUse, err := ipv6AddressToUse(l4netlb.cloud, l4netlb.Service, existingIPv6FwdRule, subnetworkURL)
	if err != nil {
		klog.Errorf("ipv6AddressToUse for service %s/%s returned error %v", l4netlb.Service.Namespace, l4netlb.Service.Name, err)
		return nil, err
	}
	netTier, isFromAnnotation := utils.GetNetworkTier(l4netlb.Service)

	// Only for IPv6, address reservation is not supported on Standard Tier
	if !l4netlb.cloud.IsLegacyNetwork() && netTier == cloud.NetworkTierPremium {
		nm := types.NamespacedName{Namespace: l4netlb.Service.Namespace, Name: l4netlb.Service.Name}.String()
		addrMgr := newAddressManager(l4netlb.cloud, nm, l4netlb.cloud.Region(), subnetworkURL, expectedIPv6FrName, ipv6AddrToUse, cloud.SchemeExternal, netTier, IPv6Version)

		// If network tier annotation in Service Spec is present
		// check if it matches network tiers from forwarding rule and external ip Address.
		// If they do not match, tear down the existing resources with the wrong tier.
		if isFromAnnotation {
			if err := l4netlb.tearDownResourcesWithWrongNetworkTier(existingIPv6FwdRule, netTier, addrMgr); err != nil {
				return nil, err
			}
		}

		ipv6AddrToUse, _, err = addrMgr.HoldAddress()
		if err != nil {
			return nil, err
		}
		klog.V(2).Infof("ensureIPv6ForwardingRule(%v): reserved IP %q for the forwarding rule %s", nm, ipv6AddrToUse, expectedIPv6FrName)
		defer func() {
			// Release the address that was reserved, in all cases. If the forwarding rule was successfully created,
			// the ephemeral IP is not needed anymore. If it was not created, the address should be released to prevent leaks.
			if err := addrMgr.ReleaseAddress(); err != nil {
				klog.Errorf("ensureIPv6ForwardingRule: failed to release address reservation, possibly causing an orphan: %v", err)
			}
		}()
	} else if existingIPv6FwdRule != nil && existingIPv6FwdRule.NetworkTier != netTier.ToGCEValue() {
		err := l4netlb.forwardingRules.Delete(existingIPv6FwdRule.Name)
		if err != nil {
			klog.Errorf("l4netlb.forwardingRules.Delete(%s) returned error %v, want nil", existingIPv6FwdRule.Name, err)
		}
	}

	expectedIPv6FwdRule, err := l4netlb.buildExpectedIPv6ForwardingRule(bsLink, ipv6AddrToUse, subnetworkURL, netTier)
	if err != nil {
		return nil, fmt.Errorf("l4netlb.buildExpectedIPv6ForwardingRule(%s, %s, %s, %v) returned error %w, want nil", bsLink, ipv6AddrToUse, subnetworkURL, netTier, err)
	}

	if existingIPv6FwdRule != nil {
		if existingIPv6FwdRule.NetworkTier != expectedIPv6FwdRule.NetworkTier {
			resource := fmt.Sprintf("Forwarding rule (%v)", existingIPv6FwdRule.Name)
			networkTierMismatchError := utils.NewNetworkTierErr(resource, existingIPv6FwdRule.NetworkTier, expectedIPv6FwdRule.NetworkTier)
			return nil, networkTierMismatchError
		}

		equal, err := EqualIPv6ForwardingRules(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return existingIPv6FwdRule, err
		}
		if equal {
			klog.V(2).Infof("ensureIPv6ForwardingRule: Skipping update of unchanged ipv6 forwarding rule - %s", expectedIPv6FwdRule.Name)
			return existingIPv6FwdRule, nil
		}
		err = l4netlb.deleteChangedIPv6ForwardingRule(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return nil, err
		}
	}
	klog.V(2).Infof("ensureIPv6ForwardingRule: Creating/Recreating forwarding rule - %s", expectedIPv6FwdRule.Name)
	err = l4netlb.forwardingRules.Create(expectedIPv6FwdRule)
	if err != nil {
		return nil, err
	}

	createdFr, err := l4netlb.forwardingRules.Get(expectedIPv6FwdRule.Name)
	return createdFr, err
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
	portRange, protocol := utils.MinMaxPortRangeAndProtocol(svcPorts)
	fr := &composite.ForwardingRule{
		Name:                frName,
		Description:         frDesc,
		IPAddress:           ipv6AddressToUse,
		IPProtocol:          protocol,
		PortRange:           portRange,
		LoadBalancingScheme: string(cloud.SchemeExternal),
		BackendService:      bsLink,
		IpVersion:           IPVersionIPv6,
		NetworkTier:         netTier.ToGCEValue(),
		Subnetwork:          subnetworkURL,
	}

	return fr, nil
}

func (l4netlb *L4NetLB) deleteChangedIPv6ForwardingRule(existingFwdRule *composite.ForwardingRule, expectedFwdRule *composite.ForwardingRule) error {
	frDiff := cmp.Diff(existingFwdRule, expectedFwdRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "IPAddress"))
	klog.V(2).Infof("IPv6 External forwarding rule changed - Existing - %+v\n, New - %+v\n, Diff(-existing, +new) - %s\n. Deleting existing ipv6 forwarding rule.", existingFwdRule, expectedFwdRule, frDiff)

	err := l4netlb.forwardingRules.Delete(existingFwdRule.Name)
	if err != nil {
		return err
	}
	l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "External ForwardingRule %q deleted", existingFwdRule.Name)
	return nil
}

func EqualIPv6ForwardingRules(fr1, fr2 *composite.ForwardingRule) (bool, error) {
	id1, err := cloud.ParseResourceURL(fr1.BackendService)
	if err != nil {
		return false, fmt.Errorf("EqualIPv6ForwardingRules(): failed to parse backend resource URL from FR, err - %w", err)
	}
	id2, err := cloud.ParseResourceURL(fr2.BackendService)
	if err != nil {
		return false, fmt.Errorf("EqualIPv6ForwardingRules(): failed to parse resource URL from FR, err - %w", err)
	}
	return fr1.IPProtocol == fr2.IPProtocol &&
		fr1.LoadBalancingScheme == fr2.LoadBalancingScheme &&
		utils.EqualStringSets(fr1.Ports, fr2.Ports) &&
		id1.Equal(id2) &&
		fr1.AllowGlobalAccess == fr2.AllowGlobalAccess &&
		fr1.AllPorts == fr2.AllPorts &&
		fr1.Subnetwork == fr2.Subnetwork &&
		fr1.NetworkTier == fr2.NetworkTier, nil
}

// ipv6AddrToUse determines which IPv4 address needs to be used in the ForwardingRule,
// address evaluated in the following order:
//
//  1. Use static addresses annotation "networking.gke.io/load-balancer-ip-addresses".
//  2. Use existing forwarding rule IP. If subnetwork was changed (or no existing IP),
//     reset the IP (by returning empty string).
func ipv6AddressToUse(cloud *gce.Cloud, svc *corev1.Service, ipv6FwdRule *composite.ForwardingRule, requestedSubnet string) (string, error) {
	// Get value from new annotation which support both IPv4 and IPv6
	ipv6AddressFromAnnotation, err := annotations.FromService(svc).IPv6AddressAnnotation(cloud)
	if err != nil {
		return "", err
	}
	if ipv6AddressFromAnnotation != "" {
		// Google Cloud stores ipv6 addresses in CIDR form,
		// but to create static address you need to specify address without range
		return ipv6AddressWithoutRange(ipv6AddressFromAnnotation), nil
	}
	if ipv6FwdRule == nil {
		return "", nil
	}
	if requestedSubnet != ipv6FwdRule.Subnetwork {
		// reset ip address since subnet is being changed.
		return "", nil
	}

	// Google Cloud creates ipv6 forwarding rules with IPAddress in CIDR form,
	// but to create static address you need to specify address without range
	return ipv6AddressWithoutRange(ipv6FwdRule.IPAddress), nil
}

func ipv6AddressWithoutRange(ipv6Address string) string {
	return strings.Split(ipv6Address, "/")[0]
}
