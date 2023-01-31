package throttling

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/ingress-gce/pkg/utils/slice"
	"k8s.io/klog/v2"
)

const (
	errorsBeforeIncreasingDelay            = 3
	successesBeforeDecreasingDelay         = 2
	successesBeforeResettingDelay          = 15
	noRequestsTimeoutBeforeDecreasingDelay = 30 * time.Second
	noRequestsTimeoutBeforeResettingDelay  = 1 * time.Minute

	defaultQps   = 5
	defaultBurst = 10
)

// RequestGroup handles requests to the same API group and is backed by a Strategy.
type RequestGroup[R any] interface {
	// Run executes the request and pushes feedback to a strategy.
	// If the group should be delayed by Strategy, then it's blocked until the delay time passes.
	Run(f func() (R, error), version meta.Version) (R, error)
}

type NoResponse = any

type defaultRequestGroup[R any] struct {
	lock       sync.Mutex
	strategies map[meta.Version]Strategy
	clock      clock.Clock
	logger     klog.Logger
}

func NewDefaultRequestGroup[R any](minDelay, maxDelay time.Duration, logger klog.Logger) RequestGroup[R] {
	clock := clock.RealClock{}
	logger = logger.WithName("DefaultRequestGroup")
	strategies := make(map[meta.Version]Strategy)
	for _, version := range meta.AllVersions {
		strategy := NewDynamicTwoWayStrategy(
			minDelay,
			maxDelay,
			errorsBeforeIncreasingDelay,
			successesBeforeDecreasingDelay,
			successesBeforeResettingDelay,
			noRequestsTimeoutBeforeDecreasingDelay,
			noRequestsTimeoutBeforeResettingDelay,
			clock,
			logger,
		)
		strategies[version] = strategy
	}

	return &defaultRequestGroup[R]{
		strategies: strategies,
		clock:      clock,
		logger:     logger,
	}
}

func (g *defaultRequestGroup[R]) delayIfNeeded(strategy Strategy) {
	g.lock.Lock()
	defer g.lock.Unlock()
	delay := strategy.GetDelay()
	if delay != 0 {
		<-g.clock.After(delay)
	}
}

func (g *defaultRequestGroup[R]) Run(f func() (R, error), version meta.Version) (R, error) {
	strategy := g.strategies[version]
	g.delayIfNeeded(strategy)
	res, err := f()
	strategy.PushFeedback(err)
	return res, err
}

type qpsRequestGroup[R any] struct {
	lock         sync.Mutex
	rateLimiters map[meta.Version]flowcontrol.RateLimiter
}

func NewQpsRequestGroup[R any](specs []string, serviceAndOperation string, logger klog.Logger) RequestGroup[R] {
	logger = logger.WithName("QpsRequestGroup")
	return &qpsRequestGroup[R]{
		rateLimiters: createRateLimiters(specs, serviceAndOperation, logger),
	}
}

func createRateLimiters(specs []string, serviceAndOperation string, logger klog.Logger) map[meta.Version]flowcontrol.RateLimiter {
	rateLimiters := make(map[meta.Version]flowcontrol.RateLimiter)
	for _, spec := range specs {
		params := strings.Split(spec, ",")
		if !strings.HasSuffix(params[0], serviceAndOperation) {
			continue
		}

		rl, version := constructRateLimiter(params)
		if rl != nil {
			rateLimiters[*version] = rl
		}
	}
	for _, version := range meta.AllVersions {
		if rateLimiters[version] == nil {
			logger.V(4).Info(fmt.Sprintf("Using default rate limiter for %v.%v with qps=%v and burst=%v", version, serviceAndOperation, defaultQps, defaultBurst))
			rateLimiters[version] = flowcontrol.NewTokenBucketRateLimiter(defaultQps, defaultBurst)
		}
	}
	return rateLimiters
}

func constructRateLimiter(params []string) (flowcontrol.RateLimiter, *meta.Version) {
	if len(params) < 2 {
		return nil, nil
	}
	key := strings.Split(params[0], ".")
	if len(key) != 3 {
		return nil, nil
	}
	version := meta.Version(key[0])
	if !slice.Contains(meta.AllVersions, version, nil) {
		return nil, nil
	}
	rlType := params[1]
	if rlType != "qps" {
		return nil, nil
	}
	implArgs := params[2:]
	if len(implArgs) != 2 {
		return nil, nil
	}
	qps, err := strconv.ParseFloat(implArgs[0], 32)
	if err != nil || qps <= 0 {
		return nil, nil
	}
	burst, err := strconv.Atoi(implArgs[1])
	if err != nil {
		return nil, nil
	}
	return flowcontrol.NewTokenBucketRateLimiter(float32(qps), burst), &version
}

func (g *qpsRequestGroup[R]) delayIfNeeded(rateLimiter flowcontrol.RateLimiter) {
	g.lock.Lock()
	defer g.lock.Unlock()
	if rateLimiter != nil {
		rateLimiter.Accept()
	}
}

func (g *qpsRequestGroup[R]) Run(f func() (R, error), version meta.Version) (R, error) {
	g.delayIfNeeded(g.rateLimiters[version])
	return f()
}
