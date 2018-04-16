// Copyright 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mapper

import (
	"fmt"

	multierror "github.com/hashicorp/go-multierror"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
)

// Implements ClusterServiceMapper
type clusterServiceMapper struct {
	// Function to convert a (svcName, namespace) pair into a Service object.
	svcGetter func(svcName string, namespace string) (*v1.Service, error)
	// Services that are expected to exist in cluster.
	expectedSvcs map[string]bool
}

var _ ClusterServiceMapper = &clusterServiceMapper{}

// NewClusterServiceMapper creates a new ClusterServiceMapper given:
// 1. a list of services expected to exist in the cluster. If empty, then all services are expected to exist.
// 2. a function that gets a Service object from a cluster given its name and namespace.
func NewClusterServiceMapper(
	svcGetter func(svcName string, namespace string) (*v1.Service, error),
	expectedSvcs []string) ClusterServiceMapper {
	es := make(map[string]bool)
	if expectedSvcs == nil {
		return &clusterServiceMapper{svcGetter, es}
	}
	for _, expectedSvc := range expectedSvcs {
		es[expectedSvc] = true
	}
	return &clusterServiceMapper{svcGetter, es}
}

// Services returns a mapping for a cluster of IngressBackend -> Service given an Ingress.
func (c *clusterServiceMapper) Services(ing *v1beta1.Ingress) (map[v1beta1.IngressBackend]v1.Service, error) {
	backendToService := make(map[v1beta1.IngressBackend]v1.Service)
	var result *multierror.Error

	defaultBackend := ing.Spec.Backend
	if defaultBackend != nil {
		// We don't expect this service to exist
		// so don't bother actually getting it.
		if !c.expectedToExist(defaultBackend.ServiceName) {
			goto Loop
		}
		svc, err := c.svcGetter(defaultBackend.ServiceName, ing.Namespace)
		if err != nil {
			result = multierror.Append(result, fmt.Errorf("error getting service %v/%v for backend %+v: %v", ing.Namespace, defaultBackend.ServiceName, *defaultBackend, err))
		} else {
			backendToService[*defaultBackend] = *svc
		}
	}
Loop:
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			result = multierror.Append(result, fmt.Errorf("no HTTP rule specified in %v", rule))
			continue
		}
		for _, path := range rule.HTTP.Paths {
			svcName := path.Backend.ServiceName
			if !c.expectedToExist(svcName) {
				// We don't expect this service to exist
				// so don't bother actually getting it.
				continue
			}
			svc, err := c.svcGetter(path.Backend.ServiceName, ing.Namespace)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("error getting service %v/%v for backend %+v: %v", ing.Namespace, path.Backend.ServiceName, path.Backend, err))
				continue
			}
			backendToService[path.Backend] = *svc
		}
	}
	return backendToService, result.ErrorOrNil()
}

// expectedToExist returns true if the provided service name is expected to exist.
func (c *clusterServiceMapper) expectedToExist(svcName string) bool {
	if _, ok := c.expectedSvcs[svcName]; !ok && len(c.expectedSvcs) > 0 {
		return false
	}
	return true
}
