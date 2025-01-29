/*
Copyright 2018 The Kubernetes Authors.

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
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	// maxForwardedPorts is the maximum number of ports that can be specified in an Forwarding Rule
	maxForwardedPorts = 5
	// addressAlreadyInUseMessageExternal is the error message string returned by the compute API
	// when creating an external forwarding rule that uses a conflicting IP address.
	addressAlreadyInUseMessageExternal = "Specified IP address is in-use and would result in a conflict."
	// addressAlreadyInUseMessageInternal is the error message string returned by the compute API
	// when creating an internal forwarding rule that uses a conflicting IP address.
	addressAlreadyInUseMessageInternal = "IP_IN_USE_BY_ANOTHER_RESOURCE"
)

func (l7 *L7) checkHttpForwardingRule() (err error) {
	if l7.tp == nil {
		return fmt.Errorf("cannot create forwarding rule without proxy")
	}
	name := l7.namer.ForwardingRule(namer.HTTPProtocol)
	address, _, err := l7.getEffectiveIP()
	if err != nil {
		return err
	}
	fw, err := l7.checkForwardingRule(namer.HTTPProtocol, name, l7.tp.SelfLink, address)
	if err != nil {
		return err
	}
	l7.fw = fw
	return nil
}

func (l7 *L7) checkHttpsForwardingRule() (err error) {
	if l7.tps == nil {
		l7.logger.V(3).Info("No https target proxy for l7, not created https forwarding rule", "l7", l7)
		return nil
	}
	name := l7.namer.ForwardingRule(namer.HTTPSProtocol)
	address, _, err := l7.getEffectiveIP()
	if err != nil {
		return err
	}
	fws, err := l7.checkForwardingRule(namer.HTTPSProtocol, name, l7.tps.SelfLink, address)
	if err != nil {
		return err
	}
	l7.fws = fws
	return nil
}

func (l7 *L7) checkForwardingRule(protocol namer.NamerProtocol, name, proxyLink, ip string) (existing *composite.ForwardingRule, err error) {
	key, err := l7.CreateKey(name)
	if err != nil {
		return nil, err
	}
	version := l7.Versions().ForwardingRule
	description, err := l7.description()
	if err != nil {
		return nil, err
	}

	isL7ILB := utils.IsGCEL7ILBIngress(l7.runtimeInfo.Ingress)
	isL7XLBRegional := utils.IsGCEL7XLBRegionalIngress(&l7.ingress)
	tr := translator.NewTranslator(isL7ILB, isL7XLBRegional, l7.namer)
	env := &translator.Env{VIP: ip, Network: l7.cloud.NetworkURL(), Subnetwork: l7.cloud.SubnetworkURL()}
	fr := tr.ToCompositeForwardingRule(env, protocol, version, proxyLink, description, l7.runtimeInfo.StaticIPSubnet)

	existing, _ = composite.GetForwardingRule(l7.cloud, key, version, l7.logger)
	if existing != nil && (fr.IPAddress != "" && existing.IPAddress != fr.IPAddress || existing.PortRange != fr.PortRange) {
		l7.logger.Info("Recreating forwarding rule %v(%v), so it has %v(%v)",
			"existingIp", existing.IPAddress, "existingPortRange", existing.PortRange,
			"targetIp", fr.IPAddress, "targetPortRange", fr.PortRange)
		if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l7.cloud, key, version, l7.logger)); err != nil {
			return nil, err
		}
		existing = nil
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", key.Name)
	}
	if existing == nil {
		// This is a special case where exactly one of http or https forwarding rule
		// existed before and the existing forwarding rule uses ingress managed static ip address.
		// In this case, the forwarding rule needs to be created with the same static ip.
		// Note that this is not needed when user specifies a static IP.
		if ip == "" {
			managedStaticIPName := l7.namer.ForwardingRule(namer.HTTPProtocol)
			// Get static IP address if ingress has static IP annotation.
			// Note that this Static IP annotation is applied by ingress controller.
			if currentIPName, exists := l7.ingress.Annotations[annotations.StaticIPKey]; exists && currentIPName == managedStaticIPName {
				currentIP, _ := l7.cloud.GetGlobalAddress(managedStaticIPName)
				if currentIP != nil {
					l7.logger.V(3).Info("Ingress managed static IP %s(%s) exists, using it to create forwarding rule %s", "currentIpName", currentIPName, "currentIp", currentIP.Address, "forwardingRuleName", name)
					fr.IPAddress = currentIP.Address
				}
			}
		}
		l7.logger.V(3).Info("Creating forwarding rule for proxy and ip", "proxy", proxyLink, "ip", ip, "protocol", protocol)

		if err = composite.CreateForwardingRule(l7.cloud, key, fr, l7.logger); err != nil {
			return nil, err
		}
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q created", key.Name)

		key, err = l7.CreateKey(name)
		if err != nil {
			return nil, err
		}
		existing, err = composite.GetForwardingRule(l7.cloud, key, version, l7.logger)
		if err != nil {
			return nil, err
		}
	}
	// TODO: If the port range and protocol don't match, recreate the rule
	if utils.EqualResourceIDs(existing.Target, proxyLink) {
		l7.logger.V(4).Info("Forwarding rule already exists", "forwardingRuleName", existing.Name)
	} else {
		l7.logger.V(3).Info("Forwarding rule has the wrong proxy, overwriting",
			"forwardingRuleName", existing.Name, "existingTarget", existing.Target, "proxy", proxyLink)
		key, err := l7.CreateKey(existing.Name)
		if err != nil {
			return nil, err
		}
		if err := composite.SetProxyForForwardingRule(l7.cloud, key, existing, proxyLink, l7.logger); err != nil {
			return nil, err
		}
	}
	return existing, nil
}

// getEffectiveIP returns a string with the IP to use in the HTTP and HTTPS
// forwarding rules, a boolean indicating if this is an IP the controller
// should manage or not and an error if the specified IP was not found.
func (l7 *L7) getEffectiveIP() (string, bool, error) {

	// A note on IP management:
	// User specifies a different IP on startup:
	//	- We create a forwarding rule with the given IP.
	//		- If this ip doesn't exist in GCE, an error is thrown which fails ingress creation.
	//	- In the happy case, no static ip is created or deleted by this controller.
	// Controller allocates a staticIP/ephemeralIP, but user changes it:
	//  - We still delete the old static IP, but only when we tear down the
	//	  Ingress in Cleanup(). Till then the static IP stays around, but
	//    the forwarding rules get deleted/created with the new IP.
	//  - There will be a period of downtime as we flip IPs.
	// User specifies the same static IP to 2 Ingresses:
	//  - GCE will throw a 400, and the controller will keep trying to use
	//    the IP in the hope that the user manually resolves the conflict
	//    or deletes/modifies the Ingress.
	// TODO: Handle the last case better.

	if l7.runtimeInfo.StaticIPName != "" {
		key, err := l7.CreateKey(l7.runtimeInfo.StaticIPName)
		if err != nil {
			return "", false, err
		}

		// Existing static IPs allocated to forwarding rules will get orphaned
		// till the Ingress is torn down.
		// TODO(shance): Replace version
		if ip, err := composite.GetAddress(l7.cloud, key, meta.VersionGA, l7.logger); err != nil || ip == nil {
			return "", false, fmt.Errorf("the given static IP name %v doesn't translate to an existing static IP.",
				l7.runtimeInfo.StaticIPName)
		} else {
			l7.runtimeInfo.StaticIPSubnet = ip.Subnetwork
			return ip.Address, false, nil
		}
	}
	if l7.ip != nil {
		return l7.ip.Address, true, nil
	}
	return "", true, nil
}

// ensureIPv4ForwardingRule creates a forwarding rule with the given name, if it does not exist. It updates the existing
// forwarding rule if needed.
func (l4 *L4) ensureIPv4ForwardingRule(bsLink string, options gce.ILBOptions, existingFwdRule *composite.ForwardingRule, subnetworkURL, ipToUse string) (*composite.ForwardingRule, error) {
	start := time.Now()

	// version used for creating the existing forwarding rule.
	version := meta.VersionGA
	frName := l4.GetFRName()

	frLogger := l4.svcLogger.WithValues("forwardingRuleName", frName)
	frLogger.V(2).Info("Ensuring internal forwarding rule for L4 ILB Service", "backendServiceLink", bsLink)
	defer func() {
		frLogger.V(2).Info("Finished ensuring internal forwarding rule for L4 ILB Service", "timeTaken", time.Since(start))
	}()

	servicePorts := l4.Service.Spec.Ports
	ports := utils.GetPorts(servicePorts)
	protocol := utils.GetProtocol(servicePorts)
	// Create the forwarding rule
	frDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), ipToUse,
		version, false, utils.ILB)
	if err != nil {
		return nil, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", frName,
			err)
	}

	fr := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           ipToUse,
		Ports:               ports,
		IPProtocol:          string(protocol),
		LoadBalancingScheme: string(cloud.SchemeInternal),
		Subnetwork:          subnetworkURL,
		Network:             l4.network.NetworkURL,
		NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
		Version:             version,
		BackendService:      bsLink,
		AllowGlobalAccess:   options.AllowGlobalAccess,
		Description:         frDesc,
	}
	if len(ports) > maxForwardedPorts {
		fr.Ports = nil
		fr.AllPorts = true
	}

	if existingFwdRule != nil {
		equal, err := Equal(existingFwdRule, fr)
		if err != nil {
			return nil, err
		}
		if equal {
			// nothing to do
			frLogger.V(2).Info("ensureIPv4ForwardingRule: Skipping update of unchanged forwarding rule")
			return existingFwdRule, nil
		}
		frDiff := cmp.Diff(existingFwdRule, fr)
		// If the forwarding rule pointed to a backend service which does not match the controller naming scheme,
		// that resource could be leaked. It is not being deleted here because that is a user-managed resource.
		frLogger.V(2).Info("ensureIPv4ForwardingRule: forwarding rule changed. Deleting existing forwarding rule.",
			"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", fr), "diff", frDiff)
		if err = l4.forwardingRules.Delete(existingFwdRule.Name); err != nil {
			return nil, err
		}
		l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", existingFwdRule.Name)
	}
	frLogger.V(2).Info("ensureIPv4ForwardingRule: Creating/Recreating forwarding rule")
	if err = l4.forwardingRules.Create(fr); err != nil {
		if isAddressAlreadyInUseError(err) {
			return nil, utils.NewIPConfigurationError(fr.IPAddress, err.Error())
		}
		return nil, err
	}

	fr, err = l4.forwardingRules.Get(fr.Name)
	if err != nil {
		return nil, err
	}
	if fr == nil {
		return nil, fmt.Errorf("forwarding Rule %s not found", frName)
	}
	return fr, nil
}

// ensureIPv4ForwardingRule creates a forwarding rule with the given name for L4NetLB,
// if it does not exist. It updates the existing forwarding rule if needed.
func (l4netlb *L4NetLB) ensureIPv4ForwardingRule(bsLink string) (*composite.ForwardingRule, IPAddressType, error) {
	frName := l4netlb.frName()

	start := time.Now()
	frLogger := l4netlb.svcLogger.WithValues("forwardingRuleName", frName)
	frLogger.V(2).Info("Ensuring external forwarding rule for L4 NetLB Service", "backendServiceLink", bsLink)
	defer func() {
		frLogger.V(2).Info("Finished ensuring external forwarding rule for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	// version used for creating the existing forwarding rule.
	version := meta.VersionGA
	existingFwdRule, err := l4netlb.forwardingRules.Get(frName)
	if err != nil {
		frLogger.Error(err, "l4netlb.forwardingRules.Get returned error")
		return nil, IPAddrUndefined, err
	}

	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then requestedIP = "".
	ipToUse, err := ipv4AddrToUse(l4netlb.cloud, l4netlb.recorder, l4netlb.Service, existingFwdRule, "")
	if err != nil {
		frLogger.Error(err, "ipv4AddrToUse for service returned error")
		return nil, IPAddrUndefined, err
	}
	frLogger.V(2).Info("ensureIPv4ForwardingRule: Got LoadBalancer IP", "ip", ipToUse)

	netTier, isFromAnnotation := utils.GetNetworkTier(l4netlb.Service)
	var isIPManaged IPAddressType
	// If the network is not a legacy network, use the address manager
	if !l4netlb.cloud.IsLegacyNetwork() {
		nm := types.NamespacedName{Namespace: l4netlb.Service.Namespace, Name: l4netlb.Service.Name}.String()
		addrMgr := newAddressManager(l4netlb.cloud, nm, l4netlb.cloud.Region() /*subnetURL = */, "", frName, ipToUse, cloud.SchemeExternal, netTier, IPv4Version, frLogger)

		// If network tier annotation in Service Spec is present
		// check if it matches network tiers from forwarding rule and external ip Address.
		// If they do not match, tear down the existing resources with the wrong tier.
		if isFromAnnotation {
			if err := l4netlb.tearDownResourcesWithWrongNetworkTier(existingFwdRule, netTier, addrMgr, frLogger); err != nil {
				return nil, IPAddrUndefined, err
			}
		}

		ipToUse, isIPManaged, err = addrMgr.HoldAddress()
		if err != nil {
			return nil, IPAddrUndefined, err
		}
		frLogger.V(2).Info("ensureIPv4ForwardingRule: reserved IP for the forwarding rule", "ip", ipToUse)
		defer func() {
			// Release the address that was reserved, in all cases. If the forwarding rule was successfully created,
			// the ephemeral IP is not needed anymore. If it was not created, the address should be released to prevent leaks.
			if err := addrMgr.ReleaseAddress(); err != nil {
				frLogger.Error(err, "ensureIPv4ForwardingRule: failed to release address reservation, possibly causing an orphan")
			}
		}()
	}

	svcPorts := l4netlb.Service.Spec.Ports
	ports := utils.GetPorts(svcPorts)
	portRange := utils.MinMaxPortRange(svcPorts)
	protocol := utils.GetProtocol(svcPorts)
	serviceKey := utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name)
	frDesc, err := utils.MakeL4LBServiceDescription(serviceKey, ipToUse, version, false, utils.XLB)
	if err != nil {
		return nil, IPAddrUndefined, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", frName,
			err)
	}
	fr := &composite.ForwardingRule{
		Name:                frName,
		Description:         frDesc,
		IPAddress:           ipToUse,
		IPProtocol:          string(protocol),
		PortRange:           portRange,
		LoadBalancingScheme: string(cloud.SchemeExternal),
		BackendService:      bsLink,
		NetworkTier:         netTier.ToGCEValue(),
	}
	if len(ports) <= maxForwardedPorts && flags.F.EnableDiscretePortForwarding {
		fr.Ports = ports
		fr.PortRange = ""
	}

	if existingFwdRule != nil {
		if existingFwdRule.NetworkTier != fr.NetworkTier {
			resource := fmt.Sprintf("Forwarding rule (%v)", frName)
			networkTierMismatchError := utils.NewNetworkTierErr(resource, existingFwdRule.NetworkTier, fr.NetworkTier)
			return nil, IPAddrUndefined, networkTierMismatchError
		}
		equal, err := Equal(existingFwdRule, fr)
		if err != nil {
			return existingFwdRule, IPAddrUndefined, err
		}
		if equal {
			// nothing to do
			frLogger.V(2).Info("ensureIPv4ForwardingRule: Skipping update of unchanged forwarding rule")
			return existingFwdRule, isIPManaged, nil
		}
		frDiff := cmp.Diff(existingFwdRule, fr)
		// If the forwarding rule pointed to a backend service which does not match the controller naming scheme,
		// that resource could be leaked. It is not being deleted here because that is a user-managed resource.
		frLogger.V(2).Info("ensureIPv4ForwardingRule: forwarding rule changed. Deleting existing forwarding rule.",
			"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", fr), "diff", frDiff)
		if err = l4netlb.forwardingRules.Delete(existingFwdRule.Name); err != nil {
			return nil, IPAddrUndefined, err
		}
		l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", existingFwdRule.Name)
	}
	frLogger.V(2).Info("ensureIPv4ForwardingRule: Creating/Recreating forwarding rule")
	if err = l4netlb.forwardingRules.Create(fr); err != nil {
		if isAddressAlreadyInUseError(err) {
			return nil, IPAddrUndefined, utils.NewIPConfigurationError(fr.IPAddress, addressAlreadyInUseMessageExternal)
		}
		return nil, IPAddrUndefined, err
	}
	createdFr, err := l4netlb.forwardingRules.Get(fr.Name)
	if err != nil {
		return nil, IPAddrUndefined, err
	}
	if createdFr == nil {
		return nil, IPAddrUndefined, fmt.Errorf("forwarding rule %s not found", fr.Name)
	}
	return createdFr, isIPManaged, err
}

// tearDownResourcesWithWrongNetworkTier removes forwarding rule or IP address if its Network Tier differs from desired.
func (l4netlb *L4NetLB) tearDownResourcesWithWrongNetworkTier(existingFwdRule *composite.ForwardingRule, svcNetTier cloud.NetworkTier, am *addressManager, frLogger klog.Logger) error {
	if existingFwdRule != nil && existingFwdRule.NetworkTier != svcNetTier.ToGCEValue() {
		err := l4netlb.forwardingRules.Delete(existingFwdRule.Name)
		if err != nil {
			frLogger.Error(err, "l4netlb.forwardingRules.Delete returned error, want nil")
		}
	}
	return am.TearDownAddressIPIfNetworkTierMismatch()
}

func Equal(fr1, fr2 *composite.ForwardingRule) (bool, error) {
	id1, err := cloud.ParseResourceURL(fr1.BackendService)
	if err != nil {
		return false, fmt.Errorf("forwardingRulesEqual(): failed to parse backend resource URL from FR, err - %w", err)
	}
	id2, err := cloud.ParseResourceURL(fr2.BackendService)
	if err != nil {
		return false, fmt.Errorf("forwardingRulesEqual(): failed to parse resource URL from FR, err - %w", err)
	}
	return fr1.IPAddress == fr2.IPAddress &&
		fr1.IPProtocol == fr2.IPProtocol &&
		fr1.LoadBalancingScheme == fr2.LoadBalancingScheme &&
		equalPorts(fr1.Ports, fr2.Ports, fr1.PortRange, fr2.PortRange) &&
		utils.EqualCloudResourceIDs(id1, id2) &&
		fr1.AllowGlobalAccess == fr2.AllowGlobalAccess &&
		fr1.AllPorts == fr2.AllPorts &&
		equalResourcePaths(fr1.Subnetwork, fr2.Subnetwork) &&
		equalResourcePaths(fr1.Network, fr2.Network) &&
		fr1.NetworkTier == fr2.NetworkTier, nil
}

// equalPorts compares two port ranges or slices of ports. Before comparison,
// slices of ports are converted into a port range from smallest to largest
// port. This is done so we don't unnecessarily recreate forwarding rules
// when upgrading from port ranges to distinct ports, because recreating
// forwarding rules is traffic impacting.
func equalPorts(ports1, ports2 []string, portRange1, portRange2 string) bool {
	if !flags.F.EnableDiscretePortForwarding {
		return utils.EqualStringSets(ports1, ports2) && portRange1 == portRange2
	}
	if len(ports1) != 0 && portRange1 == "" {
		portRange1 = utils.MinMaxPortRange(ports1)
	}
	if len(ports2) != 0 && portRange2 == "" {
		portRange2 = utils.MinMaxPortRange(ports2)
	}
	return portRange1 == portRange2
}

func equalResourcePaths(rp1, rp2 string) bool {
	return rp1 == rp2 || utils.EqualResourceIDs(rp1, rp2)
}

// ipv4AddrToUse determines which IPv4 address needs to be used in the ForwardingRule,
// address evaluated in the following order:
//
//  1. Use static addresses annotation "networking.gke.io/load-balancer-ip-addresses".
//  2. Use .Spec.LoadBalancerIP (old field, was deprecated).
//  3. Use existing forwarding rule IP. If subnetwork was changed (or no existing IP),
//     reset the IP (by returning empty string).
func ipv4AddrToUse(cloud *gce.Cloud, recorder record.EventRecorder, svc *v1.Service, fwdRule *composite.ForwardingRule, requestedSubnet string) (string, error) {
	// Get value from new annotation which support both IPv4 and IPv6
	ipv4FromAnnotation, err := annotations.FromService(svc).IPv4AddressAnnotation(cloud)
	if err != nil {
		return "", err
	}
	if ipv4FromAnnotation != "" {
		if svc.Spec.LoadBalancerIP != "" {
			recorder.Event(svc, v1.EventTypeNormal, "MixedStaticIP", "Found both .Spec.LoadBalancerIP and \"networking.gke.io/load-balancer-ip-addresses\" annotation. Consider using annotation only.")
		}
		return ipv4FromAnnotation, nil
		// if no value from annotation (for example, annotation has only IPv6 addresses) -- continue
	}
	if svc.Spec.LoadBalancerIP != "" {
		return svc.Spec.LoadBalancerIP, nil
	}
	if fwdRule == nil {
		return "", nil
	}
	if requestedSubnet != fwdRule.Subnetwork {
		// reset ip address since subnet is being changed.
		return "", nil
	}
	return fwdRule.IPAddress, nil
}

func isAddressAlreadyInUseError(err error) bool {
	// Bad request HTTP status (400) is returned for external Forwarding Rules.
	alreadyInUseExternal := utils.IsHTTPErrorCode(err, http.StatusBadRequest) && strings.Contains(err.Error(), addressAlreadyInUseMessageExternal)
	// Conflict HTTP status (409) is returned for internal Forwarding Rules.
	alreadyInUseInternal := utils.IsHTTPErrorCode(err, http.StatusConflict) && strings.Contains(err.Error(), addressAlreadyInUseMessageInternal)
	return alreadyInUseExternal || alreadyInUseInternal
}
