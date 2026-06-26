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

package shared

import "k8s.io/apimachinery/pkg/util/sets"

const (
	NegReadinessGate = "cloud.google.com/load-balancer-neg-ready"
)

type ZonesPerSubnetMap map[string]sets.Set[string]

func (m ZonesPerSubnetMap) Equal(other ZonesPerSubnetMap) bool {
	if len(m) != len(other) {
		return false
	}

	for subnet, zones := range m {
		zones_other, ok := other[subnet]
		if !ok || !zones.Equal(zones_other) {
			return false
		}
	}
	return true
}
