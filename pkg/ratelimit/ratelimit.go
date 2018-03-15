/*
Copyright 2018 The Kubernetes Authors.

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

package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce/cloud/meta"
)

// GCERateLimiter implements cloud.RateLimiter
type GCERateLimiter struct {
	// Map a RateLimitKey to its rate limiter implementation.
	rateLimitImpls map[*cloud.RateLimitKey]flowcontrol.RateLimiter
}

// NewGCERateLimiter parses the list of rate limiting specs passed in and
// returns a properly configured cloud.RateLimiter implementation.
// Expected format of specs: {"[version].[service].[operation],[type],[param1],[param2],..", "..."}
func NewGCERateLimiter(specs []string) (*GCERateLimiter, error) {
	rateLimitImpls := make(map[*cloud.RateLimitKey]flowcontrol.RateLimiter)
	// Within each specification, split on comma to get the operation,
	// rate limiter type, and extra parameters.
	for _, spec := range specs {
		params := strings.Split(spec, ",")
		if len(params) < 2 {
			return nil, fmt.Errorf("Must at least specify operation and rate limiter type.")
		}
		// params[0] should consist of the operation to rate limit.
		keys, err := constructRateLimitKeys(params[0])
		if err != nil {
			return nil, err
		}
		// params[1:] should consist of the rate limiter type and extra params.
		impl, err := constructRateLimitImpl(params[1:])
		if err != nil {
			return nil, err
		}
		// For each spec, the rate limiter type is the same for all keys generated.
		for _, key := range keys {
			rateLimitImpls[key] = impl
			glog.Infof("Configured rate limiting for: %v", key)
		}
	}
	if len(rateLimitImpls) == 0 {
		return nil, nil
	}
	return &GCERateLimiter{rateLimitImpls}, nil
}

// Implementation of cloud.RateLimiter
func (l *GCERateLimiter) Accept(ctx context.Context, key *cloud.RateLimitKey) error {
	// If the rate limiter is empty, the do some default
	ch := make(chan struct{})
	go func() {
		// Call flowcontrol.RateLimiter implementation.
		impl := l.rateLimitImpl(key)
		if impl != nil {
			impl.Accept()
		}
		close(ch)
	}()
	select {
	case <-ch:
		break
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// rateLimitImpl returns the flowcontrol.RateLimiter implementation
// associated with the passed in key.
func (l *GCERateLimiter) rateLimitImpl(key *cloud.RateLimitKey) flowcontrol.RateLimiter {
	// Since the passed in key will have the ProjectID field filled in, we need to
	// create a copy which does not, so that retreiving the rate limiter implementation
	// through the map works as expected.
	keyCopy := &cloud.RateLimitKey{
		ProjectID: "",
		Operation: key.Operation,
		Version:   key.Version,
		Service:   key.Service,
	}
	return l.rateLimitImpls[keyCopy]
}

// Expected format of param is [version].[service].[operation]
// this could return more than one cloud.RateLimitKey if "*" is used for either
// version, service, or operation (or more than one of those).
func constructRateLimitKeys(param string) ([]*cloud.RateLimitKey, error) {
	params := strings.Split(param, ".")
	if len(params) != 3 {
		return nil, fmt.Errorf("Must specify operation in [version].[service].[operation] format.")
	}
	keys := []*cloud.RateLimitKey{}

	// First parse the version.
	versions := []meta.Version{}
	if params[0] == "*" {
		versions = meta.AllVersions
	} else {
		// Validate that the full provided version exists
		if versionExists(params[0]) {
			versions = append(versions, meta.Version(params[0]))
		} else {
			return nil, fmt.Errorf("Invalid version specified: %v", params[0])
		}
	}
	// For each version we get, parse the service.
	for _, version := range versions {
		// Construct a list of all possible services for the version.
		services := []string{}
		if params[1] == "*" {
			for _, serviceInfo := meta.AllServices {
				// Only include in the list of possible services if the service is
				// available at the particular version we are looking at now.
				if serviceInfo.Version() == version {
						services = append(services, serviceInfo.Service)
				}
			}
		} else {
			// Validate that the full provided service exists.
			if serviceExists(params[1]) {
				services = append(services, params[1])
			} else {
				return nil, fmt.Errorf("Invalid service specified: %v", params[1])
			}
		}
		// For each service we get, parse the operation.
		for _, service := range services {
			// These operation exist for every service.
			operations := []string{}
			if params[2] == "*" {
				// Default for every service, regardless of version.
				// TODO(rramkumar): Implement support for additional methods.
				operations = []string{"Get", "List", "Insert", "Delete"}
			} else {
				// Validate that the full provided operation exists.
				if operationExists(params[2]) {
					operations = append(operations, params[2])
				} else {
					return nil, fmt.Errorf("Invalid operation specified: %v", params[2])
				}
			}
			for _, operation := range operations {
				key := &cloud.RateLimitKey{
					ProjectID: "",
					Operation: operation,
					Version: version,
					Service: service,
				}
				keys = append(keys, key)
			}
		}
	}
	return keys, nil
}

// constructRateLimitImpl parses the slice and returns a flowcontrol.RateLimiter
// Expected format is [type],[param1],[param2],...
func constructRateLimitImpl(params []string) (flowcontrol.RateLimiter, error) {
	// For now, only the "qps" type is supported.
	rlType := params[0]
	implArgs := params[1:]
	if rlType == "qps" {
		if len(implArgs) != 2 {
			return nil, fmt.Errorf("Invalid number of args for rate limiter type %v. Expected %d, Got %v", rlType, 2, len(implArgs))
		}
		qps, err := strconv.ParseFloat(implArgs[0], 32)
		if err != nil || qps <= 0 {
			return nil, fmt.Errorf("Invalid argument for rate limiter type %v. Either %v is not a float or not greater than 0.", rlType, implArgs[0])
		}
		burst, err := strconv.Atoi(implArgs[1])
		if err != nil {
			return nil, fmt.Errorf("Invalid argument for rate limiter type %v. Expected %v to be a int.", rlType, implArgs[1])
		}
		return flowcontrol.NewTokenBucketRateLimiter(float32(qps), burst), nil
	}
	return nil, fmt.Errorf("Invalid rate limiter type provided: %v", rlType)
}

// versionExists returns true if the passed in string is a valid meta.Version.
func versionExists(s string) bool {
	for _, version := range meta.AllVersions {
		if meta.Version(s) == version {
			return true
		}
	}
	return false
}

// serviceExists returns true if the passed in string refers to a valid GCE service.
func serviceExists(s string) bool {
	for _, serviceInfo := range meta.AllServices {
		if s == serviceInfo.Service {
			return true
		}
	}
	return false
}

// operationExists returns true if the passed string refers to a valid operation.
// Current valid operations are "Get", "List", "Insert", "Delete"
// TODO(rramkumar): Implement support for more methods.
func operationExists(s string) bool {
	for _, operation := []string{"Get", "List", "Insert", "Delete"} {
		if s == operation {
			return true
		}
	}
	return false
}
