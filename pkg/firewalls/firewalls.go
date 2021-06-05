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
	"fmt"
	"sort"
	"strings"

	"google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/utils"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
	netset "k8s.io/utils/net"
)

const (
	// DefaultFirewallName is the name to use for firewall rules created
	// by an L7 controller when --firewall-rule is not used.
	DefaultFirewallName = ""
)

// FirewallRules manages firewall rules.
type FirewallRules struct {
	cloud     Firewall
	namer     *namer_util.Namer
	srcRanges []string
	// TODO(rramkumar): Eliminate this variable. We should just pass in
	// all the port ranges to open with each call to Sync()
	nodePortRanges []string
}

// NewFirewallPool creates a new firewall rule manager.
// cloud: the cloud object implementing Firewall.
// namer: cluster namer.
func NewFirewallPool(cloud Firewall, namer *namer_util.Namer, l7SrcRanges []string, nodePortRanges []string) SingleFirewallPool {
	_, err := netset.ParseIPNets(l7SrcRanges...)
	if err != nil {
		klog.Fatalf("Could not parse L7 src ranges %v for firewall rule: %v", l7SrcRanges, err)
	}
	return &FirewallRules{
		cloud:          cloud,
		namer:          namer,
		srcRanges:      l7SrcRanges,
		nodePortRanges: nodePortRanges,
	}
}

// Sync firewall rules with the cloud.
func (fr *FirewallRules) Sync(nodeNames, additionalPorts, additionalRanges []string, allowNodePort bool) error {
	klog.V(4).Infof("Sync firewall rules: nodes (%v), additionalPorts (%v), additionalRanges (%v), allowNodePort (%v)",
		nodeNames, additionalPorts, additionalRanges, allowNodePort)
	name := fr.namer.FirewallRule()
	existingFirewall, _ := fr.cloud.GetFirewall(name)

	// Retrieve list of target tags from node names. This may be configured in
	// gce.conf or computed by the GCE cloudprovider package.
	targetTags, err := fr.cloud.GetNodeTags(nodeNames)
	if err != nil {
		return err
	}
	sort.Strings(targetTags)

	// De-dupe ports
	ports := sets.NewString()
	if allowNodePort {
		ports.Insert(fr.nodePortRanges...)
	}
	ports.Insert(additionalPorts...)

	// De-dupe srcRanges
	ranges := sets.NewString(fr.srcRanges...)
	ranges.Insert(additionalRanges...)

	// A firewall rule with TCP and an empty port list is equivalent with
	// Allow TCP on any port. Dangerous!!!
	// Delete the firewall rule if exists when the ports list is empty
	if ports.Len() == 0 {
		if existingFirewall == nil {
			klog.V(4).Info("Firewall does not need update of ports or source ranges")
			return nil
		}
		return fr.GC()
	}

	expectedFirewall := &compute.Firewall{
		Name:         name,
		Description:  "GCE L7 firewall rule",
		SourceRanges: ranges.UnsortedList(),
		Network:      fr.cloud.NetworkURL(),
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "tcp",
				Ports:      ports.List(),
			},
		},
		TargetTags: targetTags,
	}

	if existingFirewall == nil {
		klog.V(3).Infof("Creating firewall rule %q", name)
		return fr.createFirewall(expectedFirewall)
	}

	// Early return if an update is not required.
	if equal(expectedFirewall, existingFirewall) {
		klog.V(4).Info("Firewall does not need update of ports or source ranges")
		return nil
	}

	klog.V(3).Infof("Updating firewall rule %q", name)
	return fr.updateFirewall(expectedFirewall)
}

// GC deletes the firewall rule.
func (fr *FirewallRules) GC() error {
	name := fr.namer.FirewallRule()
	klog.V(3).Infof("Deleting firewall %q", name)
	return fr.deleteFirewall(name)
}

// GetFirewall just returns the firewall object corresponding to the given name.
// TODO: Currently only used in testing. Modify so we don't leak compute
// objects out of this interface by returning just the (src, ports, error).
func (fr *FirewallRules) GetFirewall(name string) (*compute.Firewall, error) {
	return fr.cloud.GetFirewall(name)
}

func (fr *FirewallRules) createFirewall(f *compute.Firewall) error {
	err := fr.cloud.CreateFirewall(f)
	if utils.IsForbiddenError(err) && fr.cloud.OnXPN() {
		gcloudCmd := gce.FirewallToGCloudCreateCmd(f, fr.cloud.NetworkProjectID())
		klog.V(3).Infof("Could not create L7 firewall on XPN cluster: %v. Raising event for cmd: %q", err, gcloudCmd)
		return newFirewallXPNError(err, gcloudCmd)
	}
	return err
}

func (fr *FirewallRules) updateFirewall(f *compute.Firewall) error {
	err := fr.cloud.UpdateFirewall(f)
	if utils.IsForbiddenError(err) && fr.cloud.OnXPN() {
		gcloudCmd := gce.FirewallToGCloudUpdateCmd(f, fr.cloud.NetworkProjectID())
		klog.V(3).Infof("Could not update L7 firewall on XPN cluster: %v. Raising event for cmd: %q", err, gcloudCmd)
		return newFirewallXPNError(err, gcloudCmd)
	}
	return err
}

func (fr *FirewallRules) deleteFirewall(name string) error {
	err := fr.cloud.DeleteFirewall(name)
	if utils.IsNotFoundError(err) {
		klog.Infof("Firewall with name %v didn't exist when attempting delete.", name)
		return nil
	} else if utils.IsForbiddenError(err) && fr.cloud.OnXPN() {
		gcloudCmd := gce.FirewallToGCloudDeleteCmd(name, fr.cloud.NetworkProjectID())
		klog.V(3).Infof("Could not attempt delete of L7 firewall on XPN cluster: %v. %q needs to be ran.", err, gcloudCmd)
		return newFirewallXPNError(err, gcloudCmd)
	}
	return err
}

func newFirewallXPNError(internal error, cmd string) *FirewallXPNError {
	return &FirewallXPNError{
		Internal: internal,
		Message:  fmt.Sprintf("Firewall change required by security admin: `%v`", cmd),
	}
}

type FirewallXPNError struct {
	Internal error
	Message  string
}

func (f *FirewallXPNError) Error() string {
	return f.Message
}

func equal(expected *compute.Firewall, existing *compute.Firewall) bool {
	if !sets.NewString(expected.TargetTags...).Equal(sets.NewString(existing.TargetTags...)) {
		klog.V(5).Infof("Expected target tags %v, actually %v", expected.TargetTags, existing.TargetTags)
		return false
	}

	expectedAllowed := allowedToStrings(expected.Allowed)
	existingAllowed := allowedToStrings(existing.Allowed)
	if !sets.NewString(expectedAllowed...).Equal(sets.NewString(existingAllowed...)) {
		klog.V(5).Infof("Expected allowed rules %v, actually %v", expectedAllowed, existingAllowed)
		return false
	}

	if !sets.NewString(expected.SourceRanges...).Equal(sets.NewString(existing.SourceRanges...)) {
		klog.V(5).Infof("Expected source ranges %v, actually %v", expected.SourceRanges, existing.SourceRanges)
		return false
	}

	// Ignore other firewall properties as the controller does not set them.
	return true
}

func allowedToStrings(allowed []*compute.FirewallAllowed) []string {
	var allowedStrs []string
	for _, v := range allowed {
		sort.Strings(v.Ports)
		s := strings.ToUpper(v.IPProtocol) + ":" + strings.Join(v.Ports, ",")
		allowedStrs = append(allowedStrs, s)
	}
	return allowedStrs
}
