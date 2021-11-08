/*
Copyright 2020 The Kubernetes Authors.

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
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

func EnsureL4FirewallRule(cloud *gce.Cloud, fwName, lbIP, nsName string, sourceRanges, portRanges, nodeNames []string, proto string, sharedRule bool, l4Type utils.L4LBType) error {
	existingFw, err := cloud.GetFirewall(fwName)
	if err != nil && !utils.IsNotFoundError(err) {
		return err
	}

	nodeTags, err := cloud.GetNodeTags(nodeNames)
	if err != nil {
		return err
	}
	fwDesc, err := utils.MakeL4LBServiceDescription(nsName, lbIP, meta.VersionGA, sharedRule, l4Type)
	if err != nil {
		klog.Warningf("EnsureL4FirewallRule(%v): failed to generate description for L4 %s rule, err: %v", fwName, l4Type.ToString(), err)
	}
	expectedFw := &compute.Firewall{
		Name:         fwName,
		Description:  fwDesc,
		Network:      cloud.NetworkURL(),
		SourceRanges: sourceRanges,
		TargetTags:   nodeTags,
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: strings.ToLower(proto),
				Ports:      portRanges,
			},
		},
	}
	if existingFw == nil {
		klog.V(2).Infof("EnsureL4FirewallRule(%v): creating L4 %s firewall rule", fwName, l4Type.ToString())
		err = cloud.CreateFirewall(expectedFw)
		if utils.IsForbiddenError(err) && cloud.OnXPN() {
			gcloudCmd := gce.FirewallToGCloudCreateCmd(expectedFw, cloud.NetworkProjectID())

			klog.V(3).Infof("EnsureL4FirewallRule(%v): Could not create L4 %s firewall on XPN cluster: %v. Raising event for cmd: %q", fwName, l4Type.ToString(), err, gcloudCmd)
			return newFirewallXPNError(err, gcloudCmd)
		}
		return err
	}
	if firewallRuleEqual(expectedFw, existingFw) {
		return nil
	}
	klog.V(2).Infof("EnsureL4FirewallRule(%v): updating L4 %s firewall", fwName, l4Type.ToString())
	err = cloud.UpdateFirewall(expectedFw)
	if utils.IsForbiddenError(err) && cloud.OnXPN() {
		gcloudCmd := gce.FirewallToGCloudUpdateCmd(expectedFw, cloud.NetworkProjectID())
		klog.V(3).Infof("EnsureL4FirewallRule(%v): Could not update L4 %s firewall on XPN cluster: %v. Raising event for cmd: %q", fwName, l4Type.ToString(), err, gcloudCmd)
		return newFirewallXPNError(err, gcloudCmd)
	}
	return err
}

func EnsureL4FirewallRuleDeleted(cloud *gce.Cloud, fwName string) error {
	if err := utils.IgnoreHTTPNotFound(cloud.DeleteFirewall(fwName)); err != nil {
		if utils.IsForbiddenError(err) && cloud.OnXPN() {
			gcloudCmd := gce.FirewallToGCloudDeleteCmd(fwName, cloud.NetworkProjectID())
			klog.V(3).Infof("EnsureL4FirewallRuleDeleted(%v): could not delete traffic firewall on XPN cluster. Raising event.", fwName)
			return newFirewallXPNError(err, gcloudCmd)
		}
		return err
	}
	return nil
}

func firewallRuleEqual(a, b *compute.Firewall) bool {
	return a.Description == b.Description &&
		len(a.Allowed) == 1 && len(a.Allowed) == len(b.Allowed) &&
		a.Allowed[0].IPProtocol == b.Allowed[0].IPProtocol &&
		utils.EqualStringSets(a.Allowed[0].Ports, b.Allowed[0].Ports) &&
		utils.EqualStringSets(a.SourceRanges, b.SourceRanges) &&
		utils.EqualStringSets(a.TargetTags, b.TargetTags)
}
