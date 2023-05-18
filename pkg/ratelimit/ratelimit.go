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
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"golang.org/x/exp/slices"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/ratelimit/metrics"
	"k8s.io/ingress-gce/pkg/throttling"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

// GCERateLimiter implements cloud.RateLimiter
type GCERateLimiter struct {
	// Map a RateLimitKey to its rate limiter implementation.
	rateLimitImpls map[cloud.RateLimitKey]flowcontrol.RateLimiter
	strategyRLs    map[cloud.RateLimitKey]*strategyRateLimiter
	// Minimum polling interval for getting operations. Underlying operations rate limiter
	// may increase the time.
	operationPollInterval time.Duration
}

// strategyRateLimiter implements cloud.RateLimiter and uses underlying throttling.Strategy
type strategyRateLimiter struct {
	lock     sync.Mutex
	strategy throttling.Strategy

	clock clock.Clock
}

// Accept blocks for the delay provided by the throttling.Strategy or until context.Done(). Key is ignored.
func (rl *strategyRateLimiter) Accept(ctx context.Context, _ *cloud.RateLimitKey) error {
	rl.lock.Lock()
	defer rl.lock.Unlock()
	select {
	case <-rl.clock.After(rl.strategy.Delay()):
		break
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Observe passes an error further to the throttling.Strategy. Key is ignored.
func (rl *strategyRateLimiter) Observe(_ context.Context, err error, _ *cloud.RateLimitKey) {
	rl.strategy.Observe(err)
}

func init() {
	metrics.RegisterMetrics()
}

// NewGCERateLimiter parses the list of rate limiting specs passed in and
// returns a properly configured cloud.RateLimiter implementation.
// Expected format of specs: {"[version].[service].[operation],[type],[param1],[param2],..", "..."}
func NewGCERateLimiter(specs []string, operationPollInterval time.Duration) (*GCERateLimiter, error) {
	rateLimitImpls := make(map[cloud.RateLimitKey]flowcontrol.RateLimiter)
	strategyRLs := make(map[cloud.RateLimitKey]*strategyRateLimiter)
	// Within each specification, split on comma to get the operation,
	// rate limiter type, and extra parameters.
	for _, spec := range specs {
		params := strings.Split(spec, ",")
		if len(params) < 2 {
			return nil, fmt.Errorf("must at least specify operation and rate limiter type")
		}
		// params[0] should consist of the operation to rate limit.
		key, err := constructRateLimitKey(params[0])
		if err != nil {
			return nil, err
		}
		// params[1:] should consist of the rate limiter type and extra params.
		if params[1] == "strategy" {
			impl, err := constructStrategy(params[2:])
			if err != nil {
				return nil, err
			}
			strategyRLs[key] = &strategyRateLimiter{
				strategy: impl,
				clock:    clock.RealClock{},
			}
			klog.Infof("Configured strategy for: %v", key)
		} else {
			impl, err := constructRateLimitImpl(params[1:])
			if err != nil {
				return nil, err
			}
			rateLimitImpls[key] = impl
			klog.Infof("Configured rate limiting for: %v, scale = %f", key, flags.F.GCERateLimitScale)
		}
	}
	if len(rateLimitImpls) == 0 && len(strategyRLs) == 0 {
		return nil, nil
	}
	return &GCERateLimiter{
		rateLimitImpls:        rateLimitImpls,
		strategyRLs:           strategyRLs,
		operationPollInterval: operationPollInterval,
	}, nil
}

// Accept looks up the associated strategyRateLimiter (if exists) and waits on it.
// Then it looks up the associated flowcontrol.RateLimiter (if exists) and waits on it.
func (grl *GCERateLimiter) Accept(ctx context.Context, key *cloud.RateLimitKey) error {
	if strategyRL := grl.strategyRLs[rateLimitKeyWithoutProject(key)]; strategyRL != nil {
		err := strategyRL.Accept(ctx, key)
		if err != nil {
			return err
		}
	}

	var rl cloud.RateLimiter
	if impl := grl.rateLimitImpls[rateLimitKeyWithoutProject(key)]; impl != nil {
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
			Minimum:     grl.operationPollInterval,
		}
	}

	start := time.Now()
	err := rl.Accept(ctx, key)
	metrics.PublishRateLimiterMetrics(fmt.Sprintf("%s.%s.%s", key.Version, key.Service, key.Operation), start)
	return err
}

// Observe looks up the associated strategyRateLimiter (if exists) and passes an error there to observe.
func (grl *GCERateLimiter) Observe(ctx context.Context, err error, key *cloud.RateLimitKey) {
	if rl := grl.strategyRLs[rateLimitKeyWithoutProject(key)]; rl != nil {
		rl.Observe(ctx, err, key)
	}
}

// rateLimitKeyWithoutProject returns a copy without ProjectID
// Since the passed in key will have the ProjectID field filled in, we need to
// create a copy which does not, so that retrieving the rate limiter implementation
// through the map works as expected.
func rateLimitKeyWithoutProject(key *cloud.RateLimitKey) cloud.RateLimitKey {
	return cloud.RateLimitKey{
		ProjectID: "",
		Operation: key.Operation,
		Version:   key.Version,
		Service:   key.Service,
	}
}

// Expected format of param is [version].[service].[operation]
func constructRateLimitKey(param string) (cloud.RateLimitKey, error) {
	var retVal cloud.RateLimitKey
	params := strings.Split(param, ".")
	if len(params) != 3 {
		return retVal, fmt.Errorf("must specify rate limit in [version].[service].[operation] format: %v", param)
	}
	version := meta.Version(params[0])
	if !slices.Contains(meta.AllVersions, version) {
		return retVal, fmt.Errorf("must specify supported version, got %v", params[0])
	}
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
func constructRateLimitImpl(params []string) (flowcontrol.RateLimiter, error) {
	// For now, only the "qps" type is supported.
	rlType := params[0]
	implArgs := params[1:]
	if rlType == "qps" {
		if len(implArgs) != 2 {
			return nil, fmt.Errorf("invalid number of args for rate limiter type %v. Expected %d, Got %v", rlType, 2, len(implArgs))
		}
		qps, err := strconv.ParseFloat(implArgs[0], 32)
		if err != nil || qps <= 0 {
			return nil, fmt.Errorf("invalid argument for rate limiter type %v, either %v is not a float or not greater than 0", rlType, implArgs[0])
		}
		burst, err := strconv.Atoi(implArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for rate limiter type %v, expected %v to be an int", rlType, implArgs[1])
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

// constructStrategy parses the slice and returns a throttling.Strategy
// Expected format is [type],[param1],[param2],...
func constructStrategy(params []string) (throttling.Strategy, error) {
	strategyType := params[0]
	implArgs := params[1:]
	if strategyType == "dynamic" {
		if len(implArgs) != 7 {
			return nil, fmt.Errorf("invalid number of args for strategy type %v. Expected %d, Got %d", strategyType, 2, len(implArgs))
		}
		minDelay, err := time.ParseDuration(implArgs[0])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for strategy type %v, could not parse minDelay=%q: %v", strategyType, implArgs[0], err)
		}
		maxDelay, err := time.ParseDuration(implArgs[1])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for strategy type %v, could not parse maxDelay=%q: %v", strategyType, implArgs[1], err)
		}
		errorsBeforeIncreasingDelay, err := strconv.Atoi(implArgs[2])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for strategy type %v, expected errorsBeforeIncreasingDelay=%q to be an int: %v", strategyType, implArgs[2], err)
		}
		successesBeforeDecreasingDelay, err := strconv.Atoi(implArgs[3])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for strategy type %v, expected successesBeforeDecreasingDelay=%q to be an int: %v", strategyType, implArgs[3], err)
		}
		successesBeforeResettingDelay, err := strconv.Atoi(implArgs[4])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for strategy type %v, expected successesBeforeResettingDelay=%q to be an int: %v", strategyType, implArgs[4], err)
		}
		noRequestsTimeoutBeforeDecreasingDelay, err := time.ParseDuration(implArgs[5])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for strategy type %v, could not parse noRequestsTimeoutBeforeDecreasingDelay=%q: %v", strategyType, implArgs[5], err)
		}
		noRequestsTimeoutBeforeResettingDelay, err := time.ParseDuration(implArgs[6])
		if err != nil {
			return nil, fmt.Errorf("invalid argument for strategy type %v, could not parse noRequestsTimeoutBeforeResettingDelay=%q: %v", strategyType, implArgs[6], err)
		}
		return throttling.NewDynamicStrategy(minDelay, maxDelay, errorsBeforeIncreasingDelay, successesBeforeDecreasingDelay, successesBeforeResettingDelay, noRequestsTimeoutBeforeDecreasingDelay, noRequestsTimeoutBeforeResettingDelay, clock.RealClock{})
	}
	return nil, fmt.Errorf("invalid strategy type provided: %v", strategyType)
}
