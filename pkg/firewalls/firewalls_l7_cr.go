/*
Copyright 2015 The Kubernetes Authors.

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

package firewalls

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	api_errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	gcpfirewallv1 "k8s.io/cloud-provider-gcp/crd/apis/gcpfirewall/v1"
	firewallclient "k8s.io/cloud-provider-gcp/crd/client/gcpfirewall/clientset/versioned"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
	netset "k8s.io/utils/net"
)

// FirewallRules manages firewall rules.
type FirewallCR struct {
	cloud     Firewall
	namer     *namer_util.Namer
	srcRanges []string
	// TODO(rramkumar): Eliminate this variable. We should just pass in
	// all the port ranges to open with each call to Sync()
	nodePortRanges []string
	firewallClient firewallclient.Interface
	dryRun         bool

	logger klog.Logger
}

// NewFirewallPool creates a new firewall rule manager.
// cloud: the cloud object implementing Firewall.
// namer: cluster namer.
func NewFirewallCRPool(client firewallclient.Interface, cloud Firewall, namer *namer_util.Namer, l7SrcRanges []string, nodePortRanges []string, dryRun bool, logger klog.Logger) SingleFirewallPool {
	_, err := netset.ParseIPNets(l7SrcRanges...)
	if err != nil {
		klog.Fatalf("Could not parse L7 src ranges %v for firewall rule: %v", l7SrcRanges, err)
	}
	return &FirewallCR{
		cloud:          cloud,
		namer:          namer,
		srcRanges:      l7SrcRanges,
		nodePortRanges: nodePortRanges,
		firewallClient: client,
		dryRun:         dryRun,
		logger:         logger.WithName("FirewallCR"),
	}
}

// Sync firewall rules with the cloud.
func (fr *FirewallCR) Sync(nodeNames, additionalPorts, additionalRanges []string, allowNodePort bool) error {
	fr.logger.V(4).Info("Sync", "nodeNames", nodeNames)
	name := fr.namer.FirewallRule()

	// De-dupe ports
	ports := sets.NewString()
	if allowNodePort {
		ports.Insert(fr.nodePortRanges...)
	}
	ports.Insert(additionalPorts...)

	// De-dupe srcRanges
	ranges := sets.NewString(fr.srcRanges...)
	ranges.Insert(additionalRanges...)

	fr.logger.V(3).Info("Firewall CR is enabled.")
	expectedFirewallCR, err := NewFirewallCR(name, ports.List(), ranges.UnsortedList(), []string{}, fr.dryRun)
	if err != nil {
		return err
	}
	return ensureFirewallCR(fr.firewallClient, expectedFirewallCR, fr.logger)
}

// ensureFirewallCR creates/updates the firewall CR
// On CR update, it will read the conditions to see if there are errors updated by PFW controller.
// If the Spec was updated by others, it will reconcile the Spec.
func ensureFirewallCR(client firewallclient.Interface, expectedFWCR *gcpfirewallv1.GCPFirewall, logger klog.Logger) error {
	fw := client.NetworkingV1().GCPFirewalls()
	currentFWCR, err := fw.Get(context.Background(), expectedFWCR.Name, metav1.GetOptions{})
	logger.V(3).Info("ensureFirewallCR Get CR", "currentFirewallCR", fmt.Sprintf("%+v", currentFWCR), "err", err)
	if err != nil {
		// Create the CR if it is not found.
		if api_errors.IsNotFound(err) {
			logger.V(3).Info("The CR is not found. ensureFirewallCR Create CR", "expectedFirewallCR", fmt.Sprintf("%+v", expectedFWCR))
			_, err = fw.Create(context.Background(), expectedFWCR, metav1.CreateOptions{})
		}
		return err
	}
	if !reflect.DeepEqual(currentFWCR.Spec, expectedFWCR.Spec) {
		// Update the current firewall CR
		logger.V(3).Info("ensureFirewallCR Update CR", "currentFirewallCR", fmt.Sprintf("%+v", currentFWCR.Spec), "expectedFirewallCR", fmt.Sprintf("%+v", expectedFWCR.Spec))
		currentFWCR.Spec = expectedFWCR.Spec
		_, err = fw.Update(context.Background(), currentFWCR, metav1.UpdateOptions{})
		return err
	}
	for _, con := range currentFWCR.Status.Conditions {
		if con.Reason == string(gcpfirewallv1.FirewallRuleReasonXPNPermissionError) ||
			con.Reason == string(gcpfirewallv1.FirewallRuleReasonInvalid) ||
			con.Reason == string(gcpfirewallv1.FirewallRuleReasonSyncError) {
			// Use recorder to emit the cmd in Sync()
			logger.V(3).Info("ensureFirewallCR: Could not enforce Firewall CR", "currentFirewallCRName", currentFWCR.Name, "reason", con.Reason)
			return fmt.Errorf(con.Reason)
		}
	}
	return nil
}

// deleteFirewallCR deletes the firewall CR
func deleteFirewallCR(client firewallclient.Interface, name string, logger klog.Logger) error {
	fw := client.NetworkingV1().GCPFirewalls()
	logger.V(3).Info("Delete CR", "firewallCRName", name)
	return fw.Delete(context.Background(), name, metav1.DeleteOptions{})
}

// NewFirewallCR constructs the firewall CR from name, ports and ranges
func NewFirewallCR(name string, ports, srcRanges, dstRanges []string, enforced bool) (*gcpfirewallv1.GCPFirewall, error) {
	firewallCR := &gcpfirewallv1.GCPFirewall{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: gcpfirewallv1.GCPFirewallSpec{
			Action:   gcpfirewallv1.ActionAllow,
			Disabled: !enforced,
		},
	}
	var protocolPorts []gcpfirewallv1.ProtocolPort

	for _, item := range ports {
		s := strings.Split(item, "-")
		var protocolPort gcpfirewallv1.ProtocolPort
		startport, err := strconv.Atoi(s[0])
		if err != nil {
			return nil, fmt.Errorf("failed to convert startport to an integer: %w", err)
		}
		sp32 := int32(startport)
		if len(s) > 1 {
			endport, err := strconv.Atoi(s[1])
			if err != nil {
				return nil, fmt.Errorf("failed to convert endport to an integer: %w", err)
			}
			ep32 := int32(endport)
			protocolPort = gcpfirewallv1.ProtocolPort{
				Protocol:  gcpfirewallv1.ProtocolTCP,
				StartPort: &sp32,
				EndPort:   &ep32,
			}
		} else {
			protocolPort = gcpfirewallv1.ProtocolPort{
				Protocol:  gcpfirewallv1.ProtocolTCP,
				StartPort: &sp32,
			}
		}
		protocolPorts = append(protocolPorts, protocolPort)
	}
	firewallCR.Spec.Ports = protocolPorts

	var src_cidrs, dst_cidrs []gcpfirewallv1.CIDR

	for _, item := range srcRanges {
		src_cidrs = append(src_cidrs, gcpfirewallv1.CIDR(item))
	}

	for _, item := range dstRanges {
		dst_cidrs = append(dst_cidrs, gcpfirewallv1.CIDR(item))
	}
	firewallCR.Spec.Ingress = &gcpfirewallv1.GCPFirewallIngress{
		Source: &gcpfirewallv1.IngressSource{
			IPBlocks: src_cidrs,
		},
		Destination: &gcpfirewallv1.IngressDestination{
			IPBlocks: dst_cidrs,
		},
	}

	return firewallCR, nil
}

// GCFirewallCR deletes the firewall CR
// For the upgraded clusters with EnableFirewallCR = true, the firewall CR and the firewall co-exist.
// We need to delete both of them every time.
func (fr *FirewallCR) GC() error {
	name := fr.namer.FirewallRule()
	fr.logger.V(3).Info("Deleting firewall CR", "firewallCRName", name)
	return deleteFirewallCR(fr.firewallClient, name, fr.logger)
}
