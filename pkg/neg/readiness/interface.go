/*
Copyright 2019 The Kubernetes Authors.

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

package readiness

import (
	"k8s.io/apimachinery/pkg/util/sets"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

// podStatusPatcher handles patching Pod status.
type podStatusPatcher interface {
	// Run starts up the patcher.
	Run(stopCh <-chan struct{})
	// Patch patches pod with input patch bytes
	Patch(namespace, name string)
}

// healthStatusPoller handles polling health status for NEGs
type healthStatusPoller interface {
	// Run starts up the patcher.
	Run(stopCh <-chan struct{})
	// Poll signals the poller to poll a neg
	Poll(neg negName)
}

type Reflector interface {
	// Run starts up the readiness reflector
	Run(stopCh <-chan struct{})
	// SyncNegService syncs the services
	SyncNegService(namespace, name string, negNames sets.String)
	// SyncPod registers the pod with reflector
	SyncPod(obj interface{})
	// CommitPod signals the reflector that a pod has been added to one NEG
	CommitPod(neg string, zone string, pods []*negtypes.EndpointMeta)
}

type NoopReflector struct{}

func (*NoopReflector) Run(stopCh <-chan struct{}) {
	return
}

func (*NoopReflector) SyncNegService(namespace, name string, negNames sets.String) {
	return
}

func (*NoopReflector) SyncPod(obj interface{}) {
	return
}

func (*NoopReflector) CommitPod(neg string, zone string, pods []*negtypes.EndpointMeta) {
	return
}
