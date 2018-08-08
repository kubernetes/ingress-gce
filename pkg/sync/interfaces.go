package sync

import (
	extensions "k8s.io/api/extensions/v1beta1"
)

// Syncer is an interface to sync GCP resources associated with an Ingress.
type Syncer interface {
	// Sync creates a full GCLB given an Ingress.
	Sync(ing *extensions.Ingress) error
	// GC cleans up GCLB resources for all Ingresses and can optionally
	// use some arbitrary state to help with the process.
	GC(state interface{}) error
}

// Controller is an interface for ingress controllers and declares methods
// on how to sync the various portions of the GCLB for an Ingress.
type Controller interface {
	// PreProcess allows for doing some pre-processing on an Ingress before
	// it is synced. Some arbitrary state can also be returned.
	PreProcess(ing *extensions.Ingress) (interface{}, error)
	// SyncBackends syncs the backends for a GCLB given an ingress or some
	// existing state.
	SyncBackends(ing *extensions.Ingress, state interface{}) error
	// GCBackends garbage collects backends for all Ingresses.
	GCBackends(state interface{}) error
	// SyncLoadBalancer syncs the front-end load balancer resources for a GCLB given
	// an ingress or some existing state.
	SyncLoadBalancer(ing *extensions.Ingress, state interface{}) error
	// GCLoadBalancers garbage collects front-end load balancer resources
	// for all Ingresses.
	GCLoadBalancers(state interface{}) error
	// PostProcess allows for doing some post-processing on an Ingress before
	// the overall sync is complete.
	PostProcess(ing *extensions.Ingress) error
}
