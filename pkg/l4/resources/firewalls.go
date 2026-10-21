/*
Copyright 2026 The Kubernetes Authors.

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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/firewalls"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// cleanUpDenyFirewallRule will attempt the deletion only after checking that it exists with GET call.
// This is done to avoid polluting Audit Logs with 404 errors (even though the 404 might be expected).
func cleanUpDenyFirewallRule(cloud *gce.Cloud, fwName string, logger klog.Logger) error {
	log := logger.WithName("cleanUpDenyFirewallRule").WithValues("firewallName", fwName)
	_, err := cleanUpFirewallRule(cloud, fwName, log)
	return err
}

// cleanUpFirewallRule will attempt the deletion only after checking that it exists with GET call.
// This is done to avoid polluting Audit Logs with 404 errors (even though the 404 might be expected).
// It reports whether a rule was actually deleted, so that callers can record the
// update in the sync status.
//
// The logger is expected to already identify the firewall rule being cleaned up.
func cleanUpFirewallRule(cloud *gce.Cloud, fwName string, logger klog.Logger) (l4utils.ResourceSyncStatus, error) {
	// Skip deleting if it doesn't exist to not produce noisy audit logs
	fa := firewalls.NewFirewallAdapter(cloud)
	if _, err := fa.GetFirewall(fwName); err != nil {
		if utils.IsNotFoundError(err) {
			logger.V(3).Info("no firewall found to delete")
			return l4utils.ResourceResync, nil // no need for cleanup
		}
		return l4utils.ResourceResync, err
	}

	logger.V(3).Info("firewall exists, deleting it")
	if err := firewalls.EnsureL4FirewallRuleDeleted(cloud, fwName, logger); err != nil {
		return l4utils.ResourceResync, err
	}
	return l4utils.ResourceUpdate, nil
}

// noSourceRangesForIPFamilyReason is the event reason emitted when a Service
// specifies loadBalancerSourceRanges with no CIDRs matching one IP family.
const noSourceRangesForIPFamilyReason = "NoSourceRangesForIPFamily"

// ensureNoAllowFirewall ensures that no nodes allow firewall rule exists for
// ipFamily when a Service sets loadBalancerSourceRanges without any CIDRs for
// that family, deleting any existing rule from a prior sync and recording a
// warning event.
func ensureNoAllowFirewall(cloud *gce.Cloud, svc *corev1.Service, recorder record.EventRecorder, fwName string, ipFamily corev1.IPFamily, logger klog.Logger) (l4utils.ResourceSyncStatus, error) {
	logger.V(2).Info("No loadBalancerSourceRanges configured for IP family, ensuring nodes allow firewall does not exist", "ipFamily", ipFamily)

	syncStatus, err := cleanUpFirewallRule(cloud, fwName, logger)
	if err != nil {
		if fwErr, ok := err.(*firewalls.FirewallXPNError); ok {
			logger.V(3).Info("Could not delete nodes allow firewall on XPN cluster, raising event", "ipFamily", ipFamily, "err", err)
			recorder.Event(svc, corev1.EventTypeNormal, "XPN", fwErr.Message)
			return syncStatus, nil
		}
		logger.Error(err, "Failed to clean up nodes allow firewall", "ipFamily", ipFamily)
		return syncStatus, err
	}

	recorder.Eventf(svc, corev1.EventTypeWarning, noSourceRangesForIPFamilyReason,
		"loadBalancerSourceRanges is set but contains no %[1]s CIDR, so no %[1]s allow firewall rule is configured and the load balancer is not reachable over %[1]s. Add an %[1]s CIDR to loadBalancerSourceRanges to allow %[1]s traffic.",
		ipFamily)
	return syncStatus, nil
}
