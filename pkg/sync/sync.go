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

	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/utils"
)

// ErrSkipBackendsSync is an error that can be returned by a Controller to
// indicate that syncing of backends was skipped and that all other future
// processes should be skipped as well.
var ErrSkipBackendsSync = errors.New("ingress skip backends sync and beyond")

// IngressSyncer processes an Ingress spec and produces a load balancer given
// an implementation of Controller.
type IngressSyncer struct {
	controller Controller
}

func NewIngressSyncer(controller Controller) Syncer {
	return &IngressSyncer{controller}
}

// Sync implements Syncer.
func (s *IngressSyncer) Sync(state interface{}) error {
	if err := s.controller.SyncBackends(state); err != nil {
		if err == ErrSkipBackendsSync {
			return nil
		}
		return fmt.Errorf("error running backend syncing routine: %v", err)
	}

	if err := s.controller.SyncLoadBalancer(state); err != nil {
		return fmt.Errorf("error running load balancer syncing routine: %v", err)
	}

	if err := s.controller.PostProcess(state); err != nil {
		return fmt.Errorf("error running post-process routine: %v", err)
	}

	return nil
}

// GC implements Syncer.
func (s *IngressSyncer) GC(ings []*v1beta1.Ingress) error {
	// Partition GC state into ingresses those need cleanup and those don't.
	// An Ingress is considered to exist and not considered for cleanup, if:
	// 1) It is a GCLB Ingress.
	// 2) It is not a candidate for deletion.
	toCleanup, toKeep := operator.Ingresses(ings).Partition(utils.NeedsCleanup)
	toKeepIngresses := toKeep.AsList()
	lbErr := s.controller.GCLoadBalancers(toKeepIngresses)
	beErr := s.controller.GCBackends(toKeepIngresses)
	if lbErr != nil {
		return fmt.Errorf("error running load balancer garbage collection routine: %v", lbErr)
	}
	if beErr != nil {
		return fmt.Errorf("error running backend garbage collection routine: %v", beErr)
	}

	return s.controller.MaybeRemoveFinalizers(toCleanup.AsList())
}
