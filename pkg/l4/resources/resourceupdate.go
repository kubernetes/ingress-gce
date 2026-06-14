package resources

import (
	"strings"

	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

// ResourceUpdates tracks the updates to the GCE resources that were done during ensuring the LB.
// Ensuring of a resource follows this pattern:
// - get the existing resource
// - compare it with the expected state
// - if the resource is already in the expected state - do nothing
// - if the resource differs perform an update
// This struct will track if nothing was done (resync) or if an update was performed.
// It usually should be added to the SyncResult struct of L4 controllers and updated
// with sync results of GCE resources ensure operations.
// It is part of the effort to add more transparency to what the controller
// is doing and also to detect situations where resources are unexpectedly updated.
type ResourceUpdates struct {
	backendServiceUpdate   l4utils.ResourceSyncStatus
	forwardingRuleUpdate   l4utils.ResourceSyncStatus
	healthCheckUpdate      l4utils.ResourceSyncStatus
	firewallForNodesUpdate l4utils.ResourceSyncStatus
	firewallForHCUpdate    l4utils.ResourceSyncStatus
}

// WereAnyResourcesModified returns true if any of the LB resources were updated.
func (ru *ResourceUpdates) WereAnyResourcesModified() bool {
	return ru.forwardingRuleUpdate == l4utils.ResourceUpdate ||
		ru.backendServiceUpdate == l4utils.ResourceUpdate ||
		ru.healthCheckUpdate == l4utils.ResourceUpdate ||
		ru.firewallForNodesUpdate == l4utils.ResourceUpdate ||
		ru.firewallForHCUpdate == l4utils.ResourceUpdate
}

func (ru *ResourceUpdates) String() string {
	if ru.WereAnyResourcesModified() {
		var modifiedResources []string
		if ru.forwardingRuleUpdate == l4utils.ResourceUpdate {
			modifiedResources = append(modifiedResources, "forwarding rule")
		}
		if ru.backendServiceUpdate == l4utils.ResourceUpdate {
			modifiedResources = append(modifiedResources, "backend service")
		}
		if ru.healthCheckUpdate == l4utils.ResourceUpdate {
			modifiedResources = append(modifiedResources, "health check")
		}
		if ru.firewallForNodesUpdate == l4utils.ResourceUpdate {
			modifiedResources = append(modifiedResources, "nodes firewall")
		}
		if ru.firewallForHCUpdate == l4utils.ResourceUpdate {
			modifiedResources = append(modifiedResources, "health check firewall")
		}
		return strings.Join(modifiedResources, ",")
	}
	return "-"
}

func (ru *ResourceUpdates) set(field *l4utils.ResourceSyncStatus, new l4utils.ResourceSyncStatus) {
	if *field == l4utils.ResourceUpdate {
		return
	}
	*field = new
}

// SetBackendService sets the status of the Backend Service update.
// When this function is invoked multiple times with at least one UPDATE status then the result will be UPDATE.
func (ru *ResourceUpdates) SetBackendService(status l4utils.ResourceSyncStatus) {
	ru.set(&ru.backendServiceUpdate, status)
}

// SetForwardingRule sets the status of the Forwarding Rule update.
// When this function is invoked multiple times with at least one UPDATE status then the result will be UPDATE.
func (ru *ResourceUpdates) SetForwardingRule(status l4utils.ResourceSyncStatus) {
	ru.set(&ru.forwardingRuleUpdate, status)
}

// SetHealthCheck sets the status of the Health Check update.
// When this function is invoked multiple times with at least one UPDATE status then the result will be UPDATE.
func (ru *ResourceUpdates) SetHealthCheck(status l4utils.ResourceSyncStatus) {
	ru.set(&ru.healthCheckUpdate, status)
}

// SetFirewallForNodes sets the status of the Firewall for nodes update.
// When this function is invoked multiple times with at least one UPDATE status then the result will be UPDATE.
func (ru *ResourceUpdates) SetFirewallForNodes(status l4utils.ResourceSyncStatus) {
	ru.set(&ru.firewallForNodesUpdate, status)
}

// SetFirewallForHealthCheck sets the status of the Firewall for Health Check update.
// When this function is invoked multiple times with at least one UPDATE status then the result will be UPDATE.
func (ru *ResourceUpdates) SetFirewallForHealthCheck(status l4utils.ResourceSyncStatus) {
	ru.set(&ru.firewallForHCUpdate, status)
}
