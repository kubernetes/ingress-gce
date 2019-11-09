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
package types

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/legacy-cloud-providers/gce"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
)

func TestAggregatedListNetworkEndpointGroup(t *testing.T) {
	t.Parallel()

	const (
		neg1  = "neg1"
		zone1 = "zone1"
		neg2  = "neg2"
		zone2 = "zone2"
	)

	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := NewAdapter(fakeGCE)

	validateAggregatedList(t, fakeCloud, 0, map[string][]string{})

	neg := &composite.NetworkEndpointGroup{Name: neg1, Version: meta.VersionGA}
	zone := zone1
	if err := fakeCloud.CreateNetworkEndpointGroup(neg, zone); err != nil {
		t.Fatalf("Got CreateNetworkEndpointGroup(%v, %v) = %v, want nil", neg, zone, err)
	}

	validateAggregatedList(t, fakeCloud, 1, map[string][]string{zone1: {neg1}})

	neg = &composite.NetworkEndpointGroup{Name: neg2, Version: meta.VersionGA}
	zone = zone2
	if err := fakeCloud.CreateNetworkEndpointGroup(neg, zone); err != nil {
		t.Fatalf("Got CreateNetworkEndpointGroup(%v, %v) = %v, want nil", neg, zone, err)
	}

	validateAggregatedList(t, fakeCloud, 2, map[string][]string{zone1: {neg1}, zone2: {neg2}})

	neg = &composite.NetworkEndpointGroup{Name: neg1, Version: meta.VersionGA}
	zone = zone2
	if err := fakeCloud.CreateNetworkEndpointGroup(neg, zone); err != nil {
		t.Fatalf("Got CreateNetworkEndpointGroup(%v, %v) = %v, want nil", neg, zone, err)
	}

	validateAggregatedList(t, fakeCloud, 2, map[string][]string{zone1: {neg1}, zone2: {neg1, neg2}})

	if err := fakeCloud.DeleteNetworkEndpointGroup(neg1, zone1, meta.VersionGA); err != nil {
		t.Fatalf("Got DeleteNetworkEndpointGroup(%v, %v) = %v, want nil", neg1, zone1, err)
	}

	validateAggregatedList(t, fakeCloud, 1, map[string][]string{zone2: {neg1, neg2}})
}

func validateAggregatedList(t *testing.T, adapter NetworkEndpointGroupCloud, expectZoneNum int, expectZoneNegs map[string][]string) {
	ret, err := adapter.AggregatedListNetworkEndpointGroup(meta.VersionGA)
	if err != nil {
		t.Errorf("Expect AggregatedListNetworkEndpointGroup to return nil error, but got %v", err)
	}
	if len(ret) != expectZoneNum {
		t.Errorf("Expect len(ret) == %v, got %v", expectZoneNum, len(ret))
	}

	zoneNames := sets.NewString()
	expectZoneNames := sets.NewString()
	for key := range expectZoneNegs {
		expectZoneNames.Insert(key)
	}
	for zone, negs := range ret {
		zoneNames.Insert(zone)
		negNames := sets.NewString()
		expectNegNames := sets.NewString()

		for _, neg := range negs {
			negNames.Insert(neg.Name)
		}

		expectNegs, ok := expectZoneNegs[zone]
		if !ok {
			t.Errorf("Zone %v from return is not expected", zone)
			continue
		}

		for _, neg := range expectNegs {
			expectNegNames.Insert(neg)
		}
		if !negNames.Equal(expectNegNames) {
			t.Errorf("Expect NEG names %v, but got %v", expectNegNames.List(), negNames.List())
		}
	}

	if !zoneNames.Equal(expectZoneNames) {
		t.Errorf("Expect zones %v, but got %v", expectZoneNames.List(), zoneNames.List())
	}
}
