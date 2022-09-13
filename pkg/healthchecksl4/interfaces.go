package healthchecksl4

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

// L4HealthChecks defines methods for creating and deleting health checks (and their firewall rules) for l4 services
type L4HealthChecks interface {
	// EnsureHealthCheckWithFirewall creates health check with firewall rule for l4 service.
	EnsureHealthCheckWithFirewall(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType, nodeNames []string) *EnsureL4HealthCheckResult
	// DeleteHealthCheckWithFirewall deletes health check with firewall rule for l4 service.
	DeleteHealthCheckWithFirewall(svc *v1.Service, namer namer.L4ResourcesNamer, sharedHC bool, scope meta.KeyType, l4Type utils.L4LBType) (string, error)
}

type EnsureL4HealthCheckResult struct {
	HCName             string
	HCLink             string
	HCFirewallRuleName string
	GceResourceInError string
	Err                error
}

type healthChecksProvider interface {
	Get(name string, scope meta.KeyType) (*composite.HealthCheck, error)
	Create(healthCheck *composite.HealthCheck) error
	Update(name string, scope meta.KeyType, updatedHealthCheck *composite.HealthCheck) error
	Delete(name string, scope meta.KeyType) error
	SelfLink(name string, scope meta.KeyType) (string, error)
}
