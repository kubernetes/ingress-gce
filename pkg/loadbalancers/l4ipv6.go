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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/utils"
)

// ensureIPv6Resources creates resources specific to IPv6 L4 Load Balancers:
// - IPv6 Forwarding Rule
// - IPv6 Firewall
// it also adds IPv6 address to LB status
func (l4 *L4) ensureIPv6Resources(syncResult *L4ILBSyncResult, nodeNames []string, options gce.ILBOptions, bsLink string, existingIPv6FwdRule *composite.ForwardingRule, ipv6AddressToUse string) {
	ipv6fr, err := l4.ensureIPv6ForwardingRule(bsLink, options, existingIPv6FwdRule, ipv6AddressToUse)
	if err != nil {
		l4.svcLogger.Error(err, "ensureIPv6Resources: Failed to ensure ipv6 forwarding rule")
		syncResult.GCEResourceInError = annotations.ForwardingRuleIPv6Resource
		syncResult.Error = err
		return
	}

	if ipv6fr.IPProtocol == string(corev1.ProtocolTCP) {
		syncResult.Annotations[annotations.TCPForwardingRuleIPv6Key] = ipv6fr.Name
	} else {
		syncResult.Annotations[annotations.UDPForwardingRuleIPv6Key] = ipv6fr.Name
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
	if !shouldCheckAnnotations || l4.hasAnnotation(annotations.TCPForwardingRuleIPv6Key) || l4.hasAnnotation(annotations.UDPForwardingRuleIPv6Key) {
		err := l4.deleteIPv6ForwardingRule()
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
	protocol := utils.GetProtocol(l4.Service.Spec.Ports)
	return l4.getIPv6FRNameWithProtocol(string(protocol))
}

func (l4 *L4) getIPv6FRNameWithProtocol(protocol string) string {
	return l4.namer.L4IPv6ForwardingRule(l4.Service.Namespace, l4.Service.Name, strings.ToLower(protocol))
}

func (l4 *L4) ensureIPv6NodesFirewall(ipAddress string, nodeNames []string, result *L4ILBSyncResult) {
	start := time.Now()

	firewallName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)

	svcPorts := l4.Service.Spec.Ports
	portRanges := utils.GetServicePortRanges(svcPorts)
	protocol := utils.GetProtocol(svcPorts)

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
		PortRanges:        portRanges,
		SourceRanges:      ipv6SourceRanges,
		DestinationRanges: []string{ipAddress},
		Protocol:          string(protocol),
		Name:              firewallName,
		NodeNames:         nodeNames,
		L4Type:            utils.ILB,
		Network:           l4.network,
	}

	err = firewalls.EnsureL4LBFirewallForNodes(l4.Service, &ipv6nodesFWRParams, l4.cloud, l4.recorder, fwLogger)
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
	protocol := utils.GetProtocol(servicePorts)

	oldIPv6FRName := l4.getIPv6FRName()
	if existingBS != nil && existingBS.Protocol != string(protocol) {
		oldIPv6FRName = l4.getIPv6FRNameWithProtocol(existingBS.Protocol)
	}

	return l4.forwardingRules.Get(oldIPv6FRName)
}

func (l4 *L4) serviceSubnetHasInternalIPv6Range() error {
	subnetName := l4.subnetName()
	hasIPv6SubnetRange, err := utils.SubnetHasIPv6Range(l4.cloud, subnetName, subnetInternalAccessType)
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

func (l4 *L4) serviceSubnetHasInternalIPv4Range() error {
	subnetName := l4.subnetName()
	hasIPv6SubnetRange, err := utils.SubnetHasIPv4Range(l4.cloud, subnetName, subnetInternalAccessType)
	if err != nil {
		return err
	}
	if !hasIPv6SubnetRange {
		// We don't need to emit custom event, because errors are already emitted to the user as events.
		l4.svcLogger.Info("Subnet for IPv4 Service does not have internal IPv4 ranges", "subnetName", subnetName)
		return utils.NewUserError(
			fmt.Errorf(
				"subnet %s does not have internal IPv4 ranges, required for an internal IPv4 Service. You can specify an internal IPv4 subnet using the \"%s\" annotation on the Service", subnetName, annotations.CustomSubnetAnnotationKey))
	}
	return nil
}
