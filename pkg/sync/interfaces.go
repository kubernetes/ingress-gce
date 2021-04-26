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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/ingress-gce/pkg/utils"
)

// Syncer is an interface to sync GCP resources associated with an Ingress.
type Syncer interface {
	// Sync creates a full GCLB given some state related to an Ingress.
	Sync(state interface{}) error
	// GC cleans up GCLB resources for all Ingresses and can optionally
	// use some arbitrary to help with the process.
	// GC workflow performs frontend resource deletion based on given gc algorithm.
	// TODO(rramkumar): Do we need to rethink the strategy of GC'ing
	// all Ingresses at once?
	GC(ings []*v1.Ingress, currIng *v1.Ingress, frontendGCAlgorithm utils.FrontendGCAlgorithm, scope meta.KeyType) error
}

// Controller is an interface for ingress controllers and declares methods
// on how to sync the various portions of the GCLB for an Ingress.
type Controller interface {
	// SyncBackends syncs the backends for a GCLB given some existing state.
	SyncBackends(state interface{}) error
	// GCBackends garbage collects backends for all ingresses given a list of ingresses to exclude from GC.
	GCBackends(toKeep []*v1.Ingress) error
	// SyncLoadBalancer syncs the front-end load balancer resources for a GCLB given some existing state.
	SyncLoadBalancer(state interface{}) error
	// GCv1LoadBalancers garbage collects front-end load balancer resources for all ingresses
	// given a list of ingresses with v1 naming policy to exclude from GC.
	GCv1LoadBalancers(toKeep []*v1.Ingress) error
	// GCv2LoadBalancer garbage collects front-end load balancer resources for given ingress
	// with v2 naming policy.
	GCv2LoadBalancer(ing *v1.Ingress, scope meta.KeyType) error
	// PostProcess allows for doing some post-processing after an Ingress is synced to a GCLB.
	PostProcess(state interface{}) error
	// EnsureDeleteV1Finalizers ensures that v1 finalizers are removed for given list of ingresses.
	EnsureDeleteV1Finalizers(toCleanup []*v1.Ingress) error
	// EnsureDeleteV2Finalizer ensures that v2 finalizer is removed for given ingress.
	EnsureDeleteV2Finalizer(ing *v1.Ingress) error
}
