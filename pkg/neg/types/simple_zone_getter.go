/*
Copyright 2020 The Kubernetes Authors.

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

package types

import "k8s.io/ingress-gce/pkg/utils"

// simpleZoneGetter implements ZoneGetter interface
// It always return its one single stored zone
type simpleZoneGetter struct {
	zone string
}

func (s *simpleZoneGetter) GetZoneForNode(string) (string, error) {
	return s.zone, nil
}

func (s *simpleZoneGetter) ListZones(_ utils.NodeConditionPredicate) ([]string, error) {
	return []string{s.zone}, nil
}

func NewSimpleZoneGetter(zone string) ZoneGetter {
	return &simpleZoneGetter{zone: zone}
}
