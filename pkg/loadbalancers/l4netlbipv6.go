/*
Copyright 2023 The Kubernetes Authors.

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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	compute "google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	ipv6Suffix                   = "-ipv6"
	subnetExternalIPv6AccessType = "EXTERNAL"
)

// ensureIPv6Resources creates resources specific to IPv6 L4 NetLB Load Balancers:
// - IPv6 Forwarding Rule
// - IPv6 Firewall
// it also adds IPv6 address to LB status
func (l4netlb *L4NetLB) ensureIPv6Resources(syncResult *L4NetLBSyncResult, nodeNames []string, bsLink string) {
	ipv6fr, wasUpdate, err := l4netlb.ensureIPv6ForwardingRule(bsLink)
	syncResult.GCEResourceUpdate.SetForwardingRule(wasUpdate)
	if err != nil {
		l4netlb.svcLogger.Error(err, "ensureIPv6Resources: Failed to create ipv6 forwarding rule")
		syncResult.GCEResourceInError = annotations.ForwardingRuleIPv6Resource
		syncResult.Error = err
		return
	}

	if ipv6fr.IPProtocol == string(corev1.ProtocolTCP) {
		syncResult.Annotations[annotations.TCPForwardingRuleIPv6Key] = ipv6fr.Name
	} else {
		syncResult.Annotations[annotations.UDPForwardingRuleIPv6Key] = ipv6fr.Name
	}
	syncResult.GCEResourceURLs = append(syncResult.GCEResourceURLs, ipv6fr.SelfLink)

	// Google Cloud creates ipv6 forwarding rules with IPAddress in CIDR form. We will take only first address
	trimmedIPv6Address := strings.Split(ipv6fr.IPAddress, "/")[0]

	l4netlb.ensureIPv6NodesFirewall(trimmedIPv6Address, nodeNames, syncResult)
	if syncResult.Error != nil {
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
func (l4netlb *L4NetLB) deleteIPv6ResourcesOnSync(syncResult *L4NetLBSyncResult) {
	l4netlb.deleteIPv6ResourcesAnnotationBased(syncResult, false)
}

// deleteIPv6ResourcesOnDelete deletes all resources specific to IPv6.
// This function is called only on Service deletion.
// During sync, we delete resources only that exist in annotations,
// so they could be leaked, if annotation was deleted.
// That's why on service deletion we delete all IPv4 resources, ignoring their existence in annotations
func (l4netlb *L4NetLB) deleteIPv6ResourcesOnDelete(syncResult *L4NetLBSyncResult) {
	l4netlb.deleteIPv6ResourcesAnnotationBased(syncResult, true)
}

// deleteIPv6ResourcesAnnotationBased deletes IPv6 only resources with checking,
// if resource exists in Service annotation, if shouldIgnoreAnnotations not set to true
// IPv6 Specific resources:
// - IPv6 Forwarding Rule
// - IPv6 Firewall
// This function does not delete Backend Service and Health Check, because they are shared between IPv4 and IPv6.
// IPv6 Firewall Rule for Health Check also will not be deleted here, and will be left till the Service Deletion.
func (l4netlb *L4NetLB) deleteIPv6ResourcesAnnotationBased(syncResult *L4NetLBSyncResult, shouldIgnoreAnnotations bool) {
	if shouldIgnoreAnnotations || l4netlb.hasAnnotation(annotations.TCPForwardingRuleIPv6Key) || l4netlb.hasAnnotation(annotations.UDPForwardingRuleIPv6Key) {
		l4netlb.deleteIPv6ForwardingRule(syncResult)
	}

	if shouldIgnoreAnnotations || l4netlb.hasAnnotation(annotations.FirewallRuleIPv6Key) {
		l4netlb.deleteIPv6NodesFirewall(syncResult)
	}
}

func (l4netlb *L4NetLB) ipv6FRName() string {
	return namer.GetSuffixedName(l4netlb.frName(), ipv6Suffix)
}

func (l4netlb *L4NetLB) ensureIPv6NodesFirewall(ipAddress string, nodeNames []string, syncResult *L4NetLBSyncResult) {
	// DisableL4LBFirewall flag disables L4 FW enforcment to remove conflicts with firewall policies
	if l4netlb.disableNodesFirewallProvisioning {
		l4netlb.svcLogger.Info("Skipped ensuring IPv6 nodes firewall for L4 NetLB Service to enable compatibility with firewall policies. " +
			"Be sure this cluster has a manually created global firewall policy in place.")
		return
	}
	start := time.Now()

	firewallName := l4netlb.namer.L4IPv6Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)
	svcPorts := l4netlb.Service.Spec.Ports
	portRanges := utils.GetServicePortRanges(svcPorts)
	protocol := utils.GetProtocol(svcPorts)

	fwLogger := l4netlb.svcLogger.WithValues("firewallName", firewallName)
	fwLogger.V(2).Info("Ensuring IPv6 nodes firewall for L4 NetLB Service", "ipAddress", ipAddress, "protocol", protocol, "len(nodeNames)", len(nodeNames), "portRanges", portRanges)
	defer func() {
		fwLogger.V(2).Info("Finished ensuring IPv6 nodes firewall for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	// ensure firewalls
	ipv6SourceRanges, err := utils.IPv6ServiceSourceRanges(l4netlb.Service)
	if err != nil {
		syncResult.Error = err
		return
	}

	ipv6nodesFWRParams := firewalls.FirewallParams{
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: string(protocol),
				Ports:      portRanges,
			},
		},
		SourceRanges:      ipv6SourceRanges,
		DestinationRanges: []string{ipAddress},
		Name:              firewallName,
		NodeNames:         nodeNames,
		L4Type:            utils.XLB,
		Network:           l4netlb.networkInfo,
	}

	firewallRule, wasUpdate, err := firewalls.EnsureL4LBFirewallForNodes(l4netlb.Service, &ipv6nodesFWRParams, l4netlb.cloud, l4netlb.recorder, fwLogger)
	syncResult.GCEResourceUpdate.SetFirewallForNodes(wasUpdate)
	if err != nil {
		fwLogger.Error(err, "Failed to ensure ipv6 nodes firewall for L4 NetLB")
		syncResult.GCEResourceInError = annotations.FirewallRuleIPv6Resource
		syncResult.Error = err
		return
	}
	syncResult.Annotations[annotations.FirewallRuleIPv6Key] = firewallName
	syncResult.GCEResourceURLs = append(syncResult.GCEResourceURLs, firewallRule.SelfLink)
}

func (l4netlb *L4NetLB) deleteIPv6ForwardingRule(syncResult *L4NetLBSyncResult) {
	ipv6FrName := l4netlb.ipv6FRName()

	start := time.Now()
	l4netlb.svcLogger.V(2).Info("Deleting IPv6 forwarding rule for L4 NetLB Service", "forwardingRuleName", ipv6FrName)
	defer func() {
		l4netlb.svcLogger.V(2).Info("Finished deleting IPv6 forwarding rule for L4 NetLB Service", "forwardingRuleName", ipv6FrName, "timeTaken", time.Since(start))
	}()

	err := l4netlb.forwardingRules.Delete(ipv6FrName)
	if err != nil {
		l4netlb.svcLogger.Error(err, "Failed to delete ipv6 forwarding rule for external loadbalancer service")
		syncResult.Error = err
		syncResult.GCEResourceInError = annotations.ForwardingRuleIPv6Resource
	}
}

func (l4netlb *L4NetLB) deleteIPv6NodesFirewall(syncResult *L4NetLBSyncResult) {
	ipv6FirewallName := l4netlb.namer.L4IPv6Firewall(l4netlb.Service.Namespace, l4netlb.Service.Name)

	start := time.Now()
	fwLogger := l4netlb.svcLogger.WithValues("firewallName", ipv6FirewallName)
	fwLogger.V(2).Info("Deleting IPv6 nodes firewall for L4 NetLB Service")
	defer func() {
		fwLogger.V(2).Info("Finished deleting IPv6 nodes firewall for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	err := l4netlb.deleteFirewall(ipv6FirewallName, fwLogger)
	if err != nil {
		fwLogger.Error(err, "Failed to delete ipv6 firewall rule for external loadbalancer service")
		syncResult.GCEResourceInError = annotations.FirewallRuleIPv6Resource
		syncResult.Error = err
	}
}

func (l4netlb *L4NetLB) ipv6SubnetURL() (string, error) {
	// at first, try to get subnet from annotation
	if subnetName := annotations.FromService(l4netlb.Service).GetExternalLoadBalancerAnnotationSubnet(); subnetName != "" {
		subnetKey, err := l4netlb.createKey(subnetName)
		if err != nil {
			return "", err
		}
		return cloud.SelfLink(meta.VersionGA, l4netlb.cloud.NetworkProjectID(), "subnetworks", subnetKey), nil
	}
	// if no subnet in annotation, use cluster subnet
	return l4netlb.cloud.SubnetworkURL(), nil
}

func (l4netlb *L4NetLB) ipv6SubnetName() string {
	// At first check custom subnet annotation.
	customSubnetName := annotations.FromService(l4netlb.Service).GetExternalLoadBalancerAnnotationSubnet()
	if customSubnetName != "" {
		return customSubnetName
	}

	// If no custom subnet in annotation -- use cluster subnet.
	clusterSubnetURL := l4netlb.cloud.SubnetworkURL()
	splitURL := strings.Split(clusterSubnetURL, "/")
	return splitURL[len(splitURL)-1]
}

func (l4netlb *L4NetLB) serviceSubnetHasExternalIPv6Range() error {
	subnetName := l4netlb.ipv6SubnetName()
	hasIPv6SubnetRange, err := utils.SubnetHasIPv6Range(l4netlb.cloud, subnetName, subnetExternalIPv6AccessType)
	if err != nil {
		return err
	}
	if !hasIPv6SubnetRange {
		// We don't need to emit custom event, because errors are already emitted to the user as events.
		l4netlb.svcLogger.Info("Subnet for IPv6 Service does not have external IPv6 ranges", "subnetName", subnetName)
		return utils.NewUserError(
			fmt.Errorf(
				"subnet %s does not have external IPv6 ranges, required for an external IPv6 Service. You can specify an external IPv6 subnet using the \"%s\" annotation on the Service", subnetName, annotations.CustomSubnetAnnotationKey))
	}
	return nil
}
