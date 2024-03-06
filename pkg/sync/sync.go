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

package sync

import (
	"errors"
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

// ErrSkipBackendsSync is an error that can be returned by a Controller to
// indicate that syncing of backends was skipped and that all other future
// processes should be skipped as well.
var ErrSkipBackendsSync = errors.New("ingress skip backends sync and beyond")

// IngressSyncer processes an Ingress spec and produces a load balancer given
// an implementation of Controller.
type IngressSyncer struct {
	controller Controller

	logger klog.Logger
}

func NewIngressSyncer(controller Controller, logger klog.Logger) Syncer {
	return &IngressSyncer{controller, logger.WithName("IngressSyncer")}
}

// Sync implements Syncer.
func (s *IngressSyncer) Sync(state interface{}, ingLogger klog.Logger) error {
	if err := s.controller.SyncBackends(state, ingLogger); err != nil {
		if err == ErrSkipBackendsSync {
			return nil
		}
		return fmt.Errorf("error running backend syncing routine: %v", err)
	}

	if err := s.controller.SyncLoadBalancer(state, ingLogger); err != nil {
		return fmt.Errorf("error running load balancer syncing routine: %v", err)
	}

	if err := s.controller.PostProcess(state, ingLogger); err != nil {
		return fmt.Errorf("error running post-process routine: %v", err)
	}

	return nil
}

// GC implements Syncer.
func (s *IngressSyncer) GC(ings []*v1.Ingress, currIng *v1.Ingress, frontendGCAlgorithm utils.FrontendGCAlgorithm, scope meta.KeyType, ingLogger klog.Logger) error {
	var lbErr, err error
	var errs []error
	switch frontendGCAlgorithm {
	case utils.CleanupV2FrontendResources:
		ingLogger.V(3).Info("Using algorithm CleanupV2FrontendResources to GC frontend of ingress")
		lbErr = s.controller.GCv2LoadBalancer(currIng, scope)

		defer func() {
			if err != nil {
				return
			}
			err = s.controller.EnsureDeleteV2Finalizer(currIng, ingLogger)
		}()
	case utils.CleanupV2FrontendResourcesScopeChange:
		ingLogger.V(3).Info("Using algorithm CleanupV2FrontendResourcesScopeChange to GC frontend of ingress")
		lbErr = s.controller.GCv2LoadBalancer(currIng, scope)
	case utils.CleanupV1FrontendResources:
		ingLogger.V(3).Info("Using algorithm CleanupV1FrontendResources to GC frontend of ingress")
		// Filter GCE ingresses that use v1 naming scheme.
		v1Ingresses := operator.Ingresses(ings).Filter(func(ing *v1.Ingress) bool {
			return namer.FrontendNamingScheme(ing, s.logger) == namer.V1NamingScheme
		})
		// Partition these into ingresses those need cleanup and those don't.
		toCleanupV1, toKeepV1 := v1Ingresses.Partition(utils.NeedsCleanup)
		// Note that only GCE ingress associated resources are managed by this controller.
		toKeepV1Gce := toKeepV1.Filter(utils.IsGCEIngress)
		lbErr = s.controller.GCv1LoadBalancers(toKeepV1Gce.AsList())

		defer func() {
			if err != nil {
				return
			}
			err = s.controller.EnsureDeleteV1Finalizers(toCleanupV1.AsList(), ingLogger)
		}()
	case utils.NoCleanUpNeeded:
		ingLogger.V(3).Info("Using algorithm NoCleanUpNeeded to GC frontend of ingress")
	default:
		lbErr = fmt.Errorf("unexpected frontend GC algorithm %v", frontendGCAlgorithm)
	}
	if lbErr != nil {
		errs = append(errs, fmt.Errorf("error running load balancer garbage collection routine: %v", lbErr))
	}
	// Filter ingresses that needs to exist after GC.
	// An Ingress is considered to exist and not considered for cleanup, if:
	// 1) It is a GCLB Ingress.
	// 2) It is not a deletion candidate. A deletion candidate is an ingress
	//    with deletion stamp and a finalizer.
	toKeep := operator.Ingresses(ings).Filter(func(ing *v1.Ingress) bool {
		return !utils.NeedsCleanup(ing)
	}).AsList()
	if beErr := s.controller.GCBackends(toKeep, ingLogger); beErr != nil {
		errs = append(errs, fmt.Errorf("error running backend garbage collection routine: %v", beErr))
	}
	if errs != nil {
		err = utils.JoinErrs(errs)
	}
	return err
}
