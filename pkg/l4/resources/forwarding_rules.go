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
		if isConflictingPortsError(err) {
			return utils.NewConflictingPortsConfigurationError(newFr.PortRange, err.Error())
		}
		return err
	}
	return nil
}

// ensureIPv4ForwardingRule creates a forwarding rule with the given name for L4NetLB,
// if it does not exist. It updates the existing forwarding rule if needed.
// This should only handle single protocol forwarding rules.
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
	rules, err := l4netlb.mixedManager.AllRules()
	if err != nil {
		frLogger.Error(err, "l4netlb.mixedManager.AllRules returned error")
		return nil, address.IPAddrUndefined, utils.ResourceResync, err
	}

	addrHandle, err := address.HoldExternalIPv4(address.HoldConfig{
		Cloud:                 l4netlb.cloud,
		Recorder:              l4netlb.recorder,
		Logger:                l4netlb.svcLogger,
		Service:               l4netlb.Service,
		ExistingRules:         []*composite.ForwardingRule{rules.Legacy, rules.TCP, rules.UDP},
		ForwardingRuleDeleter: l4netlb.forwardingRules,
	})
	if err != nil {
		frLogger.Error(err, "address.HoldExternalIPv4 returned error")
		return nil, address.IPAddrUndefined, utils.ResourceResync, err
	}
	defer func() {
		err = addrHandle.Release()
		if err != nil {
			frLogger.Error(err, "addrHandle.Release returned error")
		}
	}()

	// Leaving this without feature flag, so after rollback
	// forwarding rules will be cleaned up
	mixedRulesExist := rules.TCP != nil || rules.UDP != nil
	if mixedRulesExist {
		if err := l4netlb.mixedManager.DeleteIPv4(); err != nil {
			frLogger.Error(err, "l4netlb.mixedManager.DeleteIPv4 returned an error")
			return nil, address.IPAddrUndefined, utils.ResourceResync, err
		}
	}

	existingFwdRule := rules.Legacy
	ipToUse := addrHandle.IP
	isIPManaged := addrHandle.Managed
	netTier, _ := annotations.NetworkTier(l4netlb.Service)
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
		if isConflictingPortsError(err) {
			return utils.NewConflictingPortsConfigurationError(newFr.PortRange, err.Error())
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
