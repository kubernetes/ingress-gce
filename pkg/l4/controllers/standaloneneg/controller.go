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

// Package standaloneneg implements the L4 Standalone NEG LB controller.
//
// The controller manages LoadBalancer services with the
// networking.gke.io/standalone-passthrough-lb-neg load balancer class. The
// load balancer itself (forwarding rules, backend services and health checks)
// is created by the user, the NEGs of the service are created by the NEG
// controller. The service references its forwarding rules in the
// networking.gke.io/custom-forwarding-rule annotation.
//
// For each service the controller:
//  1. parses the forwarding rule references from the annotation,
//  2. retrieves the forwarding rules and their backend services, and checks
//     that they are supported and that the service NEGs are attached to them,
//  3. computes the networking.gke.io/external-hc annotation from the HTTP
//     health checks of the backend services,
//  4. updates the networking.gke.io/external-hc annotation, and
//  5. writes the IPs of the usable forwarding rules to the service status,
//     together with the ExternalIPProgrammed condition.
package standaloneneg

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	ccontext "k8s.io/ingress-gce/pkg/context"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	"k8s.io/ingress-gce/pkg/l4/resources"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	// controllerName is the name of the controller used in the metrics.
	controllerName = "standalone-neg-lb-controller"

	defaultNumWorkers = 1
)

// Controller manages the LoadBalancer services using the Standalone NEG load balancer class.
type Controller struct {
	ctx       *ccontext.ControllerContext
	svcQueue  utils.TaskQueue
	logger    klog.Logger
	namer     namer.L4ResourcesNamer
	stopCh    <-chan struct{}
	hasSynced func() bool
}

// NewController creates a new instance of Controller.
func NewController(ctx *ccontext.ControllerContext, stopCh <-chan struct{}, logger klog.Logger) *Controller {
	logger = logger.WithName("StandaloneNEGLBController")
	c := &Controller{
		ctx:       ctx,
		stopCh:    stopCh,
		namer:     ctx.L4Namer,
		hasSynced: ctx.HasSynced,
		logger:    logger,
	}
	c.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("standalone-l4-neg-lb", "services", defaultNumWorkers, c.syncWrapper, logger)

	ctx.ServiceInformer.AddEventHandler(c.serviceEventHandler())

	return c
}

// Run waits for the informer caches to sync and runs the controller until
// the stop channel is closed.
func (c *Controller) Run() {
	c.logger.Info("Starting StandaloneNEGLBController", "numWorkers", defaultNumWorkers)

	err := wait.PollUntilContextCancel(wait.ContextForChannel(c.stopCh), 5*time.Second, true, func(ctx context.Context) (done bool, err error) {
		c.logger.V(2).Info("Waiting for initial cache sync before starting L4 Standalone NEG controller")
		return c.hasSynced(), nil
	})
	if err != nil {
		// This will fail if the context is canceled which means the channel was closed. We should return in that case.
		c.logger.Error(err, "Failed to wait for initial cache sync")
		return
	}

	defer c.svcQueue.Shutdown()
	c.svcQueue.Run()
	<-c.stopCh
}

func (c *Controller) syncWrapper(key string) (err error) {
	syncTrackingID := rand.Int31()
	svcLogger := c.logger.WithValues("serviceKey", key, "syncId", syncTrackingID)

	defer func() {
		if r := recover(); r != nil {
			errMessage := fmt.Sprintf("Panic in L4 Standalone NEG LB sync worker goroutine: %v", r)
			svcLogger.Error(nil, errMessage)
			l4metrics.PublishL4ControllerPanicCount(controllerName)
			err = fmt.Errorf("%s", errMessage)
		}
	}()
	syncErr := c.sync(key, svcLogger)
	return syncErr
}

func (c *Controller) sync(key string, svcLogger klog.Logger) error {
	start := time.Now()
	l4metrics.PublishL4controllerLastSyncTime(controllerName)

	obj, exists, err := c.ctx.ServiceInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to lookup service for key %s: %w", key, err)
	}
	if !exists || obj == nil {
		svcLogger.V(3).Info("Ignoring sync of non-existent service")
		c.ctx.L4Metrics.DeleteL4StandaloneNEGService(key)
		return nil
	}
	svc := obj.(*v1.Service)

	if svc.DeletionTimestamp != nil {
		svcLogger.V(3).Info("Ignoring sync of service undergoing deletion")
		c.ctx.L4Metrics.DeleteL4StandaloneNEGService(key)
		return nil
	}
	if !wantsStandaloneNEGLB(svc) {
		svcLogger.V(3).Info("Ignoring sync: service does not match standalone LB criteria")
		c.ctx.L4Metrics.DeleteL4StandaloneNEGService(key)
		return c.removeLoadBalancer(svc, svcLogger)
	}

	schemes, err := c.syncLoadBalancer(svc, svcLogger)
	c.publishMetrics(key, schemes, err, start)
	return err
}

func (c *Controller) publishMetrics(key string, schemes sets.Set[string], syncErr error, start time.Time) {
	state := l4metrics.L4StandaloneNEGServiceState{
		Status: l4metrics.StatusSuccess,
	}
	if syncErr != nil {
		// TODO: IsUserError matches if any of the joined sync errors is a user error, a
		// sync failing with both user and system errors should be reported as a system error.
		if resources.IsUserError(syncErr) {
			state.Status = l4metrics.StatusUserError
		} else {
			state.Status = l4metrics.StatusError
			now := time.Now()
			state.FirstSyncErrorTime = &now
		}
	}

	state.LBSchemeExternal = schemes.Has("EXTERNAL")
	state.LBSchemeExternalPassthrough = schemes.Has("EXTERNAL_PASSTHROUGH")
	state.LBSchemeInternal = schemes.Has("INTERNAL")

	c.ctx.L4Metrics.SetL4StandaloneNEGService(key, state)
	l4metrics.PublishL4StandaloneNEGSyncLatency(syncErr == nil, start)
}
