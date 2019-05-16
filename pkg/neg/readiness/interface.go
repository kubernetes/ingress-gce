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
	"k8s.io/api/core/v1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

// Reflector defines the interaction between readiness reflector and other NEG controller components
type Reflector interface {
	// Run starts the reflector.
	// Closing stopCh will signal the reflector to stop running.
	Run(stopCh <-chan struct{})
	// SyncPod signals the reflector to evaluate pod and patch pod status if needed.
	SyncPod(pod *v1.Pod)
	// CommitPods signals the reflector that pods has been added to and NEG it is time to poll the NEG health status
	// syncerKey is the key to uniquely identify the NEG syncer
	// negName is the name of the network endpoint group (NEG) in the zone (e.g. k8s1-1234567-namespace-name-80-1234567)
	// zone is the corresponding zone of the NEG resource (e.g. us-central1-b)
	// endpointMap contains mapping from all network endpoints to pods which have been added into the NEG
	CommitPods(syncerKey negtypes.NegSyncerKey, negName string, zone string, endpointMap negtypes.EndpointPodMap)
}

// NegLookup defines an interface for looking up pod membership.
type NegLookup interface {
	// ReadinessGateEnabledNegs returns a list of NEGs which has readiness gate enabled for the input pod's namespace and labels.
	ReadinessGateEnabledNegs(namespace string, labels map[string]string) []string
	// ReadinessGateEnabled returns true if the NEG requires readiness feedback
	ReadinessGateEnabled(syncerKey negtypes.NegSyncerKey) bool
}

type NoopReflector struct{}

func (*NoopReflector) Run(<-chan struct{}) {}

func (*NoopReflector) SyncPod(*v1.Pod) {}

func (*NoopReflector) CommitPods(negtypes.NegSyncerKey, string, string, negtypes.EndpointPodMap) {}
