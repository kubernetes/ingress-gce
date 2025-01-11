package systemhealth

import (
	"sync"

	"k8s.io/klog/v2"
)

// SystemHealth is responsible for checking the health of the ingress-gce
// binary and the controllers running inside this binary.
type SystemHealth struct {
	hcLock       sync.Mutex
	healthChecks map[string]func() error
	logger       klog.Logger
}

// NewSystemHealth creates a new SystemHealth instance.
func NewSystemHealth(logger klog.Logger) *SystemHealth {
	return &SystemHealth{
		healthChecks: make(map[string]func() error),
		logger:       logger,
	}
}

// AddHealthCheck registers a function to be called for health checking.
func (sh *SystemHealth) AddHealthCheck(id string, hc func() error) {
	sh.hcLock.Lock()
	defer sh.hcLock.Unlock()

	sh.logger.Info("Adding health check", "id", id)
	sh.healthChecks[id] = hc
}

// HealthCheckResults contains a mapping of component -> health check results.
type HealthCheckResults map[string]error

// HealthCheck runs all registered health check functions.
func (sh *SystemHealth) HealthCheck() HealthCheckResults {
	sh.hcLock.Lock()
	defer sh.hcLock.Unlock()

	healthChecks := make(map[string]error)
	for component, f := range sh.healthChecks {
		sh.logger.Info("Running health check", "component", component)
		healthChecks[component] = f()
	}

	sh.logger.Info("Health check results", "results", healthChecks)
	return healthChecks
}
