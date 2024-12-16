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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/address"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/forwardingrules"
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
func (l4 *L4) ensureIPv4ForwardingRule(bsLink string, options gce.ILBOptions, existingFwdRule *composite.ForwardingRule, subnetworkURL, ipToUse string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
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
	protocol := string(utils.GetProtocol(servicePorts))
	allPorts := false
	if l4.enableMixedProtocol {
		protocol = forwardingrules.GetILBProtocol(servicePorts)
		if protocol == forwardingrules.ProtocolL3 {
			allPorts = true
			ports = nil
		}
	}
	if len(ports) > maxForwardedPorts {
		allPorts = true
		ports = nil
	}

	// Create the forwarding rule
	frDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), ipToUse,
		version, false, utils.ILB)
	if err != nil {
		return nil, utils.ResourceResync, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", frName,
			err)
	}

	newFwdRule := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           ipToUse,
		Ports:               ports,
		AllPorts:            allPorts,
		IPProtocol:          protocol,
		LoadBalancingScheme: string(cloud.SchemeInternal),
		Subnetwork:          subnetworkURL,
		Network:             l4.network.NetworkURL,
		NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
		Version:             version,
		BackendService:      bsLink,
		AllowGlobalAccess:   options.AllowGlobalAccess,
		Description:         frDesc,
	}

	if existingFwdRule != nil {
		equal, err := forwardingrules.EqualIPv4(existingFwdRule, newFwdRule)
		if err != nil {
			return nil, utils.ResourceResync, err
		}
		if equal {
			// nothing to do
			frLogger.V(2).Info("ensureIPv4ForwardingRule: Skipping update of unchanged forwarding rule")
			return existingFwdRule, utils.ResourceResync, nil
		}
		frDiff := cmp.Diff(existingFwdRule, newFwdRule)
		frLogger.V(2).Info("ensureIPv4ForwardingRule: forwarding rule changed.",
			"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", newFwdRule), "diff", frDiff)

		patchable, filtered := forwardingrules.PatchableIPv4(existingFwdRule, newFwdRule)
		if patchable {
			if err = l4.forwardingRules.Patch(filtered); err != nil {
				return nil, utils.ResourceUpdate, err
			}
			l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s patched", existingFwdRule.Name)
		} else {
			if err := l4.updateForwardingRule(existingFwdRule, newFwdRule, frLogger); err != nil {
				return nil, utils.ResourceUpdate, err
			}
		}
	} else {
		if err = l4.createFwdRule(newFwdRule, frLogger); err != nil {
			return nil, utils.ResourceUpdate, err
		}
		l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s created", newFwdRule.Name)
	}

	readFwdRule, err := l4.forwardingRules.Get(newFwdRule.Name)
	if err != nil {
		return nil, utils.ResourceUpdate, err
	}
	if readFwdRule == nil {
		return nil, utils.ResourceUpdate, fmt.Errorf("Forwarding Rule %s not found", frName)
	}
	return readFwdRule, utils.ResourceUpdate, nil
}

func (l4 *L4) updateForwardingRule(existingFwdRule, newFr *composite.ForwardingRule, frLogger klog.Logger) error {
	if err := l4.forwardingRules.Delete(existingFwdRule.Name); err != nil {
		return err
	}
	l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s deleted", existingFwdRule.Name)

	if err := l4.createFwdRule(newFr, frLogger); err != nil {
		return err
	}
	l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s re-created", newFr.Name)
	return nil
}

func (l4 *L4) createFwdRule(newFr *composite.ForwardingRule, frLogger klog.Logger) error {
	frLogger.V(2).Info("ensureIPv4ForwardingRule: Creating/Recreating forwarding rule")
	if err := l4.forwardingRules.Create(newFr); err != nil {
		if isAddressAlreadyInUseError(err) {
			return utils.NewIPConfigurationError(newFr.IPAddress, err.Error())
		}
		return err
	}
	return nil
}

// ensureIPv4ForwardingRule creates a forwarding rule with the given name for L4NetLB,
// if it does not exist. It updates the existing forwarding rule if needed.
func (l4netlb *L4NetLB) ensureIPv4ForwardingRule(bsLink string) (*composite.ForwardingRule, address.IPAddressType, utils.ResourceSyncStatus, error) {
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
		return nil, address.IPAddrUndefined, utils.ResourceResync, err
	}

	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then requestedIP = "".
	ipToUse, err := address.IPv4ToUse(l4netlb.cloud, l4netlb.recorder, l4netlb.Service, existingFwdRule, "")
	if err != nil {
		frLogger.Error(err, "address.IPv4ToUse for service returned error")
		return nil, address.IPAddrUndefined, utils.ResourceResync, err
	}
	frLogger.V(2).Info("ensureIPv4ForwardingRule: Got LoadBalancer IP", "ip", ipToUse)

	netTier, isFromAnnotation := annotations.NetworkTier(l4netlb.Service)
	var isIPManaged address.IPAddressType
	// If the network is not a legacy network, use the address manager
	if !l4netlb.cloud.IsLegacyNetwork() {
		nm := types.NamespacedName{Namespace: l4netlb.Service.Namespace, Name: l4netlb.Service.Name}.String()
		addrMgr := address.NewManager(l4netlb.cloud, nm, l4netlb.cloud.Region() /*subnetURL = */, "", frName, ipToUse, cloud.SchemeExternal, netTier, address.IPv4Version, frLogger)

		// If network tier annotation in Service Spec is present
		// check if it matches network tiers from forwarding rule and external ip Address.
		// If they do not match, tear down the existing resources with the wrong tier.
		if isFromAnnotation {
			if err := l4netlb.tearDownResourcesWithWrongNetworkTier(existingFwdRule, netTier, addrMgr, frLogger); err != nil {
				return nil, address.IPAddrUndefined, utils.ResourceResync, err
			}
		}

		ipToUse, isIPManaged, err = addrMgr.HoldAddress()
		if err != nil {
			return nil, address.IPAddrUndefined, utils.ResourceResync, err
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
		return nil, address.IPAddrUndefined, utils.ResourceResync, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", frName,
			err)
	}
	newFwdRule := &composite.ForwardingRule{
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
		newFwdRule.Ports = ports
		newFwdRule.PortRange = ""
	}

	if existingFwdRule != nil {
		if existingFwdRule.NetworkTier != newFwdRule.NetworkTier {
			resource := fmt.Sprintf("Forwarding rule (%v)", frName)
			networkTierMismatchError := utils.NewNetworkTierErr(resource, existingFwdRule.NetworkTier, newFwdRule.NetworkTier)
			return nil, address.IPAddrUndefined, utils.ResourceUpdate, networkTierMismatchError
		}
		equal, err := forwardingrules.EqualIPv4(existingFwdRule, newFwdRule)
		if err != nil {
			return existingFwdRule, address.IPAddrUndefined, utils.ResourceResync, err
		}
		if equal {
			// nothing to do
			frLogger.V(2).Info("ensureIPv4ForwardingRule: Skipping update of unchanged forwarding rule")
			return existingFwdRule, isIPManaged, utils.ResourceResync, nil
		}
		frDiff := cmp.Diff(existingFwdRule, newFwdRule)
		frLogger.V(2).Info("ensureIPv4ForwardingRule: forwarding rule changed.",
			"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", newFwdRule), "diff", frDiff)

		patchable, filtered := forwardingrules.PatchableIPv4(existingFwdRule, newFwdRule)
		if patchable {
			if err = l4netlb.forwardingRules.Patch(filtered); err != nil {
				return nil, address.IPAddrUndefined, utils.ResourceUpdate, err
			}
			l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s patched", existingFwdRule.Name)
		} else {
			if err := l4netlb.updateForwardingRule(existingFwdRule, newFwdRule, frLogger); err != nil {
				return nil, address.IPAddrUndefined, utils.ResourceUpdate, err
			}
		}

	} else {
		if err = l4netlb.createFwdRule(newFwdRule, frLogger); err != nil {
			return nil, address.IPAddrUndefined, utils.ResourceUpdate, err
		}
		l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s created", newFwdRule.Name)
	}
	createdFr, err := l4netlb.forwardingRules.Get(newFwdRule.Name)
	if err != nil {
		return nil, address.IPAddrUndefined, utils.ResourceUpdate, err
	}
	if createdFr == nil {
		return nil, address.IPAddrUndefined, utils.ResourceUpdate, fmt.Errorf("forwarding rule %s not found", newFwdRule.Name)
	}
	return createdFr, isIPManaged, utils.ResourceUpdate, err
}

func (l4netlb *L4NetLB) updateForwardingRule(existingFwdRule, newFr *composite.ForwardingRule, frLogger klog.Logger) error {
	if err := l4netlb.forwardingRules.Delete(existingFwdRule.Name); err != nil {
		return err
	}
	l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s deleted", existingFwdRule.Name)

	if err := l4netlb.createFwdRule(newFr, frLogger); err != nil {
		return err
	}
	l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s re-created", newFr.Name)
	return nil
}

func (l4netlb *L4NetLB) createFwdRule(newFr *composite.ForwardingRule, frLogger klog.Logger) error {
	frLogger.V(2).Info("ensureIPv4ForwardingRule: Creating/Recreating forwarding rule")
	if err := l4netlb.forwardingRules.Create(newFr); err != nil {
		if isAddressAlreadyInUseError(err) {
			return utils.NewIPConfigurationError(newFr.IPAddress, addressAlreadyInUseMessageExternal)
		}
		return err
	}
	return nil
}

// tearDownResourcesWithWrongNetworkTier removes forwarding rule or IP address if its Network Tier differs from desired.
func (l4netlb *L4NetLB) tearDownResourcesWithWrongNetworkTier(existingFwdRule *composite.ForwardingRule, svcNetTier cloud.NetworkTier, am *address.Manager, frLogger klog.Logger) error {
	if existingFwdRule != nil && existingFwdRule.NetworkTier != svcNetTier.ToGCEValue() {
		err := l4netlb.forwardingRules.Delete(existingFwdRule.Name)
		if err != nil {
			frLogger.Error(err, "l4netlb.forwardingRules.Delete returned error, want nil")
		}
	}
	return am.TearDownAddressIPIfNetworkTierMismatch()
}

func isAddressAlreadyInUseError(err error) bool {
	// Bad request HTTP status (400) is returned for external Forwarding Rules.
	alreadyInUseExternal := utils.IsHTTPErrorCode(err, http.StatusBadRequest) && strings.Contains(err.Error(), addressAlreadyInUseMessageExternal)
	// Conflict HTTP status (409) is returned for internal Forwarding Rules.
	alreadyInUseInternal := utils.IsHTTPErrorCode(err, http.StatusConflict) && strings.Contains(err.Error(), addressAlreadyInUseMessageInternal)
	return alreadyInUseExternal || alreadyInUseInternal
}
