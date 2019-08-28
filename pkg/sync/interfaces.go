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
	"k8s.io/api/networking/v1beta1"
)

// Syncer is an interface to sync GCP resources associated with an Ingress.
type Syncer interface {
	// Sync creates a full GCLB given some state related to an Ingress.
	Sync(state interface{}) error
	// GC cleans up GCLB resources for all Ingresses and can optionally
	// use some arbitrary to help with the process.
	// TODO(rramkumar): Do we need to rethink the strategy of GC'ing
	// all Ingresses at once?
	GC(ings []*v1beta1.Ingress) error
}

// Controller is an interface for ingress controllers and declares methods
// on how to sync the various portions of the GCLB for an Ingress.
type Controller interface {
	// SyncBackends syncs the backends for a GCLB given some existing state.
	SyncBackends(state interface{}) error
	// GCBackends garbage collects backends for all ingresses given a list of ingresses to exclude from GC.
	GCBackends(toKeep []*v1beta1.Ingress) error
	// SyncLoadBalancer syncs the front-end load balancer resources for a GCLB given some existing state.
	SyncLoadBalancer(state interface{}) error
	// GCLoadBalancers garbage collects front-end load balancer resources for all ingresses given a list of ingresses to exclude from GC.
	GCLoadBalancers(toKeep []*v1beta1.Ingress) error
	// PostProcess allows for doing some post-processing after an Ingress is synced to a GCLB.
	PostProcess(state interface{}) error
	// MaybeRemoveFinalizers removes finalizers from a list of ingresses one by one after all of its associated resources are deleted.
	MaybeRemoveFinalizers(toCleanup []*v1beta1.Ingress) error
}
