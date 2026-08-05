/*
Copyright 2026 The Kubernetes Authors.

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

// CompositeNegLookup aggregates multiple NegLookup implementations to evaluate NEG pod membership and readiness gate enablement across all registered NEG lookup sources.
type CompositeNegLookup struct {
	lookups []NegLookup
}

// NewCompositeNegLookup returns a new CompositeNegLookup initialized with the provided lookups.
func NewCompositeNegLookup(lookups ...NegLookup) *CompositeNegLookup {
	return &CompositeNegLookup{
		lookups: lookups,
	}
}

// AddLookup appends a NegLookup delegate to the composite lookup.
func (h *CompositeNegLookup) AddLookup(lookup NegLookup) {
	h.lookups = append(h.lookups, lookup)
}

// ReadinessGateEnabledNegs returns the union of NEGs from all underlying lookups.
func (h *CompositeNegLookup) ReadinessGateEnabledNegs(namespace string, labels map[string]string) []string {
	ret := sets.New[string]()
	for _, lookup := range h.lookups {
		ret.Insert(lookup.ReadinessGateEnabledNegs(namespace, labels)...)
	}
	return sets.List(ret)
}

// ReadinessGateEnabled returns true if any underlying lookup enables readiness gate.
func (h *CompositeNegLookup) ReadinessGateEnabled(syncerKey negtypes.NegSyncerKey) bool {
	for _, lookup := range h.lookups {
		if lookup.ReadinessGateEnabled(syncerKey) {
			return true
		}
	}
	return false
}
