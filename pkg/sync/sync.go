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
func (s *IngressSyncer) GC(state interface{}) error {
	lbErr := s.controller.GCLoadBalancers(state)
	beErr := s.controller.GCBackends(state)
	if lbErr != nil {
		return fmt.Errorf("error running load balancer garbage collection routine: %v", lbErr)
	}
	if beErr != nil {
		return fmt.Errorf("error running backend garbage collection routine: %v", beErr)
	}
	return nil
}
