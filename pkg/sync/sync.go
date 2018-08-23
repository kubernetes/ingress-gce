package sync

import (
	"errors"
	"fmt"

	extensions "k8s.io/api/extensions/v1beta1"
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
func (s *IngressSyncer) Sync(ing *extensions.Ingress) error {
	state, err := s.controller.PreProcess(ing)
	if err != nil {
		return fmt.Errorf("Error running pre-process routine: %v", err)
	}

	if err := s.controller.SyncBackends(ing, state); err != nil {
		if err == ErrSkipBackendsSync {
			return nil
		}
		return fmt.Errorf("Error running backend syncing routine: %v", err)
	}

	if err := s.controller.SyncLoadBalancer(ing, state); err != nil {
		return fmt.Errorf("Error running load balancer syncing routine: %v", err)
	}

	if err := s.controller.PostProcess(ing); err != nil {
		return fmt.Errorf("Error running post-process routine: %v", err)
	}
	return nil
}

// GC implements Syncer.
func (s *IngressSyncer) GC(state interface{}) error {
	lbErr := s.controller.GCLoadBalancers(state)
	beErr := s.controller.GCBackends(state)
	if lbErr != nil {
		return fmt.Errorf("Error running load balancer garbage collection routine: %v", lbErr)
	}
	if beErr != nil {
		return fmt.Errorf("Error running backend garbage collection routine: %v", beErr)
	}
	return nil
}
