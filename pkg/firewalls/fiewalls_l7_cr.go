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
	"k8s.io/cloud-provider-gcp/crd/apis/gcpfirewall/v1beta1"
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
}

// NewFirewallPool creates a new firewall rule manager.
// cloud: the cloud object implementing Firewall.
// namer: cluster namer.
func NewFirewallCRPool(client firewallclient.Interface, cloud Firewall, namer *namer_util.Namer, l7SrcRanges []string, nodePortRanges []string, dryRun bool) SingleFirewallPool {
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
	}
}

// Sync firewall rules with the cloud.
func (fr *FirewallCR) Sync(nodeNames, additionalPorts, additionalRanges []string, allowNodePort bool) error {
	klog.V(4).Infof("Sync(%v)", nodeNames)
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

	klog.V(3).Infof("Firewall CR is enabled.")
	expectedFirewallCR, err := NewFirewallCR(name, ports.List(), ranges.UnsortedList(), []string{}, fr.dryRun)
	if err != nil {
		return err
	}
	return ensureFirewallCR(fr.firewallClient, expectedFirewallCR)
}

// ensureFirewallCR creates/updates the firewall CR
// On CR update, it will read the conditions to see if there are errors updated by PFW controller.
// If the Spec was updated by others, it will reconcile the Spec.
func ensureFirewallCR(client firewallclient.Interface, expectedFWCR *v1beta1.GCPFirewall) error {
	fw := client.NetworkingV1beta1().GCPFirewalls()
	currentFWCR, err := fw.Get(context.Background(), expectedFWCR.Name, metav1.GetOptions{})
	klog.V(3).Infof("ensureFirewallCR Get CR :%+v, err :%v", currentFWCR, err)
	if err != nil {
		// Create the CR if it is not found.
		if api_errors.IsNotFound(err) {
			klog.V(3).Infof("The CR is not found. ensureFirewallCR Create CR :%+v", expectedFWCR)
			_, err = fw.Create(context.Background(), expectedFWCR, metav1.CreateOptions{})
		}
		return err
	}
	if !reflect.DeepEqual(currentFWCR.Spec, expectedFWCR.Spec) {
		// Update the current firewall CR
		klog.V(3).Infof("ensureFirewallCR Update CR currentFW.Spec: %+v, expectedFW.Spec: %+v", currentFWCR.Spec, expectedFWCR.Spec)
		currentFWCR.Spec = expectedFWCR.Spec
		_, err = fw.Update(context.Background(), currentFWCR, metav1.UpdateOptions{})
		return err
	}
	for _, con := range currentFWCR.Status.Conditions {
		if con.Reason == string(v1beta1.FirewallRuleReasonXPNPermissionError) ||
			con.Reason == string(v1beta1.FirewallRuleReasonInvalid) ||
			con.Reason == string(v1beta1.FirewallRuleReasonSyncError) {
			// Use recorder to emit the cmd in Sync()
			klog.V(3).Infof("ensureFirewallCR(%v): Could not enforce Firewall CR Reason:%v", currentFWCR.Name, con.Reason)
			return fmt.Errorf(con.Reason)
		}
	}
	return nil
}

// deleteFirewallCR deletes the firewall CR
func deleteFirewallCR(client firewallclient.Interface, name string) error {
	fw := client.NetworkingV1beta1().GCPFirewalls()
	klog.V(3).Infof("Delete CR :%v", name)
	return fw.Delete(context.Background(), name, metav1.DeleteOptions{})
}

// NewFirewallCR constructs the firewall CR from name, ports and ranges
func NewFirewallCR(name string, ports, srcRanges, dstRanges []string, enforced bool) (*v1beta1.GCPFirewall, error) {
	firewallCR := &v1beta1.GCPFirewall{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1beta1.GCPFirewallSpec{
			Action:   v1beta1.ActionAllow,
			Disabled: !enforced,
		},
	}
	var protocolPorts []v1beta1.ProtocolPort

	for _, item := range ports {
		s := strings.Split(item, "-")
		var protocolPort v1beta1.ProtocolPort
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
			protocolPort = v1beta1.ProtocolPort{
				Protocol:  v1beta1.ProtocolTCP,
				StartPort: &sp32,
				EndPort:   &ep32,
			}
		} else {
			protocolPort = v1beta1.ProtocolPort{
				Protocol:  v1beta1.ProtocolTCP,
				StartPort: &sp32,
			}
		}
		protocolPorts = append(protocolPorts, protocolPort)
	}
	firewallCR.Spec.Ports = protocolPorts

	var src_cidrs, dst_cidrs []v1beta1.CIDR

	for _, item := range srcRanges {
		src_cidrs = append(src_cidrs, v1beta1.CIDR(item))
	}

	for _, item := range dstRanges {
		dst_cidrs = append(dst_cidrs, v1beta1.CIDR(item))
	}
	firewallCR.Spec.Ingress = &v1beta1.GCPFirewallIngress{
		Source: &v1beta1.IngressSource{
			IPBlocks: src_cidrs,
		},
		Destination: &v1beta1.IngressDestination{
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
	klog.V(3).Infof("Deleting firewall CR %q", name)
	return deleteFirewallCR(fr.firewallClient, name)
}
