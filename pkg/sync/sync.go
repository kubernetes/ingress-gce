package sync

import (
	extensions "k8s.io/api/extensions/v1beta1"
)

// IngressSyncer processes an Ingress spec and produces a load balancer given
// an implementation of Controller.
type IngressSyncer struct {
	controller Controller
}

func NewIngressSyncer(controller Controller) Syncer {
	return &IngressSyncer{controller}
}

// Implements Syncer
func (s *IngressSyncer) Sync(ing *extensions.Ingress) error {
	state, err := s.controller.PreProcess(ing)
	if err != nil {
		return err
	}

	if err := s.controller.SyncBackends(ing, state); err != nil {
		return err
	}

	if err := s.controller.SyncLoadBalancer(ing, state); err != nil {
		return err
	}

	if err := s.controller.PostProcess(ing); err != nil {
		return err
	}
	return nil
}

// Implements Syncer
func (s *IngressSyncer) GC(state interface{}) error {
	if err := s.controller.GCBackends(state); err != nil {
		return err
	}
	if err := s.controller.GCLoadBalancers(state); err != nil {
		return err
	}
	return nil
}
