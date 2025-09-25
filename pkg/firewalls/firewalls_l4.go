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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// FirewallParams holds all data needed to create firewall for L4 LB
type FirewallParams struct {
	Name              string
	IP                string
	SourceRanges      []string
	DestinationRanges []string
	NodeNames         []string
	Allowed           []*compute.FirewallAllowed
	L4Type            utils.L4LBType
	Network           network.NetworkInfo
}

func EnsureL4FirewallRule(cloud *gce.Cloud, nsName string, params *FirewallParams, sharedRule bool, fwLogger klog.Logger) (*compute.Firewall, utils.ResourceSyncStatus, error) {
	fwLogger = fwLogger.WithValues("l4Type", params.L4Type.ToString())
	fa := NewFirewallAdapter(cloud)
	existingFw, err := fa.GetFirewall(params.Name)
	if err != nil && !utils.IsNotFoundError(err) {
		return nil, utils.ResourceResync, err
	}

	nodeTags, err := cloud.GetNodeTags(params.NodeNames)
	if err != nil {
		return nil, utils.ResourceResync, err
	}
	fwDesc, err := utils.MakeL4LBFirewallDescription(nsName, params.IP, meta.VersionGA, sharedRule)
	if err != nil {
		fwLogger.Info("EnsureL4FirewallRule: failed to generate description for L4 rule", "err", err)
	}

	expectedFw := &compute.Firewall{
		Name:         params.Name,
		Description:  fwDesc,
		Network:      params.Network.NetworkURL,
		SourceRanges: params.SourceRanges,
		TargetTags:   nodeTags,
		Allowed:      params.Allowed,
	}
	if flags.F.EnablePinhole {
		expectedFw.DestinationRanges = params.DestinationRanges
	}
	if existingFw == nil {
		fwLogger.V(2).Info("EnsureL4FirewallRule: creating L4 firewall rule")
		err = fa.CreateFirewall(expectedFw)
		if utils.IsForbiddenError(err) && cloud.OnXPN() {
			gcloudCmd := gce.FirewallToGCloudCreateCmd(expectedFw, cloud.NetworkProjectID())

			fwLogger.V(3).Info("EnsureL4FirewallRule: Could not create L4 firewall on XPN cluster. Raising event for cmd", "err", err, "gcloudCmd", gcloudCmd)
			return nil, utils.ResourceUpdate, newFirewallXPNError(err, gcloudCmd)
		}
		return expectedFw, utils.ResourceUpdate, err
	}

	// Don't compare the "description" field for shared firewall rules
	if eq, err := Equal(expectedFw, existingFw, sharedRule); eq || err != nil {
		return existingFw, utils.ResourceResync, err
	}

	fwLogger.V(2).Info("EnsureL4FirewallRule: patching L4 firewall")
	err = fa.PatchFirewall(expectedFw)
	if utils.IsForbiddenError(err) && cloud.OnXPN() {
		gcloudCmd := gce.FirewallToGCloudUpdateCmd(expectedFw, cloud.NetworkProjectID())
		fwLogger.V(3).Info("EnsureL4FirewallRule: Could not patch L4 firewall on XPN cluster. Raising event for cmd", "err", err, "gcloudCmd", gcloudCmd)
		return nil, utils.ResourceUpdate, newFirewallXPNError(err, gcloudCmd)
	}
	return expectedFw, utils.ResourceUpdate, err
}

func EnsureL4FirewallRuleDeleted(cloud *gce.Cloud, fwName string, fwLogger klog.Logger) error {
	fa := NewFirewallAdapter(cloud)
	if err := utils.IgnoreHTTPNotFound(fa.DeleteFirewall(fwName)); err != nil {
		if utils.IsForbiddenError(err) && cloud.OnXPN() {
			gcloudCmd := gce.FirewallToGCloudDeleteCmd(fwName, cloud.NetworkProjectID())
			fwLogger.V(3).Info("EnsureL4FirewallRuleDeleted: could not delete traffic firewall on XPN cluster. Raising event.", "err", err, "gcloudCmd", gcloudCmd)
			return newFirewallXPNError(err, gcloudCmd)
		}
		return err
	}
	return nil
}

func ensureFirewall(svc *v1.Service, shared bool, params *FirewallParams, cloud *gce.Cloud, recorder record.EventRecorder, fwLogger klog.Logger) (*compute.Firewall, utils.ResourceSyncStatus, error) {
	nsName := utils.ServiceKeyFunc(svc.Namespace, svc.Name)
	fwRule, updateStatus, err := EnsureL4FirewallRule(cloud, nsName, params, shared, fwLogger)
	if err != nil {
		if fwErr, ok := err.(*FirewallXPNError); ok {
			recorder.Eventf(svc, v1.EventTypeNormal, "XPN", fwErr.Message)
			return nil, updateStatus, nil
		}
		return nil, updateStatus, err
	}
	return fwRule, updateStatus, nil
}

// EnsureL4LBFirewallForHc creates or updates firewall rule for shared or non-shared health check to nodes
func EnsureL4LBFirewallForHc(svc *v1.Service, shared bool, params *FirewallParams, cloud *gce.Cloud, recorder record.EventRecorder, fwLogger klog.Logger) (*compute.Firewall, utils.ResourceSyncStatus, error) {
	return ensureFirewall(svc, shared, params, cloud, recorder, fwLogger)
}

// EnsureL4LBFirewallForNodes creates or updates firewall rule for LB traffic to nodes
func EnsureL4LBFirewallForNodes(svc *v1.Service, params *FirewallParams, cloud *gce.Cloud, recorder record.EventRecorder, fwLogger klog.Logger) (*compute.Firewall, utils.ResourceSyncStatus, error) {
	return ensureFirewall(svc /*shared = */, false, params, cloud, recorder, fwLogger)
}
