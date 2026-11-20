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

package standaloneneg

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

// syncFetcher retrieves the GCE resources of the load balancers during a
// single sync. Backend services and health checks are memoised by URL,
// including errors, so that the resources shared by multiple forwarding rules
// are retrieved only once.
type syncFetcher struct {
	cloud   *gce.Cloud
	logger  klog.Logger
	bsByURL map[string]bsResult
	hcByURL map[string]hcResult
}

type bsResult struct {
	bs  *composite.BackendService
	err error
}

type hcResult struct {
	hc  *composite.HealthCheck
	err error
}

func newSyncFetcher(cloud *gce.Cloud, logger klog.Logger) *syncFetcher {
	return &syncFetcher{
		cloud:   cloud,
		logger:  logger,
		bsByURL: make(map[string]bsResult),
		hcByURL: make(map[string]hcResult),
	}
}

// forwardingRule retrieves the referenced forwarding rule. A forwarding rule
// that does not exist is a user error.
func (f *syncFetcher) forwardingRule(ref frRef) (*composite.ForwardingRule, error) {
	fr, err := composite.GetForwardingRule(f.cloud, ref.key, meta.VersionBeta, f.logger)
	if err != nil {
		if utils.IsNotFoundError(err) {
			f.logger.Error(err, "failed to get forwarding rule", "frName", ref.raw)
			err = l4utils.NewUserError(err)
		}
		return nil, err
	}
	return fr, nil
}

// backendService retrieves the backend service with the given URL and key.
// A backend service that does not exist is a user error.
func (f *syncFetcher) backendService(url string, key *meta.Key) (*composite.BackendService, error) {
	if result, ok := f.bsByURL[url]; ok {
		return result.bs, result.err
	}
	bs, err := composite.GetBackendService(f.cloud, key, meta.VersionBeta, f.logger)
	if err != nil {
		wrappedErr := fmt.Errorf("failed to get backend service %s: %w", url, err)
		if utils.IsNotFoundError(err) {
			wrappedErr = l4utils.NewUserError(wrappedErr)
		}
		bs, err = nil, wrappedErr
	}
	f.bsByURL[url] = bsResult{bs: bs, err: err}
	return bs, err
}

// healthCheck retrieves the health check with the given URL and key. The
// error is returned as is, a health check that does not exist is handled by
// the caller.
func (f *syncFetcher) healthCheck(url string, key *meta.Key) (*composite.HealthCheck, error) {
	if result, ok := f.hcByURL[url]; ok {
		return result.hc, result.err
	}
	hc, err := composite.GetHealthCheck(f.cloud, key, meta.VersionBeta, f.logger)
	if err != nil {
		hc = nil
	}
	f.hcByURL[url] = hcResult{hc: hc, err: err}
	return hc, err
}
