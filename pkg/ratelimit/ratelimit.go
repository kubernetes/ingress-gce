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
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/klog"
)

// GCERateLimiter implements cloud.RateLimiter
type GCERateLimiter struct {
	// Map a RateLimitKey to its rate limiter implementation.
	rateLimitImpls map[cloud.RateLimitKey]flowcontrol.RateLimiter
	// Minimum polling interval for getting operations. Underlying operations rate limiter
	// may increase the time.
	operationPollInterval time.Duration
}

// NewGCERateLimiter parses the list of rate limiting specs passed in and
// returns a properly configured cloud.RateLimiter implementation.
// Expected format of specs: {"[version].[service].[operation],[type],[param1],[param2],..", "..."}
func NewGCERateLimiter(specs []string, operationPollInterval time.Duration) (*GCERateLimiter, error) {
	rateLimitImpls := make(map[cloud.RateLimitKey]flowcontrol.RateLimiter)
	// Within each specification, split on comma to get the operation,
	// rate limiter type, and extra parameters.
	for _, spec := range specs {
		params := strings.Split(spec, ",")
		if len(params) < 2 {
			return nil, fmt.Errorf("must at least specify operation and rate limiter type.")
		}
		// params[0] should consist of the operation to rate limit.
		key, err := constructRateLimitKey(params[0])
		if err != nil {
			return nil, err
		}
		// params[1:] should consist of the rate limiter type and extra params.
		impl, err := constructRateLimitImpl(params[0], params[1:])
		if err != nil {
			return nil, err
		}
		rateLimitImpls[key] = impl
		klog.Infof("Configured rate limiting for: %v, scale = %f", key, flags.F.GCERateLimitScale)
	}
	if len(rateLimitImpls) == 0 {
		return nil, nil
	}
	return &GCERateLimiter{
		rateLimitImpls:        rateLimitImpls,
		operationPollInterval: operationPollInterval,
	}, nil
}

// Accept looks up the associated flowcontrol.RateLimiter (if exists) and waits on it.
func (l *GCERateLimiter) Accept(ctx context.Context, key *cloud.RateLimitKey) error {
	var rl cloud.RateLimiter

	impl := l.rateLimitImpl(key)
	if impl != nil {
		// Wrap the flowcontrol.RateLimiter with a AcceptRateLimiter and handle context.
		rl = &cloud.AcceptRateLimiter{Acceptor: impl}
	} else {
		// Check the context then use the cloud NopRateLimiter which accepts immediately.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rl = &cloud.NopRateLimiter{}
	}

	if key.Operation == "Get" && key.Service == "Operations" {
		// Wait a minimum amount of time regardless of rate limiter.
		rl = &cloud.MinimumRateLimiter{
			RateLimiter: rl,
			Minimum:     l.operationPollInterval,
		}
	}

	return rl.Accept(ctx, key)
}

// rateLimitImpl returns the flowcontrol.RateLimiter implementation
// associated with the passed in key.
func (l *GCERateLimiter) rateLimitImpl(key *cloud.RateLimitKey) flowcontrol.RateLimiter {
	// Since the passed in key will have the ProjectID field filled in, we need to
	// create a copy which does not, so that retreiving the rate limiter implementation
	// through the map works as expected.
	keyCopy := cloud.RateLimitKey{
		ProjectID: "",
		Operation: key.Operation,
		Version:   key.Version,
		Service:   key.Service,
	}
	return l.rateLimitImpls[keyCopy]
}

// Expected format of param is [version].[service].[operation]
func constructRateLimitKey(param string) (cloud.RateLimitKey, error) {
	var retVal cloud.RateLimitKey
	params := strings.Split(param, ".")
	if len(params) != 3 {
		return retVal, fmt.Errorf("must specify rate limit in [version].[service].[operation] format: %v", param)
	}
	// TODO(rramkumar): Add another layer of validation here?
	version := meta.Version(params[0])
	service := params[1]
	operation := params[2]
	retVal = cloud.RateLimitKey{
		ProjectID: "",
		Operation: operation,
		Version:   version,
		Service:   service,
	}
	return retVal, nil
}

// constructRateLimitImpl parses the slice and returns a flowcontrol.RateLimiter
// Expected format is [type],[param1],[param2],...
// `key` is used for logging.
func constructRateLimitImpl(key string, params []string) (flowcontrol.RateLimiter, error) {
	// For now, only the "qps" type is supported.
	rlType := params[0]
	implArgs := params[1:]
	if rlType == "qps" {
		if len(implArgs) != 2 {
			return nil, fmt.Errorf("invalid number of args for rate limiter type %v. Expected %d, Got %v", rlType, 2, len(implArgs))
		}
		qps, err := strconv.ParseFloat(implArgs[0], 32)
		if err != nil || qps <= 0 {
			return nil, fmt.Errorf("invalid argument for rate limiter type %v. Either %v is not a float or not greater than 0.", rlType, implArgs[0])
		}
		burst, err := strconv.Atoi(implArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for rate limiter type %v. Expected %v to be a int.", rlType, implArgs[1])
		}
		if flags.F.GCERateLimitScale <= 1.0 {
			klog.Infof("GCERateLimitScale <= 1.0 (is %f), not adjusting rate limits", flags.F.GCERateLimitScale)
		} else {
			oldQPS := qps
			oldBurst := burst
			qps = qps * flags.F.GCERateLimitScale
			burst = int(float64(burst) * flags.F.GCERateLimitScale)
			klog.Infof("Adjusted QPS rate limit according to scale: qps was %f, now %f; burst was %d, now %d", oldQPS, qps, oldBurst, burst)
		}
		tokenBucket := flowcontrol.NewTokenBucketRateLimiter(float32(qps), burst)
		return tokenBucket, nil
	}
	return nil, fmt.Errorf("invalid rate limiter type provided: %v", rlType)
}
