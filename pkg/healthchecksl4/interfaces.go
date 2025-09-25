package healthchecksl4

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

// L4HealthChecks defines methods for creating and deleting health checks (and their firewall rules) for l4 services
type L4HealthChecks interface {
	// EnsureHealthCheckWithFirewall creates health check (and firewall rule) for l4 service.
	EnsureHealthCheckWithFirewall(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string, svcNetwork network.NetworkInfo, svcLogger klog.Logger) *EnsureHealthCheckResult
	// EnsureHealthCheckWithDualStackFirewalls creates health check (and firewall rule) for l4 service. Handles both IPv4 and IPv6.
	EnsureHealthCheckWithDualStackFirewalls(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string, needsIPv4 bool, needsIPv6 bool, svcNetwork network.NetworkInfo, svcLogger klog.Logger) *EnsureHealthCheckResult
	// DeleteHealthCheckWithFirewall deletes health check (and firewall rule) for l4 service.
	DeleteHealthCheckWithFirewall(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, svcLogger klog.Logger) (string, error)
	// DeleteHealthCheckWithDualStackFirewalls deletes health check (and firewall rule) for l4 service, deletes IPv6 firewalls if asked.
	DeleteHealthCheckWithDualStackFirewalls(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, svcLogger klog.Logger) (string, error)
}

type EnsureHealthCheckResult struct {
	HCName                 string
	HCLink                 string
	HCFirewallRuleName     string
	HCFirewallRuleLink     string
	HCFirewallRuleIPv6Name string
	HCFirewallRuleIPv6Link string
	GceResourceInError     string
	Err                    error
	WasUpdated             utils.ResourceSyncStatus
	WasFirewallUpdated     utils.ResourceSyncStatus
}

type healthChecksProvider interface {
	Get(name string, scope meta.KeyType) (*composite.HealthCheck, error)
	Create(healthCheck *composite.HealthCheck) error
	Update(name string, scope meta.KeyType, updatedHealthCheck *composite.HealthCheck) error
	Delete(name string, scope meta.KeyType) error
	SelfLink(name string, scope meta.KeyType) (string, error)
}
