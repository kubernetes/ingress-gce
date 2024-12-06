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
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/utils"
)

// ensureIPv6Resources creates resources specific to IPv6 L4 Load Balancers:
// - IPv6 Forwarding Rule
// - IPv6 Firewall
// it also adds IPv6 address to LB status
func (l4 *L4) ensureIPv6Resources(syncResult *L4ILBSyncResult, nodeNames []string, options gce.ILBOptions, bsLink string, existingIPv6FwdRule *composite.ForwardingRule, ipv6AddressToUse string) {
	ipv6fr, fwdRuleSyncStatus, err := l4.ensureIPv6ForwardingRule(bsLink, options, existingIPv6FwdRule, ipv6AddressToUse)
	syncResult.ResourceUpdates.SetForwardingRule(fwdRuleSyncStatus)
	if err != nil {
		l4.svcLogger.Error(err, "ensureIPv6Resources: Failed to ensure ipv6 forwarding rule")
		syncResult.GCEResourceInError = annotations.ForwardingRuleIPv6Resource
		syncResult.Error = err
		return
	}

	switch ipv6fr.IPProtocol {
	case forwardingrules.ProtocolTCP:
		syncResult.Annotations[annotations.TCPForwardingRuleIPv6Key] = ipv6fr.Name
	case forwardingrules.ProtocolUDP:
		syncResult.Annotations[annotations.UDPForwardingRuleIPv6Key] = ipv6fr.Name
	case forwardingrules.ProtocolL3:
		syncResult.Annotations[annotations.L3ForwardingRuleIPv6Key] = ipv6fr.Name
	}

	// Google Cloud creates ipv6 forwarding rules with IPAddress in CIDR form. We will take only first address
	trimmedIPv6Address := strings.Split(ipv6fr.IPAddress, "/")[0]

	l4.ensureIPv6NodesFirewall(trimmedIPv6Address, nodeNames, syncResult)
	if syncResult.Error != nil {
		l4.svcLogger.Error(err, "ensureIPv6Resources: Failed to ensure ipv6 nodes firewall for L4 ILB")
		return
	}

	syncResult.Status = utils.AddIPToLBStatus(syncResult.Status, trimmedIPv6Address)
}

// deleteIPv6ResourcesOnSync deletes resources specific to IPv6,
// only if corresponding resource annotation exist on Service object.
// This function is called only on Service update or periodic sync.
// Checking for annotation saves us from emitting too much error logs "Resource not found".
// If annotation was deleted, but resource still exists, it will be left till the Service deletion,
// where we delete all resources, no matter if they exist in annotations.
func (l4 *L4) deleteIPv6ResourcesOnSync(syncResult *L4ILBSyncResult) {
	l4.svcLogger.Info("Deleting IPv6 resources for L4 ILB Service on sync, with checking for existence in annotation")
	l4.deleteIPv6ResourcesAnnotationBased(syncResult, true)
}

// deleteIPv6ResourcesOnDelete deletes all resources specific to IPv6.
// This function is called only on Service deletion.
// During sync, we delete resources only that exist in annotations,
// so they could be leaked, if annotation was deleted.
// That's why on service deletion we delete all IPv4 resources, ignoring their existence in annotations
func (l4 *L4) deleteIPv6ResourcesOnDelete(syncResult *L4ILBSyncResult) {
	l4.svcLogger.Info("Deleting IPv6 resources for L4 ILB Service on delete, without checking for existence in annotation")
	l4.deleteIPv6ResourcesAnnotationBased(syncResult, false)
}

// deleteIPv6ResourcesAnnotationBased deletes IPv6 only resources with checking,
// if resource exists in Service annotation, if shouldIgnoreAnnotations not set to true
// IPv6 Specific resources:
// - IPv6 Forwarding Rule
// - IPv6 Firewall
// This function does not delete Backend Service and Health Check, because they are shared between IPv4 and IPv6.
// IPv6 Firewall Rule for Health Check also will not be deleted here, and will be left till the Service Deletion.
func (l4 *L4) deleteIPv6ResourcesAnnotationBased(syncResult *L4ILBSyncResult, shouldCheckAnnotations bool) {
	hasFwdRuleAnnotation := l4.hasAnnotation(annotations.TCPForwardingRuleIPv6Key) ||
		l4.hasAnnotation(annotations.UDPForwardingRuleIPv6Key) ||
		l4.hasAnnotation(annotations.L3ForwardingRuleIPv6Key)

	if !shouldCheckAnnotations || hasFwdRuleAnnotation {
		var err error
		if l4.enableDualStack && l4.enableMixedProtocol {
			err = l4.deleteAllIPv6ForwardingRules()
		} else {
			err = l4.deleteIPv6ForwardingRule()
		}
		if err != nil {
			l4.svcLogger.Error(err, "Failed to delete ipv6 forwarding rule for internal loadbalancer service")
			syncResult.Error = err
			syncResult.GCEResourceInError = annotations.ForwardingRuleIPv6Resource
		}
	}

	if !shouldCheckAnnotations || l4.hasAnnotation(annotations.FirewallRuleIPv6Key) {
		err := l4.deleteIPv6NodesFirewall()
		if err != nil {
			l4.svcLogger.Error(err, "Failed to delete ipv6 firewall rule for internal loadbalancer service")
			syncResult.GCEResourceInError = annotations.FirewallRuleIPv6Resource
			syncResult.Error = err
		}
	}
}

func (l4 *L4) getIPv6FRName() string {
	protocol := string(utils.GetProtocol(l4.Service.Spec.Ports))
	if l4.enableMixedProtocol {
		protocol = forwardingrules.GetILBProtocol(l4.Service.Spec.Ports)
	}
	return l4.getIPv6FRNameWithProtocol(protocol)
}

func (l4 *L4) getIPv6FRNameWithProtocol(protocol string) string {
	return l4.namer.L4IPv6ForwardingRule(l4.Service.Namespace, l4.Service.Name, strings.ToLower(protocol))
}

func (l4 *L4) ensureIPv6NodesFirewall(ipAddress string, nodeNames []string, result *L4ILBSyncResult) {
	// DisableL4LBFirewall flag disables L4 FW enforcment to remove conflicts with firewall policies
	if l4.disableNodesFirewallProvisioning {
		l4.svcLogger.Info("Skipped ensuring IPv6 nodes firewall for L4 ILB Service to enable compatibility with firewall policies. " +
			"Be sure this cluster has a manually created global firewall policy in place.")
		return
	}
	start := time.Now()

	firewallName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)

	svcPorts := l4.Service.Spec.Ports
	portRanges := utils.GetServicePortRanges(svcPorts)
	protocol := utils.GetProtocol(svcPorts)
	allowed := []*compute.FirewallAllowed{
		{
			IPProtocol: string(protocol),
			Ports:      portRanges,
		},
	}

	if l4.enableMixedProtocol {
		allowed = firewalls.AllowedForService(svcPorts)
	}

	fwLogger := l4.svcLogger.WithValues("firewallName", firewallName)
	fwLogger.V(2).Info("Ensuring IPv6 nodes firewall for L4 ILB Service", "ipAddress", ipAddress, "protocol", protocol, "len(nodeNames)", len(nodeNames), "portRanges", portRanges)
	defer func() {
		fwLogger.V(2).Info("Finished ensuring IPv6 nodes firewall for L4 ILB Service", "timeTaken", time.Since(start))
	}()

	// ensure firewalls
	ipv6SourceRanges, err := utils.IPv6ServiceSourceRanges(l4.Service)
	if err != nil {
		result.Error = err
		return
	}

	ipv6nodesFWRParams := firewalls.FirewallParams{
		Allowed:           allowed,
		SourceRanges:      ipv6SourceRanges,
		DestinationRanges: []string{ipAddress},
		Name:              firewallName,
		NodeNames:         nodeNames,
		L4Type:            utils.ILB,
		Network:           l4.network,
	}

	fwSyncStatus, err := firewalls.EnsureL4LBFirewallForNodes(l4.Service, &ipv6nodesFWRParams, l4.cloud, l4.recorder, fwLogger)
	result.ResourceUpdates.SetFirewallForNodes(fwSyncStatus)
	if err != nil {
		result.GCEResourceInError = annotations.FirewallRuleIPv6Resource
		result.Error = err
		return
	}
	result.Annotations[annotations.FirewallRuleIPv6Key] = firewallName
}

func (l4 *L4) deleteIPv6ForwardingRule() error {
	start := time.Now()

	ipv6FrName := l4.getIPv6FRName()

	l4.svcLogger.V(2).Info("Deleting IPv6 forwarding rule for L4 ILB Service", "forwardingRuleName", ipv6FrName)
	defer func() {
		l4.svcLogger.V(2).Info("Finished deleting IPv6 forwarding rule for L4 ILB Service", "forwardingRuleName", ipv6FrName, "timeTaken", time.Since(start))
	}()

	return l4.forwardingRules.Delete(ipv6FrName)
}

// When switching from for example IPv6 TCP to IPv4 UDP, simply using GetFRName()
// to delete won't be accurate since it will try to delete *IPv6 UDP* name not *TCP*,
// and the UDP forwarding rule will be leaked.
func (l4 *L4) deleteAllIPv6ForwardingRules() error {
	start := time.Now()

	frNameTCP := l4.getIPv6FRNameWithProtocol(forwardingrules.ProtocolTCP)
	frNameUDP := l4.getIPv6FRNameWithProtocol(forwardingrules.ProtocolUDP)
	frNameL3 := l4.getIPv6FRNameWithProtocol(forwardingrules.ProtocolL3)

	l4.svcLogger.Info("Deleting all potential IPv6 forwarding rule for L4 ILB Service",
		"forwardingRuleNameTCP", frNameTCP, "forwardingRuleNameUDP", frNameUDP, "forwardingRuleNameL3", frNameL3)
	defer func() {
		l4.svcLogger.Info("Finished deleting all IPv6 forwarding rule for L4 ILB Service", "forwardingRuleNameTCP", frNameTCP, "forwardingRuleNameUDP", frNameUDP, "forwardingRuleNameL3", frNameL3, "timeTaken", time.Since(start))
	}()

	errTCP := l4.forwardingRules.Delete(frNameTCP)
	errUDP := l4.forwardingRules.Delete(frNameUDP)
	errL3 := l4.forwardingRules.Delete(frNameL3)

	return errors.Join(errTCP, errUDP, errL3)
}

func (l4 *L4) deleteIPv6NodesFirewall() error {
	ipv6FirewallName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)

	start := time.Now()
	fwLogger := l4.svcLogger.WithValues("firewallName", ipv6FirewallName)
	fwLogger.V(2).Info("Deleting IPv6 nodes firewall for L4 ILB Service")
	defer func() {
		fwLogger.V(2).Info("Finished deleting IPv6 nodes firewall for L4 ILB Service", "timeTaken", time.Since(start))
	}()

	return l4.deleteFirewall(ipv6FirewallName, fwLogger)
}

// getOldIPv6ForwardingRule returns old IPv6 forwarding rule, with checking backend service protocol, if it exists.
// This is useful when switching protocols of the service,
// because forwarding rule name depends on the protocol, and we need to get forwarding rule from the old protocol name.
func (l4 *L4) getOldIPv6ForwardingRule(existingBS *composite.BackendService) (*composite.ForwardingRule, error) {
	servicePorts := l4.Service.Spec.Ports
	bsProtocol := string(utils.GetProtocol(servicePorts))

	if l4.enableMixedProtocol {
		bsProtocol = backends.GetProtocol(servicePorts)
	}

	oldIPv6FRName := l4.getIPv6FRName()
	if existingBS != nil && existingBS.Protocol != bsProtocol {
		fwdRuleProtocol := existingBS.Protocol
		if existingBS.Protocol == backends.ProtocolL3 {
			fwdRuleProtocol = forwardingrules.ProtocolL3
		}
		oldIPv6FRName = l4.getIPv6FRNameWithProtocol(fwdRuleProtocol)
	}

	return l4.forwardingRules.Get(oldIPv6FRName)
}

func (l4 *L4) serviceSubnetHasInternalIPv6Range() error {
	subnetName := l4.subnetName()
	hasIPv6SubnetRange, err := utils.SubnetHasIPv6Range(l4.cloud, subnetName, subnetInternalIPv6AccessType)
	if err != nil {
		return err
	}
	if !hasIPv6SubnetRange {
		// We don't need to emit custom event, because errors are already emitted to the user as events.
		l4.svcLogger.Info("Subnet for IPv6 Service does not have internal IPv6 ranges", "subnetName", subnetName)
		return utils.NewUserError(
			fmt.Errorf(
				"subnet %s does not have internal IPv6 ranges, required for an internal IPv6 Service. You can specify an internal IPv6 subnet using the \"%s\" annotation on the Service", subnetName, annotations.CustomSubnetAnnotationKey))
	}
	return nil
}
