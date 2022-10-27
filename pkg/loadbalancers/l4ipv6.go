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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

// ensureIPv6Resources creates resources specific to IPv6 L4 Load Balancers:
// - IPv6 Forwarding Rule
// - IPv6 Firewall
// it also adds IPv6 address to LB status
func (l4 *L4) ensureIPv6Resources(syncResult *L4ILBSyncResult, nodeNames []string, options gce.ILBOptions, bsLink string) {
	ipv6fr, err := l4.ensureIPv6ForwardingRule(bsLink, options)
	if err != nil {
		klog.Errorf("ensureIPv6Resources: Failed to create ipv6 forwarding rule - %v", err)
		syncResult.GCEResourceInError = annotations.ForwardingRuleIPv6Resource
		syncResult.Error = err
		return
	}

	if ipv6fr.IPProtocol == string(corev1.ProtocolTCP) {
		syncResult.Annotations[annotations.TCPForwardingRuleIPv6Key] = ipv6fr.Name
	} else {
		syncResult.Annotations[annotations.UDPForwardingRuleIPv6Key] = ipv6fr.Name
	}

	l4.ensureIPv6NodesFirewall(ipv6fr, nodeNames, syncResult)
	if syncResult.Error != nil {
		return
	}

	trimmedIPv6Address := strings.Split(ipv6fr.IPAddress, "/")[0]
	syncResult.Status = utils.AddIPToLBStatus(syncResult.Status, trimmedIPv6Address)
}

// deleteIPv6ResourcesOnSync deletes resources specific to IPv6,
// only if corresponding resource annotation exist on Service object.
// This function is called only on Service update or periodic sync.
// Checking for annotation saves us from emitting too much error logs "Resource not found".
// If annotation was deleted, but resource still exists, it will be left till the Service deletion,
// where we delete all resources, no matter if they exist in annotations.
func (l4 *L4) deleteIPv6ResourcesOnSync(syncResult *L4ILBSyncResult) {
	l4.deleteIPv6ResourcesAnnotationBased(syncResult, true)
}

// deleteIPv6ResourcesOnDelete deletes all resources specific to IPv6.
// This function is called only on Service deletion.
// During sync, we delete resources only that exist in annotations,
// so they could be leaked, if annotation was deleted.
// That's why on service deletion we delete all IPv4 resources, ignoring their existence in annotations
func (l4 *L4) deleteIPv6ResourcesOnDelete(syncResult *L4ILBSyncResult) {
	l4.deleteIPv6ResourcesAnnotationBased(syncResult, false)
}

// deleteIPv6ResourcesAnnotationBased deletes IPv6 only resources with checking,
// if resource exists in Service annotation, if shouldIgnoreAnnotations not set to true
// IPv6 Specific resources:
// - IPv6 Forwarding Rule
// - IPv6 Address
// - IPv6 Firewall
// This function does not delete Backend Service and Health Check, because they are shared between IPv4 and IPv6.
// IPv6 Firewall Rule for Health Check also will not be deleted here, and will be left till the Service Deletion.
func (l4 *L4) deleteIPv6ResourcesAnnotationBased(syncResult *L4ILBSyncResult, shouldCheckAnnotations bool) {
	if !shouldCheckAnnotations || l4.hasAnnotation(annotations.TCPForwardingRuleIPv6Key) || l4.hasAnnotation(annotations.UDPForwardingRuleIPv6Key) {
		err := l4.deleteIPv6ForwardingRule()
		if err != nil {
			klog.Errorf("Failed to delete ipv6 forwarding rule for internal loadbalancer service %s, err %v", l4.NamespacedName.String(), err)
			syncResult.Error = err
			syncResult.GCEResourceInError = annotations.ForwardingRuleIPv6Resource
		}
	}

	if !shouldCheckAnnotations || l4.hasAnnotation(annotations.FirewallRuleIPv6Key) {
		err := l4.deleteIPv6Firewall()
		if err != nil {
			klog.Errorf("Failed to delete ipv6 firewall rule for internal loadbalancer service %s, err %v", l4.NamespacedName.String(), err)
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

func (l4 *L4) ensureIPv6NodesFirewall(forwardingRule *composite.ForwardingRule, nodeNames []string, result *L4ILBSyncResult) {
	firewallName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)

	svcPorts := l4.Service.Spec.Ports
	portRanges := utils.GetServicePortRanges(svcPorts)
	protocol := utils.GetProtocol(svcPorts)

	ipv6nodesFWRParams := firewalls.FirewallParams{
		PortRanges: portRanges,
		// TODO(panslava): support .spec.loadBalancerSourceRanges
		SourceRanges:      []string{"0::0/0"},
		DestinationRanges: []string{forwardingRule.IPAddress},
		Protocol:          string(protocol),
		Name:              firewallName,
		NodeNames:         nodeNames,
		L4Type:            utils.ILB,
	}

	err := firewalls.EnsureL4LBFirewallForNodes(l4.Service, &ipv6nodesFWRParams, l4.cloud, l4.recorder)
	if err != nil {
		result.GCEResourceInError = annotations.FirewallRuleIPv6Resource
		result.Error = err
		return
	}
	result.Annotations[annotations.FirewallRuleIPv6Key] = firewallName
}

func (l4 *L4) deleteIPv6ForwardingRule() error {
	ipv6FrName := l4.getIPv6FRName()
	return l4.forwardingRules.Delete(ipv6FrName)
}

func (l4 *L4) deleteIPv6Firewall() error {
	ipv6FirewallName := l4.namer.L4IPv6Firewall(l4.Service.Namespace, l4.Service.Name)
	return l4.deleteFirewall(ipv6FirewallName)
}
