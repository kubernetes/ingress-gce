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

package resources

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
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

	portsConflictMessage = "Forwarding rule's port range is conflicting with forwarding rule"
)

// ensureIPv4ForwardingRule creates a forwarding rule with the given name, if it does not exist. It updates the existing
// forwarding rule if needed.
//
// ipReserved reports whether ipToUse is held by an address reservation for the duration of the sync.
// The existing forwarding rule is only deleted and re-created when it is (see forwardingrules.CheckRecreateAllowed);
// otherwise an error is returned and the sync is retried.
func (l4 *L4) ensureIPv4ForwardingRule(bsLink string, options gce.ILBOptions, existingFwdRule *composite.ForwardingRule, subnetworkURL, ipToUse string, ipReserved bool) (*composite.ForwardingRule, l4utils.ResourceSyncStatus, error) {
	start := time.Now()

	frName := l4.GetFRName()

	frLogger := l4.svcLogger.WithValues("forwardingRuleName", frName)
	frLogger.V(2).Info("Ensuring internal forwarding rule for L4 ILB Service", "backendServiceLink", bsLink)
	defer func() {
		frLogger.V(2).Info("Finished ensuring internal forwarding rule for L4 ILB Service", "timeTaken", time.Since(start))
	}()

	newFwdRule, err := l4.buildExpectedIPv4ForwardingRule(bsLink, options, subnetworkURL, ipToUse)
	if err != nil {
		return nil, l4utils.ResourceResync, err
	}

	if existingFwdRule != nil {
		equal, err := forwardingrules.EqualIPv4(existingFwdRule, newFwdRule)
		if err != nil {
			return nil, l4utils.ResourceResync, err
		}
		if equal {
			// nothing to do
			frLogger.V(2).Info("ensureIPv4ForwardingRule: Skipping update of unchanged forwarding rule")
			return existingFwdRule, l4utils.ResourceResync, nil
		}
		frDiff := cmp.Diff(existingFwdRule, newFwdRule)
		frLogger.V(2).Info("ensureIPv4ForwardingRule: forwarding rule changed.",
			"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", newFwdRule), "diff", frDiff)

		patchable, filtered := forwardingrules.PatchableIPv4(existingFwdRule, newFwdRule)
		if patchable {
			if err = l4.forwardingRules.Patch(filtered); err != nil {
				return nil, l4utils.ResourceUpdate, err
			}
			l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s patched", existingFwdRule.Name)
		} else {
			if err := l4.updateForwardingRule(existingFwdRule, newFwdRule, ipReserved, frLogger); err != nil {
				return nil, l4utils.ResourceUpdate, err
			}
		}
	} else {
		if err = l4.createFwdRule(newFwdRule, frLogger); err != nil {
			return nil, l4utils.ResourceUpdate, err
		}
		l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s created", newFwdRule.Name)
	}

	readFwdRule, err := l4.forwardingRules.Get(newFwdRule.Name)
	if err != nil {
		return nil, l4utils.ResourceUpdate, err
	}
	if readFwdRule == nil {
		return nil, l4utils.ResourceUpdate, fmt.Errorf("Forwarding Rule %s not found", frName)
	}
	return readFwdRule, l4utils.ResourceUpdate, nil
}

// isIPv4ForwardingRuleUpToDate reports whether the existing IPv4 forwarding rule already
// matches what ensureIPv4ForwardingRule would create for the Service, so that the sync
// will neither create, patch nor delete the IPv4 forwarding rule.
//
// It is intentionally conservative: whenever it can't prove that nothing will change (no
// existing rule or backend service, a backend service protocol change that deletes the
// rule, an IP that is being reset, or an error while building or comparing the rules), it
// returns false and lets the regular path reserve the address and report any error.
func (l4 *L4) isIPv4ForwardingRuleUpToDate(existingBS *composite.BackendService, backendProtocol string, existingFwdRule *composite.ForwardingRule, options gce.ILBOptions, subnetworkURL, ipToUse string) bool {
	if existingBS == nil || existingBS.Protocol != backendProtocol || existingFwdRule == nil || ipToUse == "" {
		return false
	}
	wantedFwdRule, err := l4.buildExpectedIPv4ForwardingRule(existingBS.SelfLink, options, subnetworkURL, ipToUse)
	if err != nil {
		l4.svcLogger.V(2).Info("isIPv4ForwardingRuleUpToDate: failed to build expected forwarding rule, assuming it changed", "err", err)
		return false
	}
	equal, err := forwardingrules.EqualIPv4(existingFwdRule, wantedFwdRule)
	if err != nil {
		l4.svcLogger.V(2).Info("isIPv4ForwardingRuleUpToDate: failed to compare forwarding rules, assuming they differ", "err", err)
		return false
	}
	return equal
}

// isIPv6ForwardingRuleUpToDate is the IPv6 counterpart of isIPv4ForwardingRuleUpToDate.
func (l4 *L4) isIPv6ForwardingRuleUpToDate(existingBS *composite.BackendService, backendProtocol string, existingFwdRule *composite.ForwardingRule, options gce.ILBOptions, ipv6AddressToUse string) bool {
	if existingBS == nil || existingBS.Protocol != backendProtocol || existingFwdRule == nil || ipv6AddressToUse == "" {
		return false
	}
	// EqualIPv6 doesn't compare addresses, make sure the rule already uses the selected one.
	if !address.IsSameIPv6(ipv6AddressToUse, existingFwdRule.IPAddress) {
		return false
	}
	wantedFwdRule, err := l4.buildExpectedIPv6ForwardingRule(existingBS.SelfLink, options, ipv6AddressToUse)
	if err != nil {
		l4.svcLogger.V(2).Info("isIPv6ForwardingRuleUpToDate: failed to build expected forwarding rule, assuming it changed", "err", err)
		return false
	}
	equal, err := forwardingrules.EqualIPv6(existingFwdRule, wantedFwdRule)
	if err != nil {
		l4.svcLogger.V(2).Info("isIPv6ForwardingRuleUpToDate: failed to compare forwarding rules, assuming they differ", "err", err)
		return false
	}
	return equal
}

// buildExpectedIPv4ForwardingRule returns the IPv4 forwarding rule that
// ensureIPv4ForwardingRule wants for the Service, pointing at the given backend
// service and using the given subnetwork and IP.
func (l4 *L4) buildExpectedIPv4ForwardingRule(bsLink string, options gce.ILBOptions, subnetworkURL, ipToUse string) (*composite.ForwardingRule, error) {
	// version used for creating the forwarding rule.
	version := meta.VersionGA
	frName := l4.GetFRName()

	servicePorts := l4.Service.Spec.Ports
	ports := utils.GetPorts(servicePorts)
	protocol := string(utils.GetProtocol(servicePorts))
	allPorts := false
	if l4.enableMixedProtocol {
		protocol = forwardingrules.GetProtocol(servicePorts)
		if protocol == forwardingrules.ProtocolL3 {
			allPorts = true
			ports = nil
		}
	}
	if len(ports) > maxForwardedPorts {
		allPorts = true
		ports = nil
	}

	frDesc, err := utils.MakeL4LBServiceDescription(utils.ServiceKeyFunc(l4.Service.Namespace, l4.Service.Name), ipToUse,
		version, false, utils.ILB)
	if err != nil {
		return nil, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", frName, err)
	}

	return &composite.ForwardingRule{
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
	}, nil
}

// updateForwardingRule replaces the existing forwarding rule with newFr by deleting
// and re-creating it. ipReserved must report whether the IP is held by an address
// reservation, otherwise the rule is left in place and an error is returned.
func (l4 *L4) updateForwardingRule(existingFwdRule, newFr *composite.ForwardingRule, ipReserved bool, frLogger klog.Logger) error {
	if err := forwardingrules.CheckRecreateAllowed(l4.cloud, existingFwdRule, ipReserved); err != nil {
		return err
	}
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
			return l4utils.NewIPConfigurationError(newFr.IPAddress, err.Error())
		}
		if isConflictingPortsError(err) {
			return l4utils.NewConflictingPortsConfigurationError(newFr.PortRange, err.Error())
		}
		return err
	}
	return nil
}

// ensureIPv4ForwardingRule creates a forwarding rule with the given name for L4NetLB,
// if it does not exist. It updates the existing forwarding rule if needed.
// This should only handle single protocol forwarding rules.
func (l4netlb *L4NetLB) ensureIPv4ForwardingRule(bsLink string) (*composite.ForwardingRule, address.IPAddressType, l4utils.ResourceSyncStatus, error) {
	frName := l4netlb.frName()

	start := time.Now()
	frLogger := l4netlb.svcLogger.WithValues("forwardingRuleName", frName)
	frLogger.V(2).Info("Ensuring external forwarding rule for L4 NetLB Service", "backendServiceLink", bsLink)
	defer func() {
		frLogger.V(2).Info("Finished ensuring external forwarding rule for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	rules, err := l4netlb.mixedManager.AllRules()
	if err != nil {
		frLogger.Error(err, "l4netlb.mixedManager.AllRules returned error")
		return nil, address.IPAddrUndefined, l4utils.ResourceResync, err
	}
	existingRules := []*composite.ForwardingRule{rules.Legacy, rules.TCP, rules.UDP, rules.L3}

	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then ipToUse = "".
	ipToUse, ipv4AddressName, err := address.ExternalIPv4ToUse(l4netlb.cloud, l4netlb.recorder, l4netlb.Service, existingRules)
	if err != nil {
		frLogger.Error(err, "address.ExternalIPv4ToUse returned error")
		return nil, address.IPAddrUndefined, l4utils.ResourceResync, err
	}

	// Reserving the address only protects the IP while the forwarding rule is deleted and
	// re-created. When the existing rule already matches the Service nothing is deleted, so
	// skip the reservation (and its release) to avoid two address mutations on every resync.
	if l4netlb.isIPv4ForwardingRuleUpToDate(rules, bsLink, ipToUse, frLogger) {
		frLogger.V(2).Info("ensureIPv4ForwardingRule: Skipping address reservation and update of unchanged forwarding rule")
		return rules.Legacy, address.ManagementTypeFromSpec(l4netlb.Service, ipv4AddressName), l4utils.ResourceResync, nil
	}

	addrHandle, err := address.HoldExternalIPv4(address.HoldConfig{
		Cloud:                 l4netlb.cloud,
		Logger:                l4netlb.svcLogger,
		Service:               l4netlb.Service,
		IP:                    ipToUse,
		AddressName:           ipv4AddressName,
		ExistingRules:         existingRules,
		ForwardingRuleDeleter: l4netlb.forwardingRules,
	})
	if err != nil {
		frLogger.Error(err, "address.HoldExternalIPv4 returned error")
		return nil, address.IPAddrUndefined, l4utils.ResourceResync, err
	}
	defer func() {
		err = addrHandle.Release()
		if err != nil {
			frLogger.Error(err, "addrHandle.Release returned error")
		}
	}()
	// From here on, forwarding rules using the IP may only be deleted while it is reserved.
	ipReserved := addrHandle.Reserved

	// Leaving this without feature flag, so after rollback
	// forwarding rules will be cleaned up
	for _, leftover := range []*composite.ForwardingRule{rules.TCP, rules.UDP, rules.L3} {
		if leftover == nil {
			continue
		}
		if err := forwardingrules.CheckRecreateAllowed(l4netlb.cloud, leftover, ipReserved); err != nil {
			return nil, address.IPAddrUndefined, l4utils.ResourceResync, err
		}
	}
	if err := l4netlb.mixedManager.DeleteExistingIPv4(rules); err != nil {
		frLogger.Error(err, "l4netlb.mixedManager.DeleteExistingIPv4 returned an error")
		return nil, address.IPAddrUndefined, l4utils.ResourceResync, err
	}

	existingFwdRule := rules.Legacy
	ipToUse = addrHandle.IP
	isIPManaged := addrHandle.Managed
	newFwdRule, err := l4netlb.buildExpectedIPv4ForwardingRule(bsLink, ipToUse)
	if err != nil {
		return nil, address.IPAddrUndefined, l4utils.ResourceResync, err
	}

	if existingFwdRule != nil {
		if existingFwdRule.NetworkTier != newFwdRule.NetworkTier {
			resource := fmt.Sprintf("Forwarding rule (%v)", frName)
			networkTierMismatchError := l4utils.NewNetworkTierErr(resource, existingFwdRule.NetworkTier, newFwdRule.NetworkTier)
			return nil, address.IPAddrUndefined, l4utils.ResourceUpdate, networkTierMismatchError
		}
		equal, err := forwardingrules.EqualIPv4(existingFwdRule, newFwdRule)
		if err != nil {
			return existingFwdRule, address.IPAddrUndefined, l4utils.ResourceResync, err
		}
		if equal {
			// nothing to do
			frLogger.V(2).Info("ensureIPv4ForwardingRule: Skipping update of unchanged forwarding rule")
			return existingFwdRule, isIPManaged, l4utils.ResourceResync, nil
		}
		frDiff := cmp.Diff(existingFwdRule, newFwdRule)
		frLogger.V(2).Info("ensureIPv4ForwardingRule: forwarding rule changed.",
			"existingForwardingRule", fmt.Sprintf("%+v", existingFwdRule), "newForwardingRule", fmt.Sprintf("%+v", newFwdRule), "diff", frDiff)

		patchable, filtered := forwardingrules.PatchableIPv4(existingFwdRule, newFwdRule)
		if patchable {
			if err = l4netlb.forwardingRules.Patch(filtered); err != nil {
				return nil, address.IPAddrUndefined, l4utils.ResourceUpdate, err
			}
			l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s patched", existingFwdRule.Name)
		} else {
			if err := l4netlb.updateForwardingRule(existingFwdRule, newFwdRule, ipReserved, frLogger); err != nil {
				return nil, address.IPAddrUndefined, l4utils.ResourceUpdate, err
			}
		}

	} else {
		if err = l4netlb.createFwdRule(newFwdRule, frLogger); err != nil {
			return nil, address.IPAddrUndefined, l4utils.ResourceUpdate, err
		}
		l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %s created", newFwdRule.Name)
	}
	createdFr, err := l4netlb.forwardingRules.Get(newFwdRule.Name)
	if err != nil {
		return nil, address.IPAddrUndefined, l4utils.ResourceUpdate, err
	}
	if createdFr == nil {
		return nil, address.IPAddrUndefined, l4utils.ResourceUpdate, fmt.Errorf("forwarding rule %s not found", newFwdRule.Name)
	}
	return createdFr, isIPManaged, l4utils.ResourceUpdate, err
}

// isIPv4ForwardingRuleUpToDate reports whether the existing single protocol forwarding
// rule already matches what ensureIPv4ForwardingRule would create for the Service, so
// that the sync will neither create, patch nor delete any IPv4 forwarding rule.
//
// It is intentionally conservative: whenever it can't prove that nothing will change
// (no existing rule, leftover mixed protocol rules that have to be deleted, an IP that
// is being reset, or an error while building or comparing the rules), it returns false
// and lets the regular path reserve the address and report any error.
func (l4netlb *L4NetLB) isIPv4ForwardingRuleUpToDate(rules forwardingrules.NetLBManagedRules, bsLink, ipToUse string, frLogger klog.Logger) bool {
	if rules.Legacy == nil || ipToUse == "" {
		return false
	}
	// Mixed protocol rules are deleted by ensureIPv4ForwardingRule, the IP must be held while they are.
	if rules.TCP != nil || rules.UDP != nil || rules.L3 != nil {
		return false
	}
	wantedFwdRule, err := l4netlb.buildExpectedIPv4ForwardingRule(bsLink, ipToUse)
	if err != nil {
		frLogger.V(2).Info("isIPv4ForwardingRuleUpToDate: failed to build expected forwarding rule, assuming it changed", "err", err)
		return false
	}
	equal, err := forwardingrules.EqualIPv4(rules.Legacy, wantedFwdRule)
	if err != nil {
		frLogger.V(2).Info("isIPv4ForwardingRuleUpToDate: failed to compare forwarding rules, assuming they differ", "err", err)
		return false
	}
	return equal
}

// buildExpectedIPv4ForwardingRule returns the single protocol IPv4 forwarding rule
// that ensureIPv4ForwardingRule wants for the Service, pointing at the given backend
// service and using the given IP.
func (l4netlb *L4NetLB) buildExpectedIPv4ForwardingRule(bsLink, ipToUse string) (*composite.ForwardingRule, error) {
	// version used for creating the forwarding rule.
	version := meta.VersionGA
	frName := l4netlb.frName()
	netTier, _ := annotations.NetworkTier(l4netlb.Service)
	svcPorts := l4netlb.Service.Spec.Ports
	ports := utils.GetPorts(svcPorts)
	portRange := utils.MinMaxPortRange(svcPorts)
	protocol := utils.GetProtocol(svcPorts)
	serviceKey := utils.ServiceKeyFunc(l4netlb.Service.Namespace, l4netlb.Service.Name)
	frDesc, err := utils.MakeL4LBServiceDescription(serviceKey, ipToUse, version, false, utils.XLB)
	if err != nil {
		return nil, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", frName, err)
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
	return fr, nil
}

// updateForwardingRule replaces the existing forwarding rule with newFr by deleting
// and re-creating it. ipReserved must report whether the IP is held by an address
// reservation, otherwise the rule is left in place and an error is returned.
func (l4netlb *L4NetLB) updateForwardingRule(existingFwdRule, newFr *composite.ForwardingRule, ipReserved bool, frLogger klog.Logger) error {
	if err := forwardingrules.CheckRecreateAllowed(l4netlb.cloud, existingFwdRule, ipReserved); err != nil {
		return err
	}
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
			return l4utils.NewIPConfigurationError(newFr.IPAddress, addressAlreadyInUseMessageExternal)
		}
		if isConflictingPortsError(err) {
			return l4utils.NewConflictingPortsConfigurationError(newFr.PortRange, err.Error())
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

func isConflictingPortsError(err error) bool {
	portsConflictError := utils.IsHTTPErrorCode(err, http.StatusBadRequest) && strings.Contains(err.Error(), portsConflictMessage)
	return portsConflictError
}
