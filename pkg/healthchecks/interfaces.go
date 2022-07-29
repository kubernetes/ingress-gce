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

package healthchecks

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
)

// HealthCheckProvider is an interface to manage a single GCE health check.
// TODO: (shance) convert this to use composite types
type HealthCheckProvider interface {
	CreateHTTPHealthCheck(hc *compute.HttpHealthCheck) error
	UpdateHTTPHealthCheck(hc *compute.HttpHealthCheck) error
	DeleteHTTPHealthCheck(name string) error
	GetHTTPHealthCheck(name string) (*compute.HttpHealthCheck, error)

	CreateAlphaHealthCheck(hc *computealpha.HealthCheck) error
	CreateBetaHealthCheck(hc *computebeta.HealthCheck) error
	CreateHealthCheck(hc *compute.HealthCheck) error
	UpdateAlphaHealthCheck(hc *computealpha.HealthCheck) error
	UpdateBetaHealthCheck(hc *computebeta.HealthCheck) error
	UpdateHealthCheck(hc *compute.HealthCheck) error
	DeleteHealthCheck(name string) error
	GetAlphaHealthCheck(name string) (*computealpha.HealthCheck, error)
	GetBetaHealthCheck(name string) (*computebeta.HealthCheck, error)
	GetHealthCheck(name string) (*compute.HealthCheck, error)
}

// HealthChecker is an interface to manage cloud HTTPHealthChecks.
type HealthChecker interface {
	// SyncServicePort syncs the healthcheck associated with the given
	// ServicePort and Pod Probe definition.
	//
	// `probe` can be nil if no probe exists.
	SyncServicePort(sp *utils.ServicePort, probe *v1.Probe) (string, error)
	Delete(name string, scope meta.KeyType) error
	Get(name string, version meta.Version, scope meta.KeyType) (*translator.HealthCheck, error)
}
