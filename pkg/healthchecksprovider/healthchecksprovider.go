package healthchecksprovider

import (
	"fmt"

	cloudprovider "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
)

type HealthChecks struct {
	cloud   *gce.Cloud
	version meta.Version
}

func NewHealthChecks(cloud *gce.Cloud, version meta.Version) *HealthChecks {
	return &HealthChecks{
		cloud:   cloud,
		version: version,
	}
}

func (hc *HealthChecks) Get(name string, scope meta.KeyType) (*composite.HealthCheck, error) {
	key, err := hc.createKey(name, scope)
	if err != nil {
		return nil, fmt.Errorf("hc.createKey(%s, %s) returned error %w, want nil", name, scope, err)
	}
	healthCheck, err := composite.GetHealthCheck(hc.cloud, key, hc.version)
	if err != nil {
		if utils.IsNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("composite.GetHealthCheck(_, %v, %v) returned error %w, want nil", key, meta.VersionGA, err)
	}
	return healthCheck, nil
}

func (hc *HealthChecks) Create(healthCheck *composite.HealthCheck) error {
	key, err := hc.createKey(healthCheck.Name, healthCheck.Scope)
	if err != nil {
		return fmt.Errorf("hc.createKey(%s, %s) returned error: %w, want nil", healthCheck.Name, healthCheck.Scope, err)
	}

	err = composite.CreateHealthCheck(hc.cloud, key, healthCheck)
	if err != nil {
		return fmt.Errorf("composite.CreateHealthCheck(_, %s, %v) returned error %w, want nil", key, healthCheck, err)
	}
	return nil
}

func (hc *HealthChecks) Update(name string, scope meta.KeyType, updatedHealthCheck *composite.HealthCheck) error {
	key, err := hc.createKey(name, scope)
	if err != nil {
		return fmt.Errorf("hc.createKey(%s, %s) returned error: %w, want nil", name, scope, err)
	}

	err = composite.UpdateHealthCheck(hc.cloud, key, updatedHealthCheck)
	if err != nil {
		return fmt.Errorf("composite.UpdateHealthCheck(_, %s, %v) returned error %w, want nil", key, updatedHealthCheck, err)
	}
	return nil
}

func (hc *HealthChecks) Delete(name string, scope meta.KeyType) error {
	key, err := hc.createKey(name, scope)
	if err != nil {
		return fmt.Errorf("hc.createKey(%s, %s) returned error %w, want nil", name, scope, err)
	}

	return utils.IgnoreHTTPNotFound(composite.DeleteHealthCheck(hc.cloud, key, hc.version))
}

func (hc *HealthChecks) SelfLink(name string, scope meta.KeyType) (string, error) {
	key, err := hc.createKey(name, scope)
	if err != nil {
		return "", fmt.Errorf("hc.createKey(%s, %s) returned error %w, want nil", name, scope, err)
	}

	return cloudprovider.SelfLink(meta.VersionGA, hc.cloud.ProjectID(), "healthChecks", key), nil
}

func (hc *HealthChecks) createKey(name string, scope meta.KeyType) (*meta.Key, error) {
	return composite.CreateKey(hc.cloud, name, scope)
}
