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

package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/composite"
	ccontext "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type parsedForwardingRule struct {
	key     *meta.Key
	rawName string
}

// StandaloneNEGLBController manages services with CustomNegLoadBalancerClass.
type StandaloneNEGLBController struct {
	ctx       *ccontext.ControllerContext
	svcQueue  utils.TaskQueue
	logger    klog.Logger
	stopCh    <-chan struct{}
	hasSynced func() bool
}

// NewStandaloneNEGLBController creates a new instance of StandaloneNEGLBController.
func NewStandaloneNEGLBController(ctx *ccontext.ControllerContext, stopCh <-chan struct{}, logger klog.Logger) *StandaloneNEGLBController {
	logger = logger.WithName("StandaloneNEGLBController")
	lc := &StandaloneNEGLBController{
		ctx:       ctx,
		stopCh:    stopCh,
		hasSynced: ctx.HasSynced,
		logger:    logger,
	}
	lc.svcQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers("standalone-l4-neg-lb", "services", 1, lc.sync, logger)

	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			svc := obj.(*v1.Service)
			if lc.shouldProcess(svc) {
				lc.enqueue(svc)
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			curSvc := cur.(*v1.Service)
			if lc.shouldProcess(curSvc) {
				lc.enqueue(curSvc)
			}
		},
	})

	return lc
}

func (lc *StandaloneNEGLBController) shouldProcess(svc *v1.Service) bool {
	if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false
	}
	return svc.Spec.LoadBalancerClass != nil && *svc.Spec.LoadBalancerClass == annotations.StandalonePassthroughNegLoadBalancerClass
}

func (lc *StandaloneNEGLBController) enqueue(svc *v1.Service) {
	lc.svcQueue.Enqueue(svc)
}

func (lc *StandaloneNEGLBController) Run() {
	lc.logger.Info("Starting StandaloneNEGLBController")

	err := wait.PollUntilContextCancel(wait.ContextForChannel(lc.stopCh), 5*time.Second, true, func(ctx context.Context) (done bool, err error) {
		lc.logger.V(2).Info("Waiting for initial cache sync before starting L4 Standalone NEG controller")
		return lc.hasSynced(), nil
	})
	if err != nil {
		// This will fail if the context is canceled which means the channel was closed. We should return in that case.
		lc.logger.Error(err, "Failed to wait for initial cache sync")
		return
	}

	defer lc.svcQueue.Shutdown()
	lc.svcQueue.Run()
	<-lc.stopCh
}

func (lc *StandaloneNEGLBController) sync(key string) error {
	obj, exists, err := lc.ctx.ServiceInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to lookup service for key %s: %w", key, err)
	}
	if !exists || obj == nil {
		return nil
	}
	svc := obj.(*v1.Service)
	if svc.DeletionTimestamp != nil {
		return nil
	}
	if !lc.shouldProcess(svc) {
		return nil
	}
	svcLogger := lc.logger.WithValues("service", klog.KObj(svc))

	return lc.syncStandaloneNEGLB(svc, svcLogger)
}

func (lc *StandaloneNEGLBController) parseForwardingRuleKeys(frNamesStr string, svcLogger klog.Logger) ([]parsedForwardingRule, []error) {

	if frNamesStr == "" {
		return nil, nil
	}
	var parsedRules []parsedForwardingRule
	var errs []error
	frNames := strings.Split(frNamesStr, ",")
	for _, frName := range frNames {
		frName = strings.TrimSpace(frName)
		if frName == "" {
			continue
		}
		var frKey *meta.Key
		if strings.Contains(frName, "/") {
			resourceID, err := cloud.ParseResourceURL(frName)
			if err != nil {
				svcLogger.Error(err, "failed to parse forwarding rule reference URL", "frRef", frName)
				errs = append(errs, fmt.Errorf("failed to parse forwarding rule reference URL %s: %w", frName, err))
				continue
			}
			frKey = resourceID.Key
		} else {
			frKey = meta.RegionalKey(frName, lc.ctx.Cloud.Region())
		}
		parsedRules = append(parsedRules, parsedForwardingRule{key: frKey, rawName: frName})
	}
	return parsedRules, errs
}

func validateForwardingRule(fr *composite.ForwardingRule, frName string) error {
	if fr.LoadBalancingScheme != string(cloud.SchemeExternal) {
		return fmt.Errorf("forwarding rule %s has unsupported load balancing scheme: %s", frName, fr.LoadBalancingScheme)
	}
	if fr.IPProtocol != "TCP" && fr.IPProtocol != "UDP" && fr.IPProtocol != "L3_DEFAULT" {
		return fmt.Errorf("forwarding rule %s has unsupported protocol: %s", frName, fr.IPProtocol)
	}
	return nil
}

func (lc *StandaloneNEGLBController) syncStandaloneNEGLB(svc *v1.Service, svcLogger klog.Logger) error {
	frNamesStr, ok := svc.Annotations[annotations.CustomForwardingRuleKey]
	if !ok || frNamesStr == "" {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		svcLogger.V(4).Info("Service has no forwarding rule reference, skipping")
		err := lc.clearStatusIngressIP(svc, svcLogger)
		if err != nil {
			return err
		}
		return nil
	}

	parsedRules, parseErrs := lc.parseForwardingRuleKeys(frNamesStr, svcLogger)
	var errs []error
	errs = append(errs, parseErrs...)

	if len(parsedRules) == 0 {
		err := lc.clearStatusIngressIP(svc, svcLogger)
		if err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Could not use any Forwarding Rule %s", errors.Join(errs...).Error())
			return errors.Join(errs...)
		}
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "NoForwardingRuleRef", "Service has no forwarding rule reference")
		return nil
	}

	var lbIngresses []v1.LoadBalancerIngress
	vipMode := v1.LoadBalancerIPModeVIP

	for _, parsed := range parsedRules {
		fr, err := composite.GetForwardingRule(lc.ctx.Cloud, parsed.key, meta.VersionGA, svcLogger)
		if err != nil {
			svcLogger.Error(err, "failed to get forwarding rule", "frName", parsed.rawName)
			errs = append(errs, err)
			continue
		}

		if err := validateForwardingRule(fr, parsed.rawName); err != nil {
			svcLogger.Error(err, "invalid forwarding rule", "frName", parsed.rawName)
			errs = append(errs, err)
			continue
		}

		lbIngresses = append(lbIngresses, v1.LoadBalancerIngress{IP: fr.IPAddress, IPMode: &vipMode})

	}

	if len(errs) > 0 {
		lc.ctx.Recorder(svc.Namespace).Eventf(svc, v1.EventTypeWarning, "ForwardingRuleUnusable", "Could not use all Forwarding Rules %s", errors.Join(errs...).Error())
	}
	// if at least one FR was ok then we use it
	if len(lbIngresses) == 0 {
		// if none of the FRs is usable remove any that is possibly there
		err := lc.clearStatusIngressIP(svc, svcLogger)
		if err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}

	newStatus := &v1.LoadBalancerStatus{
		Ingress: lbIngresses,
	}

	if err := updateServiceStatus(lc.ctx, svc, newStatus, nil, svcLogger); err != nil {
		return err
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (lc *StandaloneNEGLBController) clearStatusIngressIP(svc *v1.Service, svcLogger klog.Logger) error {
	if len(svc.Status.LoadBalancer.Ingress) == 0 {
		return nil
	}
	newStatus := &v1.LoadBalancerStatus{
		Ingress: nil,
	}

	if err := updateServiceStatus(lc.ctx, svc, newStatus, nil, svcLogger); err != nil {
		return err
	}
	return nil
}
