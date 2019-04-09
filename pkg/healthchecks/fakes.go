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
	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	compute "google.golang.org/api/compute/v1"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/utils"
)

// NewFakeHealthCheckProvider returns a new FakeHealthChecks.
func NewFakeHealthCheckProvider() *FakeHealthCheckProvider {
	return &FakeHealthCheckProvider{
		http:    make(map[string]compute.HttpHealthCheck),
		generic: make(map[string]computealpha.HealthCheck),
	}
}

// FakeHealthCheckProvider fakes out health checks.
type FakeHealthCheckProvider struct {
	http    map[string]compute.HttpHealthCheck
	generic map[string]computealpha.HealthCheck
}

// CreateHttpHealthCheck fakes out http health check creation.
func (f *FakeHealthCheckProvider) CreateHTTPHealthCheck(hc *compute.HttpHealthCheck) error {
	v := *hc
	v.SelfLink = cloud.NewHttpHealthChecksResourceID("mock-project", hc.Name).SelfLink(meta.VersionGA)
	f.http[hc.Name] = v
	return nil
}

// GetHttpHealthCheck fakes out getting a http health check from the cloud.
func (f *FakeHealthCheckProvider) GetHTTPHealthCheck(name string) (*compute.HttpHealthCheck, error) {
	if hc, found := f.http[name]; found {
		return &hc, nil
	}

	return nil, utils.FakeGoogleAPINotFoundErr()
}

// DeleteHttpHealthCheck fakes out deleting a http health check.
func (f *FakeHealthCheckProvider) DeleteHTTPHealthCheck(name string) error {
	if _, exists := f.http[name]; !exists {
		return utils.FakeGoogleAPINotFoundErr()
	}

	delete(f.http, name)
	return nil
}

// UpdateHttpHealthCheck sends the given health check as an update.
func (f *FakeHealthCheckProvider) UpdateHTTPHealthCheck(hc *compute.HttpHealthCheck) error {
	if _, exists := f.http[hc.Name]; !exists {
		return utils.FakeGoogleAPINotFoundErr()
	}

	f.http[hc.Name] = *hc
	return nil
}

// CreateHealthCheck fakes out http health check creation.
func (f *FakeHealthCheckProvider) CreateHealthCheck(hc *compute.HealthCheck) error {
	v := *hc
	v.SelfLink = cloud.NewHealthChecksResourceID("mock-project", hc.Name).SelfLink(meta.VersionGA)
	alphaHC, _ := v1ToAlphaHealthCheck(&v)
	f.generic[hc.Name] = *alphaHC
	return nil
}

// CreateHealthCheck fakes out http health check creation.
func (f *FakeHealthCheckProvider) CreateAlphaHealthCheck(hc *computealpha.HealthCheck) error {
	v := *hc
	v.SelfLink = cloud.NewHealthChecksResourceID("mock-project", hc.Name).SelfLink(meta.VersionAlpha)
	f.generic[hc.Name] = v
	return nil
}

// CreateHealthCheck fakes out http health check creation.
func (f *FakeHealthCheckProvider) CreateBetaHealthCheck(hc *computebeta.HealthCheck) error {
	v := *hc
	v.SelfLink = cloud.NewHealthChecksResourceID("mock-project", hc.Name).SelfLink(meta.VersionBeta)
	alphaHC, _ := betaToAlphaHealthCheck(&v)
	f.generic[hc.Name] = *alphaHC
	return nil
}

// GetHealthCheck fakes out getting a http health check from the cloud.
func (f *FakeHealthCheckProvider) GetHealthCheck(name string) (*compute.HealthCheck, error) {
	if hc, found := f.generic[name]; found {
		v1HC, _ := toV1HealthCheck(&hc)
		return v1HC, nil
	}

	return nil, utils.FakeGoogleAPINotFoundErr()
}

func (f *FakeHealthCheckProvider) GetAlphaHealthCheck(name string) (*computealpha.HealthCheck, error) {
	if hc, found := f.generic[name]; found {
		return &hc, nil
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

func (f *FakeHealthCheckProvider) GetBetaHealthCheck(name string) (*computebeta.HealthCheck, error) {
	if hc, found := f.generic[name]; found {
		ret := &computebeta.HealthCheck{}
		err := copyViaJSON(ret, &hc)
		return ret, err
	}
	return nil, utils.FakeGoogleAPINotFoundErr()
}

// DeleteHealthCheck fakes out deleting a http health check.
func (f *FakeHealthCheckProvider) DeleteHealthCheck(name string) error {
	if _, exists := f.generic[name]; !exists {
		return utils.FakeGoogleAPINotFoundErr()
	}

	delete(f.generic, name)
	return nil
}

// UpdateHealthCheck sends the given health check as an update.
func (f *FakeHealthCheckProvider) UpdateHealthCheck(hc *compute.HealthCheck) error {
	if _, exists := f.generic[hc.Name]; !exists {
		return utils.FakeGoogleAPINotFoundErr()
	}
	alphaHC, _ := v1ToAlphaHealthCheck(hc)
	f.generic[hc.Name] = *alphaHC
	return nil
}

func (f *FakeHealthCheckProvider) UpdateAlphaHealthCheck(hc *computealpha.HealthCheck) error {
	if _, exists := f.generic[hc.Name]; !exists {
		return utils.FakeGoogleAPINotFoundErr()
	}

	f.generic[hc.Name] = *hc
	return nil
}

func (f *FakeHealthCheckProvider) UpdateBetaHealthCheck(hc *computebeta.HealthCheck) error {
	if _, exists := f.generic[hc.Name]; !exists {
		return utils.FakeGoogleAPINotFoundErr()
	}
	alphaHC, _ := betaToAlphaHealthCheck(hc)
	f.generic[hc.Name] = *alphaHC
	return nil
}
