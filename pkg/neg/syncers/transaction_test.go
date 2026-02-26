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

package syncers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/nodetopology"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	// test zone and instances in the zones
	// TODO - use negtypes.TestZone consts instead.
	testZone1            = "zone1"
	testInstance1        = "instance1"
	testInstance2        = "instance2"
	testZone2            = "zone2"
	testInstance3        = "instance3"
	testInstance4        = "instance4"
	testInstance5        = "instance5"
	testInstance6        = "instance6"
	testUnreadyInstance1 = "unready-instance1"
	testUnreadyInstance2 = "unready-instance2"

	defaultTestSubnet    = "default"
	additionalTestSubnet = "additional-subnet"
	secondaryTestSubnet1 = "secondary1"
	secondaryTestSubnet2 = "secondary2"
)

func TestTransactionSyncNetworkEndpoints(t *testing.T) {
	t.Parallel()

	fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE, negtypes.NewTestContext().NegMetrics)
	testNegTypes := []negtypes.NetworkEndpointType{
		negtypes.VmIpEndpointType,
		negtypes.VmIpPortEndpointType,
	}

	for _, testNegType := range testNegTypes {
		_, transactionSyncer, err := newTestTransactionSyncer(fakeCloud, testNegType, false)
		if err != nil {
			t.Fatalf("failed to initialize transaction syncer: %v", err)
		}
		if err := transactionSyncer.ensureNetworkEndpointGroups(); err != nil {
			t.Errorf("Expect error == nil, but got %v", err)
		}
		var targetPort string
		if testNegType == negtypes.VmIpPortEndpointType {
			targetPort = "8080"
		}

		// Verify the NEGs are created as expected
		ret, _ := transactionSyncer.cloud.AggregatedListNetworkEndpointGroup(transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		// Though the test cases below only add instances in zone1 and zone2, NEGs will be created in zone3 or zone4 as well since fakeZoneGetter includes those zones.
		var expectZones []string
		if testNegType == negtypes.VmIpEndpointType {
			expectZones = []string{negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone3}
		} else {
			expectZones = []string{negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone4}
		}
		retZones := sets.NewString()

		for key := range ret {
			retZones.Insert(key.Zone)
		}
		for _, zone := range expectZones {
			_, ok := retZones[zone]
			if !ok {
				t.Errorf("Failed to find zone %q from ret %v for negType %v", zone, ret, testNegType)
				continue
			}
		}
		for _, neg := range ret {
			if neg.Name != transactionSyncer.NegName {
				t.Errorf("Unexpected neg %q, expected %q", neg.Name, transactionSyncer.NegName)
			}
			if neg.NetworkEndpointType != string(testNegType) {
				t.Errorf("Unexpected neg type %q, expected %q", neg.NetworkEndpointType, testNegType)
			}
			if neg.Description == "" {
				t.Errorf("Neg Description should be populated when NEG CRD is enabled")
			}
		}

		testCases := []struct {
			desc            string
			addEndpoints    map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
			removeEndpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
			expectEndpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		}{
			{
				desc:            "no endpoints to add or remove",
				addEndpoints:    map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			},
			{
				desc: "add some endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				desc:         "remove some endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
				},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				desc: "add duplicate endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				desc: "add and remove endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
				},
			},
			{
				desc: "add more endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
			},
			{
				desc: "add and remove endpoints in both zones",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
		}

		for _, tc := range testCases {
			// TODO(gauravkghildiyal): When the DualStack Migrator is fully
			// implemented, check if we need to cover scenarios where `migrationZone`
			// is not empty.
			err := transactionSyncer.syncNetworkEndpoints(tc.addEndpoints, tc.removeEndpoints, labels.EndpointPodLabelMap{}, negtypes.NEGLocation{})
			if err != nil {
				t.Errorf("For case %q, syncNetworkEndpoints() got %v, want nil", tc.desc, err)
			}

			if err := waitForTransactions(transactionSyncer); err != nil {
				t.Errorf("For case %q, waitForTransactions() got %v, want nil", tc.desc, err)
			}

			for negLocation, endpoints := range tc.expectEndpoints {
				list, err := fakeCloud.ListNetworkEndpoints(transactionSyncer.NegSyncerKey.NegName, negLocation.Zone, false, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
				if err != nil {
					t.Errorf("For case %q, ListNetworkEndpoints() got %v, want nil", tc.desc, err)
				}

				endpointSet := negtypes.NewNetworkEndpointSet()
				for _, ep := range list {
					tmp := negtypes.NetworkEndpoint{IP: ep.NetworkEndpoint.IpAddress, Node: ep.NetworkEndpoint.Instance}
					if testNegType == negtypes.VmIpPortEndpointType {
						tmp.Port = strconv.FormatInt(ep.NetworkEndpoint.Port, 10)
					}
					endpointSet.Insert(tmp)
				}

				if !endpoints.Equal(endpointSet) {
					t.Errorf("For case %q, in zone %q, negType %q, endpointSets endpoints == %v, but got %v, difference: \n(want - got) = %v\n(got - want) = %v", tc.desc, negLocation.Zone, testNegType, endpoints, endpointSet, endpoints.Difference(endpointSet), endpointSet.Difference(endpoints))
				}
			}
		}
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone1, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone2, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone3, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone4, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
	}
}

func TestTransactionSyncNetworkEndpointsMSC(t *testing.T) {
	vals := gce.DefaultTestClusterValues()
	vals.SubnetworkURL = defaultTestSubnetURL
	fakeGCE := gce.NewFakeGCECloud(vals)
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE, negtypes.NewTestContext().NegMetrics)
	testNegTypes := []negtypes.NetworkEndpointType{
		negtypes.VmIpEndpointType,
		negtypes.VmIpPortEndpointType,
	}

	prevFlag := flags.F.EnableMultiSubnetClusterPhase1
	currNodeTopologyCRName := flags.F.NodeTopologyCRName
	defer func() {
		flags.F.EnableMultiSubnetClusterPhase1 = prevFlag
		flags.F.NodeTopologyCRName = currNodeTopologyCRName
	}()
	flags.F.EnableMultiSubnetClusterPhase1 = true
	flags.F.NodeTopologyCRName = "default"

	nodeTopologyCrWithAdditionalSubnets := nodetopologyv1.NodeTopology{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NodeTopology",
			APIVersion: "networking.gke.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Status: nodetopologyv1.NodeTopologyStatus{
			Subnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultTestSubnet, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", defaultTestSubnet)},
				{Name: additionalTestSubnet, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", additionalTestSubnet)},
			},
		},
	}

	for _, testNegType := range testNegTypes {
		_, transactionSyncer, err := newTestTransactionSyncer(fakeCloud, testNegType, false)
		if err != nil {
			t.Fatalf("failed to initialize transaction syncer: %v", err)
		}
		if err := zonegetter.AddNodeTopologyCR(transactionSyncer.zoneGetter, &nodeTopologyCrWithAdditionalSubnets); err != nil {
			t.Fatalf("Failed to add node topology CR: %v", err)
		}
		zonegetter.SetNodeTopologyHasSynced(transactionSyncer.zoneGetter, func() bool { return true })
		nonDefaultNegName, err := transactionSyncer.getNonDefaultSubnetNEGName(additionalTestSubnet)
		if err != nil {
			t.Fatalf("Failed to get non-default subnet NEG name: %v", err)
		}

		if err := transactionSyncer.ensureNetworkEndpointGroups(); err != nil {
			t.Errorf("Expect error == nil, but got %v", err)
		}
		var targetPort string
		if testNegType == negtypes.VmIpPortEndpointType {
			targetPort = "8080"
		}

		// Verify the NEGs are created as expected
		ret, _ := transactionSyncer.cloud.AggregatedListNetworkEndpointGroup(transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		// Though the test cases below only add instances in zone1 and zone2, NEGs will be created in zone3 or zone4 as well since fakeZoneGetter includes those zones.
		var expectZones []string
		if testNegType == negtypes.VmIpEndpointType {
			expectZones = []string{negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone3}
		} else {
			expectZones = []string{negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone4}
		}
		retZones := sets.NewString()

		for key := range ret {
			retZones.Insert(key.Zone)
		}
		for _, zone := range expectZones {
			_, ok := retZones[zone]
			if !ok {
				t.Errorf("Failed to find zone %q from ret %v for negType %v", zone, ret, testNegType)
				continue
			}
		}
		for _, neg := range ret {
			if neg.NetworkEndpointType != string(testNegType) {
				t.Errorf("Unexpected neg type %q for neg %q, expected %q", neg.NetworkEndpointType, neg.Name, testNegType)
			}
			if neg.Description == "" {
				t.Errorf("Neg Description should be populated when NEG CRD is enabled")
			}
		}

		testCases := []struct {
			desc            string
			addEndpoints    map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
			removeEndpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
			expectEndpoints map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		}{
			{
				desc:            "no endpoints to add or remove",
				addEndpoints:    map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			},
			{
				desc: "add some endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				desc:         "remove some endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
				},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				desc: "add duplicate endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				desc: "add and remove endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
				},
			},
			{
				desc: "add more endpoints",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
			},
			{
				desc: "add and remove endpoints in both zones",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				desc: "add endpoints in non-default subnets",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.2.1.1"), 10, testInstance5, targetPort)),
					{Zone: testZone2, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.2.2.1"), 10, testInstance6, targetPort)),
				},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
					{Zone: testZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.2.1.1"), 10, testInstance5, targetPort)),
					{Zone: testZone2, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.2.2.1"), 10, testInstance6, targetPort)),
				},
			},
			{
				desc:         "remove endpoints in non-default subnets",
				addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
				removeEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.2.1.1"), 10, testInstance5, targetPort)),
					{Zone: testZone2, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.2.2.1"), 10, testInstance6, targetPort)),
				},
				expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
		}

		for _, tc := range testCases {
			// TODO(gauravkghildiyal): When the DualStack Migrator is fully
			// implemented, check if we need to cover scenarios where `migrationZone`
			// is not empty.
			err := transactionSyncer.syncNetworkEndpoints(tc.addEndpoints, tc.removeEndpoints, labels.EndpointPodLabelMap{}, negtypes.NEGLocation{})
			if err != nil {
				t.Errorf("For case %q, syncNetworkEndpoints() got %v, want nil", tc.desc, err)
			}

			if err := waitForTransactions(transactionSyncer); err != nil {
				t.Errorf("For case %q, waitForTransactions() got %v, want nil", tc.desc, err)
			}

			for negLocation, endpoints := range tc.expectEndpoints {
				negName := transactionSyncer.NegSyncerKey.NegName
				if negLocation.Subnet != defaultTestSubnet {
					negName = nonDefaultNegName
				}
				list, err := fakeCloud.ListNetworkEndpoints(negName, negLocation.Zone, false, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
				if err != nil {
					t.Errorf("For case %q, ListNetworkEndpoints() got %v, want nil", tc.desc, err)
				}

				endpointSet := negtypes.NewNetworkEndpointSet()
				for _, ep := range list {
					tmp := negtypes.NetworkEndpoint{IP: ep.NetworkEndpoint.IpAddress, Node: ep.NetworkEndpoint.Instance}
					if testNegType == negtypes.VmIpPortEndpointType {
						tmp.Port = strconv.FormatInt(ep.NetworkEndpoint.Port, 10)
					}
					endpointSet.Insert(tmp)
				}

				if !endpoints.Equal(endpointSet) {
					t.Errorf("For case %q, in zone %q, negType %q, endpointSets endpoints == %v, but got %v, difference: \n(want - got) = %v\n(got - want) = %v", tc.desc, negLocation.Zone, testNegType, endpoints, endpointSet, endpoints.Difference(endpointSet), endpointSet.Difference(endpoints))
				}
			}
		}
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone1, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone2, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone3, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone4, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(nonDefaultNegName, negtypes.TestZone1, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(nonDefaultNegName, negtypes.TestZone2, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(nonDefaultNegName, negtypes.TestZone3, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(nonDefaultNegName, negtypes.TestZone4, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
	}
}

func TestNegNameMultiNetworking(t *testing.T) {
	prevFlag := flags.F.EnableMultiSubnetClusterPhase1
	defer func() { flags.F.EnableMultiSubnetClusterPhase1 = prevFlag }()
	flags.F.EnableMultiSubnetClusterPhase1 = true

	fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE, negtypes.NewTestContext().NegMetrics)
	subnetInDefaultNetwork := fakeCloud.SubnetworkURL()
	secondaryNetwork := "projects/mock-project/global/networks/multi-net-secondary-network"
	subnetInSecondaryNetwork := "projects/mock-project/regions/test-region/subnetworks/multi-net-secondary-subnet"
	netInfo := network.NetworkInfo{IsDefault: false, NetworkURL: secondaryNetwork, SubnetworkURL: subnetInSecondaryNetwork}

	_, transactionSyncer, err := newTestTransactionSyncerWithNetInfo(fakeCloud, negtypes.VmIpEndpointType, false, netInfo)
	if err != nil {
		t.Fatalf("failed to initialize transaction syncer: %v", err)
	}

	// Start syncer without starting syncer goroutine
	(transactionSyncer.syncer.(*syncer)).stopped = false
	if err := transactionSyncer.ensureNetworkEndpointGroups(); err != nil {
		t.Errorf("Expect error == nil, but got %v", err)
	}

	// Verify the NEGs are created as expected
	ret, _ := transactionSyncer.cloud.AggregatedListNetworkEndpointGroup(transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
	// Though the test cases below only add instances in zone1 and zone2, NEGs will be created in zone3 or zone4 as well since fakeZoneGetter includes those zones.
	expectZones := []string{negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone3}
	retZones := sets.NewString()

	for key := range ret {
		retZones.Insert(key.Zone)
	}
	for _, zone := range expectZones {
		_, ok := retZones[zone]
		if !ok {
			t.Errorf("Failed to find zone %q from ret %v for negType %v", zone, ret, negtypes.VmIpEndpointType)
			continue
		}
	}

	err = transactionSyncer.syncInternal()
	if err != nil {
		t.Errorf("unexpected error when syncing: %s", err)
	}

	for _, neg := range ret {
		if neg.Name != transactionSyncer.NegName {
			t.Errorf("Unexpected neg %q, expected %q", neg.Name, transactionSyncer.NegName)
		}
		if neg.NetworkEndpointType != string(negtypes.VmIpEndpointType) {
			t.Errorf("Unexpected neg type %q, expected %q", neg.NetworkEndpointType, negtypes.VmIpEndpointType)
		}
		if neg.Description == "" {
			t.Errorf("Neg Description should be populated when NEG CRD is enabled")
		}
		if neg.Subnetwork != subnetInSecondaryNetwork {
			t.Errorf("Neg subnetwork URL is incorrect. Got %s, Expected %s", neg.Subnetwork, subnetInSecondaryNetwork)
		}
	}

	// Ensure that endpoints get correctly added to the NEGs in the secondary
	// VPC under multi-networking scenario.
	wantEndpointsCount := 3
	addEndpoints := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: subnetInDefaultNetwork}: generateEndpointSet(net.ParseIP("1.1.1.1"), wantEndpointsCount, "instance-name", "8080"),
	}
	err = transactionSyncer.syncNetworkEndpoints(addEndpoints, nil, nil, negtypes.NEGLocation{})
	if err != nil {
		t.Errorf("syncNetworkEndpoints(...) returned unexpected error: %v", err)
	}
	if err := waitForTransactions(transactionSyncer); err != nil {
		t.Fatalf("Errored while waiting for the syncNetworkEndpoint transactions to complete: %v", err)
	}
	gotEndpoints, err := transactionSyncer.cloud.ListNetworkEndpoints(transactionSyncer.NegName, negtypes.TestZone1, false, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
	if err != nil {
		t.Fatalf("transactionSyncer.cloud.ListNetworkEndpoints(%v, %v, ...) returned unexpected error: %v", transactionSyncer.NegName, negtypes.TestZone1, err)
	}
	if len(gotEndpoints) != wantEndpointsCount {
		t.Errorf("NEG %q in zone %q has %v endpoints; want %v endpionts", transactionSyncer.NegName, negtypes.TestZone1, len(gotEndpoints), wantEndpointsCount)
	}
}

func TestSyncNetworkEndpointLabel(t *testing.T) {

	var (
		l7EndpointSet1 = generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
		l7EndpointSet2 = generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
		l7EndpointSet3 = generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
		l7EndpointSet4 = generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
		l4EndpointSet1 = generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "")
		l4EndpointSet2 = generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "")
		l4EndpointSet3 = generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "")
		l4EndpointSet4 = generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "")
	)

	oldFlag := flags.F.EnableNEGLabelPropagation
	defer func() { flags.F.EnableNEGLabelPropagation = oldFlag }()

	testCases := []struct {
		desc                    string
		labelPropagationEnabled bool
		negType                 negtypes.NetworkEndpointType
		addEndpoints            map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		endpointPodLabelMap     labels.EndpointPodLabelMap
		expectedNEAnnotation    labels.PodLabelMap
	}{
		{
			"empty input",
			true,
			negtypes.VmIpPortEndpointType,
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			labels.EndpointPodLabelMap{},
			nil,
		},
		{
			"add L4 endpoints with label map populated",
			true,
			negtypes.VmIpEndpointType,
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet1).Union(l4EndpointSet2),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet3).Union(l4EndpointSet4),
			},
			generateEndpointPodLabelMap(
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet1).Union(l4EndpointSet2),
					testZone2: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet3).Union(l4EndpointSet4),
				},
				labels.PodLabelMap{
					"label1": "value1",
					"label2": "value2",
				},
			),
			nil,
		},
		{
			"add L4 endpoints with empty label map",
			true,
			negtypes.VmIpEndpointType,
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet1).Union(l4EndpointSet2),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet3).Union(l4EndpointSet4),
			},
			generateEndpointPodLabelMap(
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet1).Union(l4EndpointSet2),
					testZone2: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet3).Union(l4EndpointSet4),
				},
				labels.PodLabelMap{},
			),
			nil,
		},
		{
			"add L7 endpoints label map populated",
			true,
			negtypes.VmIpPortEndpointType,
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
			},
			generateEndpointPodLabelMap(
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
					testZone2: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
				},
				labels.PodLabelMap{
					"label1": "value1",
					"label2": "value2",
				},
			),
			labels.PodLabelMap{
				"label1": "value1",
				"label2": "value2",
			},
		},
		{
			"add L7 endpoints with empty label map",
			true,
			negtypes.VmIpPortEndpointType,
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
			},
			generateEndpointPodLabelMap(
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
					testZone2: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
				},
				labels.PodLabelMap{},
			),
			nil,
		},
		{
			"add L7 endpoints label map populated, but EnableNEGLabelPropagation flag disabled",
			false,
			negtypes.VmIpPortEndpointType,
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
			},
			generateEndpointPodLabelMap(
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
					testZone2: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
				},
				labels.PodLabelMap{
					"label1": "value1",
					"label2": "value2",
				},
			),
			nil,
		},
	}

	for _, tc := range testCases {
		flags.F.EnableNEGLabelPropagation = tc.labelPropagationEnabled
		vals := gce.DefaultTestClusterValues()
		vals.SubnetworkURL = defaultTestSubnetURL
		fakeGCE := gce.NewFakeGCECloud(vals)
		negtypes.MockNetworkEndpointAPIs(fakeGCE)
		fakeCloud := negtypes.NewAdapter(fakeGCE, negtypes.NewTestContext().NegMetrics)
		_, transactionSyncer, err := newTestTransactionSyncer(fakeCloud, tc.negType, false)
		if err != nil {
			t.Fatalf("failed to initialize transaction syncer: %v", err)
		}
		if err := transactionSyncer.ensureNetworkEndpointGroups(); err != nil {
			t.Errorf("Expect error == nil, but got %v", err)
		}
		err = transactionSyncer.syncNetworkEndpoints(tc.addEndpoints, map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{}, tc.endpointPodLabelMap, negtypes.NEGLocation{})
		if err != nil {
			t.Errorf("For case %q, syncNetworkEndpoints() got %v, want nil", tc.desc, err)
		}
		if err := waitForTransactions(transactionSyncer); err != nil {
			t.Errorf("For case %q, waitForTransactions() got %v, want nil", tc.desc, err)
		}

		for negLocation := range tc.addEndpoints {
			list, err := fakeCloud.ListNetworkEndpoints(transactionSyncer.NegSyncerKey.NegName, negLocation.Zone, false, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
			if err != nil {
				t.Errorf("For case %q, ListNetworkEndpoints() got %v, want nil", tc.desc, err)
			}
			for _, ep := range list {
				if fmt.Sprint(ep.NetworkEndpoint.Annotations) != fmt.Sprint(tc.expectedNEAnnotation) {
					t.Errorf("For case %s, endpoint annotation got %v, want %v", tc.desc, ep.NetworkEndpoint.Annotations, tc.expectedNEAnnotation)
				}
			}
		}
	}

}

func TestCommitTransaction(t *testing.T) {
	t.Parallel()
	vals := gce.DefaultTestClusterValues()
	vals.SubnetworkURL = defaultTestSubnetURL
	s, transactionSyncer, err := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(vals), negtypes.NewTestContext().NegMetrics), negtypes.VmIpPortEndpointType, false)
	if err != nil {
		t.Fatalf("failed to initialize transaction syncer: %v", err)
	}
	// use testSyncer to track the number of Sync got triggered
	testSyncer := &testSyncer{s.(*syncer), 0}
	testRetryer := &testRetryHandler{testSyncer, 0}
	transactionSyncer.syncer = testSyncer
	// assume NEG is initialized
	transactionSyncer.needInit = false
	transactionSyncer.retry = testRetryer

	testCases := []struct {
		desc             string
		err              error
		endpointMap      map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint
		table            func() networkEndpointTransactionTable
		expect           func() networkEndpointTransactionTable
		expectSyncCount  int
		expectRetryCount int
		expectNeedInit   bool
		operation        transactionOp
	}{
		{
			"empty inputs",
			nil,
			map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			1,
			0,
			false,
			attachOp,
		},
		{
			"attach 10 endpoints on 1 instance successfully",
			nil,
			generateEndpointBatch(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080"), labels.EndpointPodLabelMap{}),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			2,
			0,
			false,
			attachOp,
		},
		{
			"detach 20 endpoints on 2 instances successfully",
			nil,
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")), labels.EndpointPodLabelMap{}),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			3,
			0,
			false,
			detachOp,
		},
		{
			"attach 20 endpoints on 2 instances successfully with unrelated 10 entries in the transaction table",
			nil,
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")), labels.EndpointPodLabelMap{}),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			4,
			0,
			false,
			attachOp,
		},
		{
			"error and retry",
			fmt.Errorf("dummy error"),
			map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint{},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			5,
			1,
			true,
			attachOp,
		},
		{
			"error and retry #2",
			fmt.Errorf("dummy error"),
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")), labels.EndpointPodLabelMap{}),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			6,
			2,
			true,
			attachOp,
		},
		{
			"detach 20 endpoints on 2 instance but missing transaction entries on 1 instance",
			nil,
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")), labels.EndpointPodLabelMap{}),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			7,
			2,
			false,
			detachOp,
		},
		{
			"detach 20 endpoints on 2 instance but 10 endpoints needs reconcile",
			nil,
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")), labels.EndpointPodLabelMap{}),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			8,
			2,
			false,
			detachOp,
		},
	}

	for _, tc := range testCases {
		transactionSyncer.transactions = tc.table()
		transactionSyncer.commitTransaction(tc.operation, tc.err, tc.endpointMap)
		if transactionSyncer.needInit != tc.expectNeedInit {
			t.Errorf("For case %q, endpointSets needInit == %v, but got %v", tc.desc, tc.expectNeedInit, transactionSyncer.needInit)
		}
		if transactionSyncer.needInit == true {
			transactionSyncer.needInit = false
		}

		validateTransactionTableEquality(t, tc.desc, transactionSyncer.transactions, tc.expect())
		// wait for the sync count to bump
		if err := wait.PollImmediate(time.Microsecond, 5*time.Second, func() (bool, error) {
			if tc.expectSyncCount == testSyncer.SyncCount && tc.expectRetryCount == testRetryer.RetryCount {
				return true, nil
			}
			return false, nil
		}); err != nil {
			t.Errorf("For case %q, endpointSets sync count == %v, but got %v", tc.desc, tc.expectSyncCount, testSyncer.SyncCount)
			t.Errorf("For case %q, endpointSets retry count == %v, but got %v", tc.desc, tc.expectRetryCount, testRetryer.RetryCount)
		}

	}
}

func TestMergeTransactionIntoZoneEndpointMap(t *testing.T) {
	testCases := []struct {
		desc              string
		endpointMap       map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		table             func() networkEndpointTransactionTable
		expectEndpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	}{
		{
			"empty map and transactions",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		{
			"empty transactions",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"empty map",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone2,
				}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add existing endpoints",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone2,
				}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add non-existing endpoints",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone2,
				}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")),
			},
		},
		{
			"remove existing endpoints",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add non-existing endpoints and remove existing endpoints",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
	}

	for _, tc := range testCases {
		mergeTransactionIntoZoneEndpointMap(tc.endpointMap, tc.table(), klog.TODO())
		if !reflect.DeepEqual(tc.endpointMap, tc.expectEndpointMap) {
			t.Errorf("For test case %q, endpointSets endpoint map to be %+v, but got %+v", tc.desc, tc.expectEndpointMap, tc.endpointMap)
		}
	}
}

func TestFilterEndpointByTransaction(t *testing.T) {
	testCases := []struct {
		desc              string
		endpointMap       map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		table             func() networkEndpointTransactionTable
		expectEndpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	}{
		{
			"both empty",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		{
			"empty map",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		{
			"empty transaction",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"empty transaction",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
	}

	for _, tc := range testCases {
		input := tc.endpointMap
		filterEndpointByTransaction(input, tc.table(), klog.TODO())
		if !reflect.DeepEqual(tc.endpointMap, tc.expectEndpointMap) {
			t.Errorf("For test case %q, endpointSets endpoint map to be %+v, but got %+v", tc.desc, tc.expectEndpointMap, tc.endpointMap)
		}
	}
}

func TestFilterEndpointByTransactionExclDetach(t *testing.T) {
	testCases := []struct {
		desc              string
		endpointMap       map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		table             func() networkEndpointTransactionTable
		expectEndpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	}{
		{
			"both empty",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		{
			"empty map",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation: attachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		{
			"empty transaction",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"do not filter detaches",
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 2, testInstance1, "8080")),
				{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 2, testInstance2, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: detachOp,
					Zone:      testZone1,
				}, net.ParseIP("1.1.1.1"), 2, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation: attachOp,
					Zone:      testZone2,
				}, net.ParseIP("1.1.3.1"), 2, testInstance2, "8080")
				return table
			},
			map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 2, testInstance1, "8080")),
			},
		},
	}

	for _, tc := range testCases {
		input := tc.endpointMap
		filterEndpointByTransactionExclDetach(input, tc.table(), klog.TODO())
		if !reflect.DeepEqual(tc.endpointMap, tc.expectEndpointMap) {
			t.Errorf("For test case %q, endpointSets endpoint map to be %+v, but got %+v", tc.desc, tc.expectEndpointMap, tc.endpointMap)
		}
	}
}

func TestCommitPods(t *testing.T) {
	vals := gce.DefaultTestClusterValues()
	vals.SubnetworkURL = defaultTestSubnetURL
	_, transactionSyncer, err := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(vals), negtypes.NewTestContext().NegMetrics), negtypes.VmIpPortEndpointType, false)
	if err != nil {
		t.Fatalf("failed to initialize transaction syncer: %v", err)
	}
	reflector := &testReflector{}
	transactionSyncer.reflector = reflector

	syncerKey := transactionSyncer.NegSyncerKey
	negName := transactionSyncer.NegName
	prevFlag := flags.F.EnableMultiSubnetClusterPhase1
	defer func() { flags.F.EnableMultiSubnetClusterPhase1 = prevFlag }()

	for _, enableMultiSubnetPhase1 := range []bool{true, false} {
		flags.F.EnableMultiSubnetClusterPhase1 = enableMultiSubnetPhase1
		for _, tc := range []struct {
			desc         string
			input        func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap)
			expectOutput func() map[negMeta]negtypes.EndpointPodMap
		}{
			{
				desc: "empty input",
				input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
					return nil, nil
				},
				expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
					return map[negMeta]negtypes.EndpointPodMap{}
				},
			},
			{
				desc: "10 endpoints from 1 instance in 1 zone",
				input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
					endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					return map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{{Zone: testZone1, Subnet: defaultTestSubnet}: endpointSet}, endpointMap
				},
				expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
					_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					return map[negMeta]negtypes.EndpointPodMap{
						{SyncerKey: syncerKey, Name: negName, Zone: testZone1}: endpointMap,
					}
				},
			},
			{
				desc: "40 endpoints from 4 instances in 2 zone",
				input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
					retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
						{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
						{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
					}
					retMap := negtypes.EndpointPodMap{}
					endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					retMap = unionEndpointMap(retMap, endpointMap)
					endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					retMap = unionEndpointMap(retMap, endpointMap)
					endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					retMap = unionEndpointMap(retMap, endpointMap)
					endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					retMap = unionEndpointMap(retMap, endpointMap)
					return retSet, retMap
				},
				expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
					retMap := map[negMeta]negtypes.EndpointPodMap{
						{SyncerKey: syncerKey, Name: negName, Zone: testZone1}: {},
						{SyncerKey: syncerKey, Name: negName, Zone: testZone2}: {},
					}
					_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}], endpointMap)
					return retMap
				},
			},
			{
				desc: "40 endpoints from 4 instances in 2 zone, but half of the endpoints does not have corresponding pod mapping",
				input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
					retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
						{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
						{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
					}
					retMap := negtypes.EndpointPodMap{}

					endpointSet, _ := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)

					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)

					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)

					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 5, testInstance4, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)
					return retSet, retMap
				},
				expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
					retMap := map[negMeta]negtypes.EndpointPodMap{
						{SyncerKey: syncerKey, Name: negName, Zone: testZone1}: {},
						{SyncerKey: syncerKey, Name: negName, Zone: testZone2}: {},
					}
					_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 5, testInstance4, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}], endpointMap)
					return retMap
				},
			},
			{
				desc: "40 endpoints from 4 instances in 2 zone, and more endpoints are in pod mapping",
				input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
					retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
						{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
						{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
					}
					retMap := negtypes.EndpointPodMap{}

					endpointSet, _ := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 15, testInstance1, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)

					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 15, testInstance2, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)

					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 15, testInstance3, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)

					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 15, testInstance4, "8080")
					retMap = unionEndpointMap(retMap, endpointMap)
					return retSet, retMap
				},
				expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
					retMap := map[negMeta]negtypes.EndpointPodMap{
						{SyncerKey: syncerKey, Name: negName, Zone: testZone1}: {},
						{SyncerKey: syncerKey, Name: negName, Zone: testZone2}: {},
					}
					_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}], endpointMap)
					return retMap
				},
			},
			{
				desc: "40 endpoints from 4 instances in 2 zone, but some nodes do not have endpoint pod mapping",
				input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
					retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
						{Zone: testZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
						{Zone: testZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
					}
					retMap := negtypes.EndpointPodMap{}
					endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					retMap = unionEndpointMap(retMap, endpointMap)
					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
					endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					retMap = unionEndpointMap(retMap, endpointMap)
					endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
					retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
					return retSet, retMap
				},
				expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
					retMap := map[negMeta]negtypes.EndpointPodMap{
						{SyncerKey: syncerKey, Name: negName, Zone: testZone1}: {},
						{SyncerKey: syncerKey, Name: negName, Zone: testZone2}: {},
					}
					_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone1}], endpointMap)
					_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
					retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: syncerKey, Name: negName, Zone: testZone2}], endpointMap)
					return retMap
				},
			},
		} {
			reflector.Flush()
			endpointMap, endpointPodMap := tc.input()
			expectOutput := tc.expectOutput()
			transactionSyncer.commitPods(endpointMap, endpointPodMap)
			negNameSet := sets.NewString(reflector.negNames...)
			if len(expectOutput) != 0 && !(negNameSet.Len() == 1 && negNameSet.Has(transactionSyncer.NegSyncerKey.NegName)) {
				t.Errorf("For test case %q, expect neg name to be %v, but got %v", tc.desc, transactionSyncer.NegSyncerKey.NegName, negNameSet.List())
			}

			if !reflect.DeepEqual(expectOutput, reflector.pollMap) {
				t.Errorf("For test case %q, expect endpoint map to be %v, but got %v", tc.desc, expectOutput, reflector.pollMap)
			}
		}
	}
}

func TestCommitPodsMSC(t *testing.T) {
	vals := gce.DefaultTestClusterValues()
	vals.SubnetworkURL = defaultTestSubnetURL
	_, transactionSyncer, err := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(vals), negtypes.NewTestContext().NegMetrics), negtypes.VmIpPortEndpointType, false)
	if err != nil {
		t.Fatalf("failed to initialize transaction syncer: %v", err)
	}
	reflector := &testReflector{}
	transactionSyncer.reflector = reflector

	prevFlag := flags.F.EnableMultiSubnetClusterPhase1
	defer func() { flags.F.EnableMultiSubnetClusterPhase1 = prevFlag }()
	flags.F.EnableMultiSubnetClusterPhase1 = true

	defaultSubnetNegName := transactionSyncer.NegName
	defaultSubnetSyncerKey := transactionSyncer.NegSyncerKey

	nonDefaultSubnetNegName := transactionSyncer.namer.NonDefaultSubnetNEG(transactionSyncer.NegSyncerKey.Namespace, transactionSyncer.NegSyncerKey.Name, additionalTestSubnet, transactionSyncer.NegSyncerKey.PortTuple.Port)
	nonDefaultSubnetSyncerKey := transactionSyncer.NegSyncerKey
	nonDefaultSubnetSyncerKey.NegName = nonDefaultSubnetNegName

	for _, tc := range []struct {
		desc         string
		input        func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap)
		expectOutput func() map[negMeta]negtypes.EndpointPodMap
	}{
		{
			desc: "20 endpoints from 2 instance in different subnets, in 1 zone",
			input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
					{Zone: testZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
				}
				retMap := negtypes.EndpointPodMap{}
				endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: additionalTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: additionalTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				return retSet, retMap
			},
			expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
				retMap := map[negMeta]negtypes.EndpointPodMap{
					{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}:       {},
					{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}: {},
				}
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}], endpointMap)
				return retMap
			},
		},
		{
			desc: "40 endpoints from 4 instance in different subnets, in 2 zones",
			input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
					{Zone: testZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
					{Zone: testZone2, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
					{Zone: testZone2, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
				}
				retMap := negtypes.EndpointPodMap{}
				endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: additionalTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: additionalTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: additionalTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: additionalTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				return retSet, retMap
			},
			expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
				retMap := map[negMeta]negtypes.EndpointPodMap{
					{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}:       {},
					{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}: {},
					{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone2}:       {},
					{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone2}: {},
				}
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone2}], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone2}], endpointMap)
				return retMap
			},
		},
		{
			desc: "40 endpoints from 4 instance in different subnets, in 2 zones",
			input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
					{Zone: testZone1, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
					{Zone: testZone2, Subnet: defaultTestSubnet}:    negtypes.NewNetworkEndpointSet(),
					{Zone: testZone2, Subnet: additionalTestSubnet}: negtypes.NewNetworkEndpointSet(),
				}
				retMap := negtypes.EndpointPodMap{}
				endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: defaultTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: additionalTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone1, Subnet: additionalTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: defaultTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: additionalTestSubnet}] = retSet[negtypes.NEGLocation{Zone: testZone2, Subnet: additionalTestSubnet}].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				return retSet, retMap
			},
			expectOutput: func() map[negMeta]negtypes.EndpointPodMap {
				retMap := map[negMeta]negtypes.EndpointPodMap{
					{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}:       {},
					{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}: {},
					{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone2}:       {},
					{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone2}: {},
				}
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone1}], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}] = unionEndpointMap(retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone1}], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: defaultSubnetSyncerKey, Name: defaultSubnetNegName, Zone: testZone2}], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone2}] = unionEndpointMap(retMap[negMeta{SyncerKey: nonDefaultSubnetSyncerKey, Name: nonDefaultSubnetNegName, Zone: testZone2}], endpointMap)
				return retMap
			},
		},
	} {
		reflector.Flush()
		endpointMap, endpointPodMap := tc.input()
		expectOutput := tc.expectOutput()
		transactionSyncer.commitPods(endpointMap, endpointPodMap)
		negNameSet := sets.NewString(reflector.negNames...)
		if len(expectOutput) != 0 && negNameSet.Len() != 2 {
			t.Errorf("For test case %q, expect two negs, but got %v", tc.desc, negNameSet.List())
		}

		if diff := cmp.Diff(expectOutput, reflector.pollMap); diff != "" {
			t.Errorf("For test case %q, expect endpoint map to be %v, but got %v, diff = %q", tc.desc, expectOutput, reflector.pollMap, diff)
		}
	}
}

func TestTransactionSyncerWithNegCR(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testSubnetwork := defaultTestSubnetURL

	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
	testNegType := negtypes.VmIpPortEndpointType

	testCases := []struct {
		desc      string
		negExists bool
		negDesc   string
		// crStatusPopulated indicates if the NEG CR in this cluster has NEG
		// status populated before we call ensureNetworkEndpointGroups().
		// This is part of the test setup instead of an expectation on the test
		// result.
		// The fields populated are NEG Initialized and Sync condition, and
		// the list of NEG references.
		crStatusPopulated bool
		customName        bool
		expectErr         bool

		// expectNoopOnNegStatus indicates if the NEG controller should do
		// no-op on NEG CR Status.
		// This occurs when the NEG controller/syncer doesn't own this NEG.
		// Current, there are two kinds of situation:
		// 1. When we detect a conflict on NEG description within the same
		//    cluster in the same namespace. This implies the CR is owned by a
		//    different syncer.
		// 2. Custom named NEG without NEG description. In this case, customers
		//    may have created this NEG outside of the controller or through
		//    some other integration. GKE managed custom named NEG would never
		//    have no description, so this is another case where our Controller
		//    might not be the one owning this NEG.
		//
		// In term of test, we should expect the NEG Status stays the same.
		// There should be no change in the NEG status condition and the number
		// of NEG references in NegObjRef.
		expectNoopOnNegStatus bool
	}{
		{
			desc:              "Neg does not exist",
			negExists:         false,
			negDesc:           "",
			crStatusPopulated: false,
			expectErr:         false,
		},
		{
			desc:              "Neg exists, cr has populated status, without neg description",
			negExists:         true,
			negDesc:           "",
			crStatusPopulated: true,
			expectErr:         false,
		},
		{
			desc:                  "Neg exists, custom name, without neg description",
			negExists:             true,
			negDesc:               "",
			crStatusPopulated:     false,
			customName:            true,
			expectErr:             true,
			expectNoopOnNegStatus: false,
		},
		{
			desc:      "Neg exists, cr has with populated status, with correct neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "80",
			}.String(),
			crStatusPopulated: true,
			expectErr:         false,
		},
		{
			desc:              "Neg exists, without neg description",
			negExists:         true,
			negDesc:           "",
			crStatusPopulated: false,
			expectErr:         false,
		},
		{
			desc:      "Neg exists, with correct neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "80",
			}.String(),
			crStatusPopulated: false,
			expectErr:         false,
		},
		{
			desc:      "Neg exists, with mismatched cluster id in neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  "cluster-2",
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "80",
			}.String(),
			crStatusPopulated: false,
			expectErr:         true,
		},
		{
			desc:      "Neg exists, with mismatched namespace in neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   "namespace-2",
				ServiceName: testServiceName,
				Port:        "80",
			}.String(),
			crStatusPopulated: false,
			expectErr:         true,
		},
		{
			desc:      "Neg exists, with mismatched service in neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: "service-2",
				Port:        "80",
			}.String(),
			// This indicate a different syncer is owning the CR, and has already populated NEG CR Status with valid content.
			crStatusPopulated:     true,
			expectErr:             true,
			expectNoopOnNegStatus: true,
		},
		{
			desc:      "Neg exists, with mismatched port in neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "81",
			}.String(),
			// This indicate a different syncer is owning the CR, and has already populated NEG CR Status with valid content.
			crStatusPopulated:     true,
			expectErr:             true,
			expectNoopOnNegStatus: true,
		},
		{
			desc:      "Neg exists, cr has populated status, but error during initialization",
			negExists: true,
			// Cause error by having a conflicting neg description
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "81", // Expected port to be 80
			}.String(),
			crStatusPopulated:     true,
			expectErr:             true,
			expectNoopOnNegStatus: true,
		},
	}

	for _, tc := range testCases {
		_, syncer, err := newTestTransactionSyncer(fakeCloud, testNegType, tc.customName)
		if err != nil {
			t.Fatalf("failed to initialize transaction syncer: %v", err)
		}
		negClient := syncer.svcNegClient
		t.Run(tc.desc, func(t *testing.T) {
			// fakeZoneGetter will list 3 zones for VM_IP_PORT NEGs.
			expectZones := sets.NewString(negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone4)

			var expectedNegRefs map[string]negv1beta1.NegObjectReference
			var err error
			if tc.negExists {
				for zone := range expectZones {
					fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
						Version:             syncer.NegSyncerKey.GetAPIVersion(),
						Name:                testNegName,
						NetworkEndpointType: string(syncer.NegSyncerKey.NegType),
						Network:             fakeCloud.NetworkURL(),
						Subnetwork:          fakeCloud.SubnetworkURL(),
						Description:         tc.negDesc,
					}, zone, klog.TODO())
				}
				expectedNegRefs, err = negObjectReferences(fakeCloud, negv1beta1.ActiveState, expectZones, syncer.NegSyncerKey.NegName)
				if err != nil {
					t.Errorf("Failed to get negObjRef from NEG CR: %v", err)
				}
			}
			var refs []negv1beta1.NegObjectReference
			if tc.crStatusPopulated {
				for _, neg := range expectedNegRefs {
					refs = append(refs, neg)
				}
			}

			// Since timestamp gets truncated to the second, there is a chance that the timestamps will be the same as LastTransitionTime or LastSyncTime so use creation TS from an earlier date.
			creationTS := metav1.Date(2020, time.July, 23, 0, 0, 0, 0, time.UTC)
			//Create NEG CR for Syncer to update status on
			origCR := createNegCR(testNegName, creationTS, tc.crStatusPopulated, tc.crStatusPopulated, refs)
			neg, err := negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context.Background(), origCR, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Failed to create test NEG CR: %s", err)
			}
			syncer.svcNegLister.Add(neg)

			err = syncer.ensureNetworkEndpointGroups()
			if !tc.expectErr && err != nil {
				t.Errorf("Expected no error, but got: %v", err)
			}
			if tc.expectErr && err == nil {
				t.Errorf("Expected error, but got none")
			}

			negCR, err := negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context.Background(), testNegName, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get NEG from neg client: %s", err)
			}
			if !tc.expectErr {
				expectedNegRefs, err = negObjectReferences(fakeCloud, negv1beta1.ActiveState, expectZones, syncer.NegSyncerKey.NegName)
				if err != nil {
					t.Errorf("Failed to get negObjRef from NEG CR: %v", err)
				}
			}
			// if error occurs, expect that neg object references are not populated
			if tc.expectErr && !tc.crStatusPopulated {
				expectedNegRefs = nil
			}

			// NEG Object References should exist if:
			//  1. ensureNetworkEndpointGroups() doesn't result in errors, which
			//     should populate the NEG Object Reference for NEGs that have
			//     been successfully ensured.
			//  2. NEG CR is owned by a differ syncer, and the NEG object refs
			//     have been populated.
			expectPopulatedNegRefs := !tc.expectErr || (tc.crStatusPopulated && tc.expectNoopOnNegStatus)
			checkNegCR(t, negCR, creationTS, expectZones, nil, expectPopulatedNegRefs, false, fakeCloud)
			if tc.expectErr && tc.expectNoopOnNegStatus {
				// If CR is populated, we should have initialized and synced condition
				var expectedConditionLen int
				if tc.crStatusPopulated {
					expectedConditionLen = 2
				}

				if len(negCR.Status.Conditions) != expectedConditionLen {
					t.Errorf("Expected no change in NEG CR, but got len(negCR.Status.Conditions) = %d", len(negCR.Status.Conditions))
				}
				if len(negCR.Status.NetworkEndpointGroups) != len(expectedNegRefs) {
					t.Errorf("Expected no change in NEG CR, but got len(negCR.Status.NetworkEndpointGroups) = %d", len(negCR.Status.NetworkEndpointGroups))
				}
			}
			if tc.expectErr && !tc.expectNoopOnNegStatus {
				checkCondition(t, negCR.Status.Conditions, negv1beta1.Initialized, creationTS, corev1.ConditionFalse, true)
			}
			if tc.expectErr && tc.expectNoopOnNegStatus {
				checkCondition(t, negCR.Status.Conditions, negv1beta1.Initialized, creationTS, corev1.ConditionTrue, false)
			}
			if !tc.expectErr && tc.crStatusPopulated {
				checkCondition(t, negCR.Status.Conditions, negv1beta1.Initialized, creationTS, corev1.ConditionTrue, false)
			}
			if !tc.expectErr && !tc.crStatusPopulated {
				checkCondition(t, negCR.Status.Conditions, negv1beta1.Initialized, creationTS, corev1.ConditionTrue, true)
			}

			if tc.expectErr || tc.negExists {
				// Errored, so no expectation on created negs or negs were created beforehand
				return
			}

			// Verify the NEGs are created as expected
			retZones := sets.NewString()

			ret, _ := fakeCloud.AggregatedListNetworkEndpointGroup(syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
			for key, neg := range ret {
				retZones.Insert(key.Zone)
				if neg.Name != testNegName {
					t.Errorf("Unexpected neg %q, expected %q", neg.Name, testNegName)
				}

				checkNegDescription(t, syncer, neg.Description)
			}

			if !expectZones.Equal(retZones) {
				t.Errorf("Expected to find these zones: %+v, instead found: %+v", expectZones, retZones)
			}
		})

		negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Delete(context.TODO(), testNegName, metav1.DeleteOptions{})

		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone1, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone2, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone3, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone4, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())

	}
}

func TestEnsureNetworkEndpointGroupsMSC(t *testing.T) {
	zones := []string{negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone3}
	testNetworkURL := cloud.SelfLink(meta.VersionGA, "mock-project", "networks", meta.GlobalKey(defaultTestSubnet))
	testSubnetworkURL := cloud.SelfLink(meta.VersionGA, "mock-project", "subnetworks", meta.RegionalKey(defaultTestSubnet, "test-region"))
	testNegType := negtypes.VmIpPortEndpointType
	additionalTestSubnetworkURL := cloud.SelfLink(meta.VersionGA, "mock-project", "subnetworks", meta.RegionalKey(additionalTestSubnet, "test-region"))

	nodeTopologyCrWithDefaultSubnetOnly := nodetopologyv1.NodeTopology{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NodeTopology",
			APIVersion: "networking.gke.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Status: nodetopologyv1.NodeTopologyStatus{
			Subnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultTestSubnet, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", defaultTestSubnet)},
			},
		},
	}
	nodeTopologyCrWithAdditionalSubnets := nodeTopologyCrWithDefaultSubnetOnly
	nodeTopologyCrWithAdditionalSubnets.Status.Subnets = append(nodeTopologyCrWithAdditionalSubnets.Status.Subnets,
		nodetopologyv1.SubnetConfig{
			Name:       additionalTestSubnet,
			SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", additionalTestSubnet),
		},
	)

	currNodeTopologyCRName := flags.F.NodeTopologyCRName
	prevFlag := flags.F.EnableMultiSubnetClusterPhase1
	defer func() {
		flags.F.NodeTopologyCRName = currNodeTopologyCRName
		flags.F.EnableMultiSubnetClusterPhase1 = prevFlag
	}()
	flags.F.NodeTopologyCRName = "default"
	flags.F.EnableMultiSubnetClusterPhase1 = true

	negDesc := utils.NegDescription{
		ClusterUID:  kubeSystemUID,
		Namespace:   testServiceNamespace,
		ServiceName: testServiceName,
		Port:        "80",
	}.String()
	testCases := []struct {
		desc           string
		customNEGName  string
		nodeTopologyCr *nodetopologyv1.NodeTopology
		negDesc        string
		expectError    bool
		// expectNeedToUpdate indicates whether there is any conflicting NEG description.
		// When there is conflict, we do not update NEG Object Ref.
		expectNeedToUpdate bool
	}{
		{
			desc:               "NodeTopology CR doesn't exist",
			expectError:        false,
			negDesc:            negDesc,
			expectNeedToUpdate: true,
		},
		{
			desc:               "NodeTopology CR only contains default subnet",
			nodeTopologyCr:     &nodeTopologyCrWithDefaultSubnetOnly,
			negDesc:            negDesc,
			expectNeedToUpdate: true,
		},
		{
			desc:               "NodeTopology CR contains additional subnets, auto-generated NEG name",
			nodeTopologyCr:     &nodeTopologyCrWithAdditionalSubnets,
			negDesc:            negDesc,
			expectNeedToUpdate: true,
		},
		{
			desc:               "NodeTopology CR contains additional subnets, custom NEG name not exceeding character limit",
			customNEGName:      "custom-neg",
			nodeTopologyCr:     &nodeTopologyCrWithAdditionalSubnets,
			negDesc:            negDesc,
			expectError:        false,
			expectNeedToUpdate: true,
		},
		{
			desc:               "NodeTopology CR contains additional subnets, custom NEG name exceeding character limit",
			customNEGName:      "012345678901234567890123456789012345678901234567890123456", // 57 characters
			nodeTopologyCr:     &nodeTopologyCrWithAdditionalSubnets,
			negDesc:            negDesc,
			expectError:        true,
			expectNeedToUpdate: true,
		},
		{
			desc:           "NodeTopology CR contains additional subnets, conflicting NEG description",
			nodeTopologyCr: &nodeTopologyCrWithAdditionalSubnets,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "81", // Expected port to be 80
			}.String(),
			expectError:        true,
			expectNeedToUpdate: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetworkURL, testNetworkURL)

			_, syncer, err := newTestTransactionSyncer(fakeCloud, testNegType, tc.customNEGName != "")
			if err != nil {
				t.Fatalf("failed to initialize transaction syncer: %v", err)
			}
			if tc.customNEGName != "" {
				syncer.NegSyncerKey.NegName = tc.customNEGName
			}
			zonegetter.SetNodeTopologyHasSynced(syncer.zoneGetter, func() bool { return true })

			negName := syncer.NegSyncerKey.NegName

			for _, zone := range zones {
				err := fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
					Version:             syncer.NegSyncerKey.GetAPIVersion(),
					Name:                negName,
					NetworkEndpointType: string(syncer.NegSyncerKey.NegType),
					Network:             fakeCloud.NetworkURL(),
					Subnetwork:          fakeCloud.SubnetworkURL(),
					Description:         tc.negDesc,
				}, zone, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to create NEG: %v", err)
				}
			}

			negClient := syncer.svcNegClient
			negRefByZone, err := negObjectReferences(fakeCloud, negv1beta1.ActiveState, sets.NewString(zones...), syncer.NegSyncerKey.NegName)
			if err != nil {
				t.Errorf("Failed to get negObjRef from NEG CR: %v", err)
			}
			var refs []negv1beta1.NegObjectReference
			for _, neg := range negRefByZone {
				refs = append(refs, neg)
			}
			origCR := createNegCR(negName, metav1.Now(), true, true, refs)
			initialNegCr, err := negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context.Background(), origCR, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create test NEG CR: %s", err)
			}
			syncer.svcNegLister.Add(initialNegCr)

			if tc.nodeTopologyCr != nil {
				if err := zonegetter.AddNodeTopologyCR(syncer.zoneGetter, tc.nodeTopologyCr); err != nil {
					t.Fatalf("Failed to create Node Topology CR: %v", err)
				}
			}

			err = syncer.ensureNetworkEndpointGroups()

			if tc.expectError && err == nil {
				t.Errorf("Got no errors after ensureNetworkEndpointGroupsFromNodeTopology(), expected errors")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Got errors %v after ensureNetworkEndpointGroupsFromNodeTopology(), expected no errors", err)
			}

			syncedNegCR, err := negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context.Background(), negName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get NEG from neg client: %s", err)
			}
			if tc.expectNeedToUpdate {
				if reflect.DeepEqual(initialNegCr.Status, syncedNegCR.Status) {
					t.Errorf("Detected no updates on NEG CR status after ensureNetworkEndpointGroups(), expected updates:\nNEG CR Status: %v", syncedNegCR.Status)
				}

				for _, neg := range syncedNegCR.Status.NetworkEndpointGroups {
					expectedNegSubnetUrl := testSubnetworkURL
					// If this NEG is not in the default subnets
					resourceID, err := cloud.ParseResourceURL(neg.SelfLink)
					if err != nil {
						t.Fatalf("Failed to parse NEG SelfLink %q: %v", neg.SelfLink, err)
					}
					if resourceID.Key.Name != negName {
						expectedNegSubnetUrl = additionalTestSubnetworkURL
					}
					if neg.SubnetURL != expectedNegSubnetUrl {
						t.Errorf("For neg %q, got subnet URL = %q, expected %q", neg.SelfLink, neg.SubnetURL, expectedNegSubnetUrl)
					}
				}

			} else {
				if !reflect.DeepEqual(initialNegCr.Status, syncedNegCR.Status) {
					t.Errorf("Detected updates on NEG CR status after ensureNetworkEndpointGroups(), expected no updates:\nbefore %+v,\n after %+v", initialNegCr.Status, syncedNegCR.Status)
				}
			}
		})
	}
}

// TestUpdateInitStatusWithMultiSubnetCluster iterates over different zone
// transition situation, and checks if NEG Object Reference in the corresponding
// zone has the expected State.
func TestUpdateInitStatusWithMultiSubnetCluster(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testNegType := negtypes.VmIpPortEndpointType
	prevEnableMultiSubnetClusterPhase1 := flags.F.EnableMultiSubnetClusterPhase1
	prevNodeTopologyCRName := flags.F.NodeTopologyCRName
	defer func() {
		flags.F.EnableMultiSubnetClusterPhase1 = prevEnableMultiSubnetClusterPhase1
		flags.F.NodeTopologyCRName = prevNodeTopologyCRName
	}()
	flags.F.EnableMultiSubnetClusterPhase1 = true
	flags.F.NodeTopologyCRName = "default"

	// Active zones: zone1, zone2.
	// Inactive zones: zone3
	oldActiveZones := sets.NewString(negtypes.TestZone1, negtypes.TestZone2)
	oldInactiveZones := sets.NewString(negtypes.TestZone3)

	defaultSubnetConfig := nodetopologyv1.SubnetConfig{Name: defaultTestSubnet, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", defaultTestSubnet)}
	secondarySubnetConfig1 := nodetopologyv1.SubnetConfig{Name: secondaryTestSubnet1, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", secondaryTestSubnet1)}
	secondarySubnetConfig2 := nodetopologyv1.SubnetConfig{Name: secondaryTestSubnet2, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", secondaryTestSubnet2)}

	originalNonDefaultSubnets := []nodetopologyv1.SubnetConfig{secondarySubnetConfig1}

	testCases := []struct {
		desc              string
		newActiveZones    sets.String
		newInactiveZones  sets.String
		nonDefaultSubnets []nodetopologyv1.SubnetConfig
	}{
		{
			desc:              "Add a new zone zone4, an additional NEG ref should be added to NEG CR with ACTIVE status",
			newActiveZones:    sets.NewString(negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone4),
			newInactiveZones:  sets.NewString(negtypes.TestZone3),
			nonDefaultSubnets: originalNonDefaultSubnets,
		},
		{
			desc:              "Removed an ACTIVE zone zone2, corresponding NEG ref should still in NEG CR but with INACTIVE status",
			newActiveZones:    sets.NewString(negtypes.TestZone1),
			newInactiveZones:  sets.NewString(negtypes.TestZone2, negtypes.TestZone3),
			nonDefaultSubnets: originalNonDefaultSubnets,
		},
		{
			desc:              "Add back an INACTIVE zone zone3, the NEG ref in this zone should become ACTIVE in NEG CR",
			newActiveZones:    sets.NewString(negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone3),
			nonDefaultSubnets: originalNonDefaultSubnets,
		},
		{
			desc:              "Add secondarySubnet2 and remove secondarySubnet1, the NEG Refs in secondarySubnet1 should become TO_BE_DELETED in NEG CR, NEGs in secondarySubnet1 should be ACTIVE",
			newActiveZones:    oldActiveZones,
			newInactiveZones:  oldInactiveZones,
			nonDefaultSubnets: []nodetopologyv1.SubnetConfig{secondarySubnetConfig2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(defaultTestSubnetURL, testNetwork)
			nodeTopologyInformer := zonegetter.FakeNodeTopologyInformer()
			_, syncer, err := newTestTransactionSyncerWithTopologyInformer(fakeCloud, testNegType, false, nodeTopologyInformer)
			zonegetter.SetNodeTopologyHasSynced(syncer.zoneGetter, func() bool { return true })
			if err != nil {
				t.Fatalf("failed to initialize transaction syncer: %v", err)
			}
			svcNegClient := syncer.svcNegClient

			// Add topology to relect new state (default + non default subnets)
			nodeTopologyInformer.GetIndexer().Add(&nodetopologyv1.NodeTopology{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NodeTopology",
					APIVersion: "networking.gke.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: flags.F.NodeTopologyCRName,
				},

				Status: nodetopologyv1.NodeTopologyStatus{
					Subnets: append(tc.nonDefaultSubnets, defaultSubnetConfig),
				},
			})

			// Generate maps and lists to aid in test setup and verification
			// Generate map of Subnet to Neg Names to be used
			allSubnets := append(tc.nonDefaultSubnets, originalNonDefaultSubnets...)
			subnetToNameMap := generateNonDefaultSubnetNegNameMap(t, syncer, allSubnets)

			// add default subnet to list and maps
			allSubnets = append(allSubnets, defaultSubnetConfig)
			subnetToNameMap[defaultSubnetConfig] = testNegName

			// these are the original subnets that was the state at the last sync
			originalSubnets := append(originalNonDefaultSubnets, defaultSubnetConfig)
			// currentSubnets is the current state of subnets
			currentSubnets := append(tc.nonDefaultSubnets, defaultSubnetConfig)

			var initialNegRefs []negv1beta1.NegObjectReference
			for _, subnetConfig := range originalSubnets {
				// Create initial NEGs, and get their Object Ref to be used in NEG CR.
				negName := subnetToNameMap[subnetConfig]
				initialNegRefs = append(initialNegRefs, createNEGs(t, syncer, fakeCloud, negName, subnetConfig.SubnetPath, oldInactiveZones, negv1beta1.InactiveState)...)
				initialNegRefs = append(initialNegRefs, createNEGs(t, syncer, fakeCloud, negName, subnetConfig.SubnetPath, oldActiveZones, negv1beta1.ActiveState)...)
			}

			// Create NEG CR.
			creationTS := metav1.Now()
			origCR := createNegCR(testNegName, creationTS, true, true, initialNegRefs)
			svcNeg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context.Background(), origCR, metav1.CreateOptions{})
			if err != nil {
				t.Errorf("Failed to create test NEG CR: %s", err)
			}
			syncer.svcNegLister.Add(svcNeg)

			previousZones := oldActiveZones.Union(oldInactiveZones)
			// Create a NEG in a new zone if zone expanded.
			for _, subnetConfig := range currentSubnets {
				negName := subnetToNameMap[subnetConfig]
				createNEGs(t, syncer, fakeCloud, negName, subnetConfig.SubnetPath, tc.newActiveZones, negv1beta1.ActiveState)
			}

			// This is the input list to updateInitStatus().
			// It should only include NEG ref in the new active zones current subnets.
			var activeNegList []negv1beta1.NegObjectReference
			for zone := range tc.newActiveZones {
				for _, subnetConfig := range currentSubnets {
					negName := subnetToNameMap[subnetConfig]
					neg, err := fakeCloud.GetNetworkEndpointGroup(negName, zone, meta.VersionGA, klog.TODO())
					if err != nil {
						t.Fatalf("Failed to get NEG %s in zone %s: %v", negName, zone, err)
					}
					negRef := getNegObjectReference(neg, negv1beta1.ActiveState)
					activeNegList = append(activeNegList, negRef)
				}
			}

			// Inactive NEG refs should be added if there is any.
			syncer.updateInitStatus(activeNegList, nil)

			// gather negCR to validate the updates
			negCR, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context.Background(), testNegName, metav1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to create test NEG CR: %s", err)
			}

			params := checkCRParams{
				negCR:                  negCR,
				previousLastSyncTime:   creationTS,
				activeZones:            tc.newActiveZones,
				inactiveZones:          tc.newInactiveZones,
				expectPopulatedNegRefs: true,
				expectSyncTimeUpdate:   false,
				subnetToNegName:        subnetToNameMap,
				previousSubnets:        originalSubnets,
				currentSubnets:         currentSubnets,
				previousZones:          previousZones,
			}

			checkNegCRWithParams(t, fakeCloud, params)
		})
	}
}

// Test transition from only having the default subnet to multiple subnets
func TestUpdateInitStatusTransitions(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testNegType := negtypes.VmIpPortEndpointType
	prevEnableMultiSubnetClusterPhase1 := flags.F.EnableMultiSubnetClusterPhase1
	prevNodeTopologyCRName := flags.F.NodeTopologyCRName
	defer func() {
		flags.F.EnableMultiSubnetClusterPhase1 = prevEnableMultiSubnetClusterPhase1
		flags.F.NodeTopologyCRName = prevNodeTopologyCRName
	}()
	flags.F.EnableMultiSubnetClusterPhase1 = true
	flags.F.NodeTopologyCRName = "default"

	originalZones := sets.NewString(negtypes.TestZone2, negtypes.TestZone3)
	allZones := sets.NewString(negtypes.TestZone3)

	defaultSubnetConfig := nodetopologyv1.SubnetConfig{Name: defaultTestSubnet, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", defaultTestSubnet)}
	secondarySubnetConfig1 := nodetopologyv1.SubnetConfig{Name: secondaryTestSubnet1, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", secondaryTestSubnet1)}

	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(defaultTestSubnetURL, testNetwork)
	nodeTopologyInformer := zonegetter.FakeNodeTopologyInformer()
	_, syncer, err := newTestTransactionSyncerWithTopologyInformer(fakeCloud, testNegType, false, nodeTopologyInformer)
	zonegetter.SetNodeTopologyHasSynced(syncer.zoneGetter, func() bool { return true })
	if err != nil {
		t.Fatalf("failed to initialize transaction syncer: %v", err)
	}
	svcNegClient := syncer.svcNegClient
	currentSubnets := []nodetopologyv1.SubnetConfig{defaultSubnetConfig, secondarySubnetConfig1}

	subnetToNameMap := generateNonDefaultSubnetNegNameMap(t, syncer, []nodetopologyv1.SubnetConfig{secondarySubnetConfig1})
	subnetToNameMap[defaultSubnetConfig] = testNegName

	// Add topology to relect new state (default + non default subnets)
	nodeTopologyInformer.GetIndexer().Add(&nodetopologyv1.NodeTopology{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NodeTopology",
			APIVersion: "networking.gke.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: flags.F.NodeTopologyCRName,
		},

		Status: nodetopologyv1.NodeTopologyStatus{
			Subnets: currentSubnets,
		},
	})

	// Initial refs are only default subnet but in the originalZones
	refs := createNEGs(t, syncer, fakeCloud, testNegName, defaultTestSubnetURL, originalZones, negv1beta1.ActiveState)
	var initialNegRefs []negv1beta1.NegObjectReference
	for _, ref := range refs {
		refCopy := ref.DeepCopy()

		// empty the subnetwork to represent refs generated before subnetworks were added to refs
		refCopy.SubnetURL = ""
		initialNegRefs = append(initialNegRefs, *refCopy)
	}

	allRefs := createNEGs(t, syncer, fakeCloud, testNegName, defaultTestSubnetURL, allZones, negv1beta1.ActiveState)

	// Create NEG CR.
	creationTS := metav1.Now()
	origCR := createNegCR(testNegName, creationTS, true, true, initialNegRefs)
	svcNeg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context.Background(), origCR, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Failed to create test NEG CR: %s", err)
	}
	syncer.svcNegLister.Add(svcNeg)

	// create new negs in the new subnet only in the current zones
	allRefs = append(allRefs, createNEGs(t, syncer, fakeCloud, subnetToNameMap[secondarySubnetConfig1], secondarySubnetConfig1.SubnetPath, allZones, negv1beta1.ActiveState)...)

	// Inactive NEG refs should be added if there is any.
	syncer.updateInitStatus(allRefs, nil)

	// gather negCR to validate the updates
	negCR, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context.Background(), testNegName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to create test NEG CR: %s", err)
	}

	params := checkCRParams{
		negCR:                  negCR,
		previousLastSyncTime:   creationTS,
		activeZones:            allZones,
		inactiveZones:          sets.NewString(negtypes.TestZone2),
		expectPopulatedNegRefs: true,
		expectSyncTimeUpdate:   false,
		subnetToNegName:        subnetToNameMap,
		previousSubnets:        []nodetopologyv1.SubnetConfig{defaultSubnetConfig},
		currentSubnets:         currentSubnets,
		previousZones:          originalZones,
	}

	checkNegCRWithParams(t, fakeCloud, params)
}

// Test transition from only having the default subnet to multiple subnets
func TestSubnetChanges(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testNegType := negtypes.VmIpPortEndpointType
	prevEnableMultiSubnetClusterPhase1 := flags.F.EnableMultiSubnetClusterPhase1
	prevNodeTopologyCRName := flags.F.NodeTopologyCRName
	defer func() {
		flags.F.EnableMultiSubnetClusterPhase1 = prevEnableMultiSubnetClusterPhase1
		flags.F.NodeTopologyCRName = prevNodeTopologyCRName
	}()
	flags.F.EnableMultiSubnetClusterPhase1 = true
	flags.F.NodeTopologyCRName = "default"

	// to match the nodes populated into zoneGetter
	allZones := sets.NewString(negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone4)

	defaultSubnetConfig := nodetopologyv1.SubnetConfig{Name: defaultTestSubnet, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", defaultTestSubnet)}
	secondarySubnetConfig1 := nodetopologyv1.SubnetConfig{Name: secondaryTestSubnet1, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", secondaryTestSubnet1)}

	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(defaultTestSubnetURL, testNetwork)
	nodeTopologyInformer := zonegetter.FakeNodeTopologyInformer()
	_, ts, err := newTestTransactionSyncerWithTopologyInformer(fakeCloud, testNegType, false, nodeTopologyInformer)
	if err != nil {
		t.Fatalf("failed to initialize transaction syncer: %v", err)
	}

	// mark syncer as started without starting the syncer routine
	(ts.syncer.(*syncer)).stopped = false
	ts.needInit = false
	zonegetter.SetNodeTopologyHasSynced(ts.zoneGetter, func() bool { return true })

	svcNegClient := ts.svcNegClient
	currentSubnets := []nodetopologyv1.SubnetConfig{defaultSubnetConfig}

	subnetToNameMap := make(map[nodetopologyv1.SubnetConfig]string)
	subnetToNameMap[defaultSubnetConfig] = testNegName

	// Add topology to relect new state (default + non default subnets)
	nodeTopologyInformer.GetIndexer().Add(&nodetopologyv1.NodeTopology{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NodeTopology",
			APIVersion: "networking.gke.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: flags.F.NodeTopologyCRName,
		},

		Status: nodetopologyv1.NodeTopologyStatus{
			Subnets: currentSubnets,
		},
	})

	allRefs := createNEGs(t, ts, fakeCloud, testNegName, defaultTestSubnetURL, allZones, negv1beta1.ActiveState)
	// create negs in the removed subnet in the current zones
	allRefs = append(allRefs, createNEGs(t, ts, fakeCloud, subnetToNameMap[secondarySubnetConfig1], secondarySubnetConfig1.SubnetPath, allZones, negv1beta1.ActiveState)...)

	// Create NEG CR.
	creationTS := metav1.Date(2020, time.July, 23, 0, 0, 0, 0, time.UTC)
	origCR := createNegCR(testNegName, creationTS, true, true, allRefs)
	svcNeg, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context.Background(), origCR, metav1.CreateOptions{})
	if err != nil {
		t.Errorf("Failed to create test NEG CR: %s", err)
	}
	ts.svcNegLister.Add(svcNeg)

	// Inactive NEG refs should be added if there is any.
	ts.sync()

	// gather negCR to validate the updates
	negCR, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context.Background(), testNegName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to create test NEG CR: %s", err)
	}

	params := checkCRParams{
		negCR:                  negCR,
		previousLastSyncTime:   creationTS,
		activeZones:            allZones,
		expectPopulatedNegRefs: true,
		expectSyncTimeUpdate:   true,
		subnetToNegName:        subnetToNameMap,
		previousSubnets:        []nodetopologyv1.SubnetConfig{defaultSubnetConfig, secondarySubnetConfig1},
		currentSubnets:         currentSubnets,
		previousZones:          allZones,
	}

	checkNegCRWithParams(t, fakeCloud, params)
}

func TestIsSubnetChange(t *testing.T) {
	//Create all VPC and Subnets needed
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testSubnetwork := defaultTestSubnetURL
	defaultSubnetConfig := nodetopologyv1.SubnetConfig{Name: defaultTestSubnet, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", defaultTestSubnet)}
	secondarySubnetConfig1 := nodetopologyv1.SubnetConfig{Name: secondaryTestSubnet1, SubnetPath: fmt.Sprintf("projects/mock-project/regions/test-region/subnetworks/%s", secondaryTestSubnet1)}

	// Save old flags to reset at end of test
	prevNodeTopologyCRName := flags.F.NodeTopologyCRName
	defer func() {
		flags.F.NodeTopologyCRName = prevNodeTopologyCRName
	}()
	flags.F.NodeTopologyCRName = "default"

	testCases := []struct {
		desc            string
		originalSubnets []nodetopologyv1.SubnetConfig
		currentSubnets  []nodetopologyv1.SubnetConfig
		enableMSCPhase1 bool
		// emptySubnetURL refers to whether the origRefs have subnetURL populated. If this is true,
		// originalSubnets can only include the defaultSubnetConfig
		emptySubnetURL bool
		expectedResult bool
	}{
		{
			desc:            "subnet was added",
			originalSubnets: []nodetopologyv1.SubnetConfig{defaultSubnetConfig},
			currentSubnets:  []nodetopologyv1.SubnetConfig{defaultSubnetConfig, secondarySubnetConfig1},
			enableMSCPhase1: true,
			expectedResult:  true,
		},
		{
			desc:            "subnet was deleted",
			originalSubnets: []nodetopologyv1.SubnetConfig{secondarySubnetConfig1, defaultSubnetConfig},
			currentSubnets:  []nodetopologyv1.SubnetConfig{defaultSubnetConfig},
			enableMSCPhase1: true,
			expectedResult:  true,
		},
		{
			desc:            "no subnet change occurred",
			originalSubnets: []nodetopologyv1.SubnetConfig{defaultSubnetConfig, secondarySubnetConfig1},
			currentSubnets:  []nodetopologyv1.SubnetConfig{defaultSubnetConfig, secondarySubnetConfig1},
			enableMSCPhase1: true,
			expectedResult:  false,
		},
		{
			desc:            "no subnet change occurred, origRefs have empty URLs",
			originalSubnets: []nodetopologyv1.SubnetConfig{defaultSubnetConfig},
			currentSubnets:  []nodetopologyv1.SubnetConfig{defaultSubnetConfig},
			enableMSCPhase1: true,
			emptySubnetURL:  true,
			expectedResult:  false,
		},
		{
			desc:            "subnet was added and MSC is disabled",
			originalSubnets: []nodetopologyv1.SubnetConfig{defaultSubnetConfig},
			currentSubnets:  []nodetopologyv1.SubnetConfig{defaultSubnetConfig, secondarySubnetConfig1},
			enableMSCPhase1: false,
			expectedResult:  false,
		},

		// In this case NEGs allready exist in secondary subnet previously. We assume that MSC was
		// disabled after a controller update, so the controller should recognize it as a subnet change.
		{
			desc:            "subnet was deleted and MSC is disabled",
			originalSubnets: []nodetopologyv1.SubnetConfig{defaultSubnetConfig, secondarySubnetConfig1},
			currentSubnets:  []nodetopologyv1.SubnetConfig{defaultSubnetConfig},
			enableMSCPhase1: false,
			expectedResult:  true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Initialize syncer
			fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
			nodeTopologyInformer := zonegetter.FakeNodeTopologyInformer()
			testNegType := negtypes.VmIpPortEndpointType
			_, ts, err := newTestTransactionSyncerWithTopologyInformer(fakeCloud, testNegType, false, nodeTopologyInformer)
			if err != nil {
				t.Fatalf("failed to initialize transaction syncer: %v", err)
			}

			// overwrite the ZoneGetter to pipe in flag gate value
			nodeInformer := zonegetter.FakeNodeInformer()
			zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
			fakeZoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, nodeTopologyInformer, defaultTestSubnetURL, !tc.enableMSCPhase1)
			if err != nil {
				t.Errorf("failed to initialize zone getter: %v", err)
			}

			ts.zoneGetter = fakeZoneGetter
			//Make sure NodeTopologyInformer is considered as synced, otherwise only defaultSubnet is returned
			zonegetter.SetNodeTopologyHasSynced(ts.zoneGetter, func() bool { return true })
			origZones, err := fakeZoneGetter.ListZones(negtypes.NodeFilterForEndpointCalculatorMode(ts.EpCalculatorMode), klog.TODO())
			if err != nil {
				t.Errorf("errored when retrieving zones: %s", err)
			}

			var allRefs []negv1beta1.NegObjectReference
			for _, subnet := range tc.originalSubnets {
				negName := fmt.Sprintf("testNegName-%s", subnet.Name)
				refs := createNEGs(t, ts, fakeCloud, negName, subnet.SubnetPath, sets.NewString(origZones...), negv1beta1.ActiveState)

				if !tc.emptySubnetURL {
					allRefs = append(allRefs, refs...)
					continue
				}
				for _, ref := range refs {
					ref.SubnetURL = ""
					allRefs = append(allRefs, ref)
				}
			}

			negCR := createNegCR(ts.NegName, metav1.Now(), true, true, allRefs)
			if err = ts.svcNegLister.Add(negCR); err != nil {
				t.Errorf("failed to add neg to store:%s", err)
			}
			// Add topology to relect new state
			nodeTopologyInformer.GetIndexer().Add(&nodetopologyv1.NodeTopology{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NodeTopology",
					APIVersion: "networking.gke.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: flags.F.NodeTopologyCRName,
				},

				Status: nodetopologyv1.NodeTopologyStatus{
					Subnets: tc.currentSubnets,
				},
			})

			isSubnetChange := ts.isSubnetChange()
			if isSubnetChange != tc.expectedResult {
				t.Errorf("isSubnetChange() returned %t, wanted %t", isSubnetChange, tc.expectedResult)
			}
		})
	}
}

func generateNonDefaultSubnetNegNameMap(t *testing.T, ts *transactionSyncer, subnetConfigs []nodetopologyv1.SubnetConfig) map[nodetopologyv1.SubnetConfig]string {
	t.Helper()
	negNameSubnetMap := make(map[nodetopologyv1.SubnetConfig]string)

	for _, subnet := range subnetConfigs {

		negName, err := ts.getNonDefaultSubnetNEGName(subnet.Name)
		if err != nil {
			t.Fatalf("failed to generate non default subnet name: %v", err)
		}
		negNameSubnetMap[subnet] = negName
	}
	return negNameSubnetMap
}

// createNEGs creates NEG in the specified zones and creates relevant NegRefs with the provided Neg state
func createNEGs(t *testing.T, ts *transactionSyncer, cloud negtypes.NetworkEndpointGroupCloud, negName, subnetURL string, zones sets.String, negRefState negv1beta1.NegState) []negv1beta1.NegObjectReference {
	t.Helper()

	var refs []negv1beta1.NegObjectReference

	for zone := range zones {
		err := cloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
			Version:             ts.NegSyncerKey.GetAPIVersion(),
			Name:                negName,
			NetworkEndpointType: string(ts.NegSyncerKey.NegType),
			Network:             cloud.NetworkURL(),
			Subnetwork:          subnetURL,
			Zone:                zone,
		}, zone, klog.TODO())
		if err != nil {
			t.Fatalf("Failed to create NEG %s in zone %s: %v", negName, zone, err)
		}
		neg, err := cloud.GetNetworkEndpointGroup(negName, zone, meta.VersionGA, klog.TODO())
		if err != nil {
			t.Fatalf("Failed to get NEG %s in zone %s: %v", negName, zone, err)
		}
		negRef := negv1beta1.NegObjectReference{
			Id:                  fmt.Sprint(neg.Id),
			SelfLink:            neg.SelfLink,
			NetworkEndpointType: negv1beta1.NetworkEndpointType(neg.NetworkEndpointType),
			State:               negRefState,
			SubnetURL:           neg.Subnetwork,
		}
		refs = append(refs, negRef)
	}
	return refs
}

func TestUpdateStatus(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testNegType := negtypes.VmIpPortEndpointType
	testNegRefs := []negv1beta1.NegObjectReference{
		{
			Id:                  "0",
			SelfLink:            "self-link-0",
			NetworkEndpointType: "neg-type-0",
		},
		{
			Id:                  "1",
			SelfLink:            "self-link-1",
			NetworkEndpointType: "neg-type-1",
		},
	}

	testCases := []struct {
		desc               string
		populateConditions map[string]bool
		negRefs            []negv1beta1.NegObjectReference
		expectedNeedInit   bool
	}{
		{desc: "conditions don't exist, neg refs don't exist",
			populateConditions: map[string]bool{
				negv1beta1.Initialized: false,
				negv1beta1.Synced:      false,
			},
			expectedNeedInit: true,
		},
		{desc: "both conditions exist, neg refs exist",
			populateConditions: map[string]bool{
				negv1beta1.Initialized: true,
				negv1beta1.Synced:      true,
			},
			negRefs:          testNegRefs,
			expectedNeedInit: false,
		},
		{desc: "both conditions exist, neg refs don't exist",
			populateConditions: map[string]bool{
				negv1beta1.Initialized: true,
				negv1beta1.Synced:      true,
			},
			expectedNeedInit: true,
		},
		{desc: "initialized exists, neg refs exist",
			populateConditions: map[string]bool{
				negv1beta1.Initialized: true,
				negv1beta1.Synced:      false,
			},
			negRefs:          testNegRefs,
			expectedNeedInit: false,
		},
		{desc: "synced exists, neg refs exist",
			populateConditions: map[string]bool{
				negv1beta1.Initialized: false,
				negv1beta1.Synced:      true,
			},
			negRefs:          testNegRefs,
			expectedNeedInit: true,
		},
		{desc: "conditions don't exist, negRefs exist",
			populateConditions: map[string]bool{
				negv1beta1.Initialized: false,
				negv1beta1.Synced:      false,
			},
			negRefs:          testNegRefs,
			expectedNeedInit: true,
		},
	}
	for _, syncErr := range []error{nil, fmt.Errorf("error")} {
		for _, tc := range testCases {
			t.Run(tc.desc, func(t *testing.T) {
				fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(defaultTestSubnetURL, testNetwork)
				_, syncer, err := newTestTransactionSyncer(fakeCloud, testNegType, false)
				if err != nil {
					t.Fatalf("failed to initialize transaction syncer: %v", err)
				}
				svcNegClient := syncer.svcNegClient
				syncer.needInit = false
				if len(tc.negRefs) == 0 {
					err := fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
						Version:             syncer.NegSyncerKey.GetAPIVersion(),
						Name:                testNegName,
						NetworkEndpointType: string(syncer.NegSyncerKey.NegType),
						Network:             fakeCloud.NetworkURL(),
						Subnetwork:          fakeCloud.SubnetworkURL(),
						Description:         "",
					}, testZone1, klog.TODO())
					if err != nil {
						t.Errorf("failed to create test NEG: %s", err)
					}

					_, err = fakeCloud.GetNetworkEndpointGroup(testNegName, testZone1, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
					if err != nil {
						t.Errorf("failed to get neg from cloud: %s ", err)
					}
				}

				// Since timestamp gets truncated to the second, there is a chance that the timestamps will be the same as LastTransitionTime or LastSyncTime so use creation TS from an earlier date
				creationTS := metav1.Date(2020, time.July, 23, 0, 0, 0, 0, time.UTC)
				origCR := createNegCR(testNegName, creationTS, tc.populateConditions[negv1beta1.Initialized], tc.populateConditions[negv1beta1.Synced], tc.negRefs)
				origCR, err = svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context.Background(), origCR, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("Failed to create test NEG CR: %s", err)
				}
				syncer.svcNegLister.Add(origCR)

				syncer.updateStatus(syncErr)

				negCR, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context.Background(), testNegName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to create test NEG CR: %s", err)
				}

				if syncErr != nil {
					checkCondition(t, negCR.Status.Conditions, negv1beta1.Synced, creationTS, corev1.ConditionFalse, true)
				} else if tc.populateConditions[negv1beta1.Synced] {
					checkCondition(t, negCR.Status.Conditions, negv1beta1.Synced, creationTS, corev1.ConditionTrue, false)
				} else {
					checkCondition(t, negCR.Status.Conditions, negv1beta1.Synced, creationTS, corev1.ConditionTrue, true)
				}

				if syncer.needInit != tc.expectedNeedInit {
					t.Errorf("expected manager.needInit to be %t, but was %t", tc.expectedNeedInit, syncer.needInit)
				}

				if !creationTS.Before(&negCR.Status.LastSyncTime) {
					t.Errorf("neg cr should have an updated LastSyncTime")
				}
			})
		}
	}
}

func TestIsZoneChange(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testSubnetwork := defaultTestSubnetURL
	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
	testNegType := negtypes.VmIpPortEndpointType

	testCases := []struct {
		desc                           string
		zoneDeleted                    bool
		zoneAdded                      bool
		nodeWithNoProviderIDAdded      bool
		negCRStateInactive             bool
		nodeWithInvalidProviderIDAdded bool
		expectedResult                 bool
	}{
		{
			desc:           "zone was added",
			zoneAdded:      true,
			expectedResult: true,
		},
		{
			desc:           "zone was deleted",
			zoneDeleted:    true,
			expectedResult: true,
		},
		{
			desc:                           "node with invalid providerID was added",
			nodeWithInvalidProviderIDAdded: true,
			expectedResult:                 false,
		},
		{
			desc:                      "node with no providerID was added",
			nodeWithNoProviderIDAdded: true,
			expectedResult:            false,
		},
		{
			desc:                      "node with no providerID was added and normal nodes added",
			zoneAdded:                 true,
			nodeWithNoProviderIDAdded: true,
			expectedResult:            true,
		},
		{
			desc:           "no zone change occurred",
			expectedResult: false,
		},
		{
			desc:               "neg cr state is inactive",
			negCRStateInactive: true,
			expectedResult:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, syncer, err := newTestTransactionSyncer(fakeCloud, testNegType, false)
			if err != nil {
				t.Fatalf("failed to initialize transaction syncer: %v", err)
			}
			fakeZoneGetter := syncer.zoneGetter
			origZones, err := fakeZoneGetter.ListZones(negtypes.NodeFilterForEndpointCalculatorMode(syncer.EpCalculatorMode), klog.TODO())
			if err != nil {
				t.Errorf("errored when retrieving zones: %s", err)
			}

			for _, zone := range origZones {
				fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{
					Version:             syncer.NegSyncerKey.GetAPIVersion(),
					Name:                testNegName,
					NetworkEndpointType: string(syncer.NegSyncerKey.NegType),
					Network:             fakeCloud.NetworkURL(),
					Subnetwork:          fakeCloud.SubnetworkURL(),
				}, zone, klog.TODO())
			}
			negRefMap, err := negObjectReferences(fakeCloud, negv1beta1.ActiveState, sets.NewString(origZones...), syncer.NegSyncerKey.NegName)
			if err != nil {
				t.Errorf("Failed to get negObjRef from NEG CR: %v", err)
			}

			var refs []negv1beta1.NegObjectReference
			for _, neg := range negRefMap {
				refs = append(refs, neg)
			}
			if tc.negCRStateInactive {
				refs[0].State = negv1beta1.InactiveState
			}
			negCR := createNegCR(syncer.NegName, metav1.Now(), true, true, refs)
			if err = syncer.svcNegLister.Add(negCR); err != nil {
				t.Errorf("failed to add neg to store:%s", err)
			}

			if tc.zoneDeleted {
				zonegetter.DeleteFakeNodesInZone(t, "zone1", fakeZoneGetter)
			}

			if tc.zoneAdded {
				// Add a node in the zone to add a new zone to the ZoneGetter.
				if err := zonegetter.AddFakeNodes(fakeZoneGetter, "zoneA", "instance-1"); err != nil {
					t.Errorf("failed to add zone:%s", err)
				}
			}

			if tc.nodeWithInvalidProviderIDAdded {
				if err := zonegetter.AddFakeNode(fakeZoneGetter, &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-with-invalid-providerID",
					},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://foo-project/instance",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				}); err != nil {
					t.Errorf("failed to add node with invalid providerID:%s", err)
				}
			}

			if tc.nodeWithNoProviderIDAdded {
				if err := zonegetter.AddFakeNode(fakeZoneGetter, &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-without-providerID",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:   corev1.NodeReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				}); err != nil {
					t.Errorf("failed to add node with no providerID:%s", err)
				}
			}

			isZoneChange := syncer.isZoneChange()
			if isZoneChange != tc.expectedResult {
				t.Errorf("isZoneChange() returned %t, wanted %t", isZoneChange, tc.expectedResult)
			}
		})

	}
}

func TestUnknownNodes(t *testing.T) {
	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testSubnetwork := defaultTestSubnetURL
	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)

	testIP1 := "10.100.1.1"
	testIP2 := "10.100.1.2"
	testIP3 := "10.100.2.1"
	testPort := int64(80)

	testEndpointSlices := getDefaultEndpointSlices()
	testEndpointSlices[0].Endpoints[0].NodeName = ptr.To("unknown-node")
	testEndpointMap := map[negtypes.NEGLocation]*composite.NetworkEndpoint{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: {
			Instance:  negtypes.TestInstance1,
			IpAddress: testIP1,
			Port:      testPort,
		},

		{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: {
			Instance:  negtypes.TestInstance3,
			IpAddress: testIP2,
			Port:      testPort,
		},

		{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: {
			Instance:  negtypes.TestUpgradeInstance1,
			IpAddress: testIP3,
			Port:      testPort,
		},
	}

	// Create initial NetworkEndpointGroups in cloud
	var objRefs []negv1beta1.NegObjectReference
	for negLocation, endpoint := range testEndpointMap {
		fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, negLocation.Zone, klog.TODO())
		fakeCloud.AttachNetworkEndpoints(testNegName, negLocation.Zone, []*composite.NetworkEndpoint{endpoint}, meta.VersionGA, klog.TODO())
		neg, err := fakeCloud.GetNetworkEndpointGroup(testNegName, negLocation.Zone, meta.VersionGA, klog.TODO())
		if err != nil {
			t.Fatalf("failed to get neg from fake cloud: %s", err)
		}

		objRefs = append(objRefs, negv1beta1.NegObjectReference{SelfLink: neg.SelfLink, SubnetURL: defaultTestSubnetURL})
	}
	neg := &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNegName,
			Namespace: testServiceNamespace,
		},
		Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
			NetworkEndpointGroups: objRefs,
		},
	}

	_, s, err := newTestTransactionSyncer(fakeCloud, negtypes.VmIpPortEndpointType, false)
	if err != nil {
		t.Fatalf("failed to initialize transaction syncer: %v", err)
	}
	s.needInit = false

	for _, eps := range testEndpointSlices {
		s.endpointSliceLister.Add(eps)
	}
	s.svcNegLister.Add(neg)
	// mark syncer as started without starting the syncer routine
	(s.syncer.(*syncer)).stopped = false

	err = s.syncInternal()
	if err == nil {
		t.Errorf("syncInternal returned nil, expected an error")
	}

	// Check that unknown zone did not cause endpoints to be removed
	out, _, _, err := retrieveExistingZoneNetworkEndpointMap(map[string]string{defaultTestSubnet: testNegName}, zoneGetter, fakeCloud, meta.VersionGA, negtypes.L7Mode, false, klog.TODO(), s.negMetrics, false)
	if err != nil {
		t.Errorf("errored retrieving existing network endpoints")
	}

	expectedEndpoints := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))},
		),
		{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))},
		),
		{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestUpgradeInstance1, Port: strconv.Itoa(int(testPort))},
		),
	}

	if !reflect.DeepEqual(expectedEndpoints, out) {
		t.Errorf("endpoints were modified after syncInternal:\ngot %+v,\n expected %+v", out, expectedEndpoints)
	}
}

// TestEnableDegradedMode verifies if DegradedMode has been correctly enabled for L7 endpoint calculator
func TestEnableDegradedMode(t *testing.T) {
	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), test.DefaultTestSubnetURL, false)
	if err != nil {
		t.Fatalf("failed to initialize zone getter: %v", err)
	}
	fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE, negtypes.NewTestContext().NegMetrics)
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	ipOutOfRange := "1.1.1.1"
	mockGCE.MockNetworkEndpointGroups.AttachNetworkEndpointsHook = func(ctx context.Context, key *meta.Key, obj *compute.NetworkEndpointGroupsAttachEndpointsRequest, m *cloud.MockNetworkEndpointGroups, options ...cloud.Option) error {
		for _, newEP := range obj.NetworkEndpoints {
			if newEP.IpAddress == ipOutOfRange {
				return &googleapi.Error{
					Code:    http.StatusBadRequest,
					Message: fmt.Sprintf("Specified IP address %v doesn't belong to the (sub)network or the instance", newEP.IpAddress),
				}
			}
		}
		return negtypes.MockAttachNetworkEndpointsHook(ctx, key, obj, m)
	}

	testIP1 := "10.100.1.1"
	testIP2 := "10.100.1.2"
	testIP3 := "10.100.2.1"
	testPort := int64(80)

	// only include matching port endpoints so we won't encounter error when validatingEndpoints
	nodeMissingEndpointSlices := getDefaultEndpointSlices()[:1]
	nodeMissingEndpointSlices[0].Endpoints[0].NodeName = nil
	ipOutOfCIDREndpointSlices := getDefaultEndpointSlices()[:1]
	ipOutOfCIDREndpointSlices[0].Endpoints[3].Addresses = []string{ipOutOfRange}
	validEndpointSlice := getDefaultEndpointSlices()[:1]
	testEndpointMap := map[string]*composite.NetworkEndpoint{
		negtypes.TestZone1: {
			Instance:  negtypes.TestInstance1,
			IpAddress: testIP1,
			Port:      testPort,
		},
		negtypes.TestZone2: {
			Instance:  negtypes.TestInstance3,
			IpAddress: testIP2,
			Port:      testPort,
		},
		negtypes.TestZone4: {
			Instance:  negtypes.TestUpgradeInstance1,
			IpAddress: testIP3,
			Port:      testPort,
		},
	}

	initialEndpoints := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
		),
		{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance3||80"),
		),
		{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.2.1||upgrade-instance1||80"),
		),
	}

	updateSucceedEndpoints := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80")),
		{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80")),
		{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
	}

	updateFailedEndpoints := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
		{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80")),
		{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
		{Zone: negtypes.TestZone4, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
	}

	testCases := []struct {
		desc                 string
		modify               func(ts *transactionSyncer)
		negName              string // to distinguish endpoints in differnt NEGs
		testEndpointSlices   []*discovery.EndpointSlice
		expectedEndpoints    map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expectedInErrorState bool
		expectErr            error
	}{
		{
			desc: "enable degraded mode, not error state, include invalid endpoints that would trigger error state before API calls",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = true
				ts.resetErrorState()
			},
			negName:              "neg-1",
			testEndpointSlices:   nodeMissingEndpointSlices,
			expectedEndpoints:    initialEndpoints,
			expectedInErrorState: true,
			expectErr:            negtypes.ErrEPNodeMissing,
		},
		{
			desc: "enable degraded mode, in error state, include invalid endpoints that would trigger error state before API calls",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = true
				ts.setErrorState()
			},
			negName:              "neg-2",
			testEndpointSlices:   nodeMissingEndpointSlices,
			expectedEndpoints:    updateSucceedEndpoints,
			expectedInErrorState: true,
			expectErr:            nil,
		},
		{
			desc: "enable degraded mode, not error state, no invalid endpoints",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = true
				ts.resetErrorState()
			},
			negName:              "neg-3",
			testEndpointSlices:   validEndpointSlice,
			expectedEndpoints:    updateSucceedEndpoints,
			expectedInErrorState: false,
			expectErr:            nil,
		},
		{
			desc: "enable degraded mode, in error state, no invalid endpoints",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = true
				ts.setErrorState()
			},
			negName:              "neg-4",
			testEndpointSlices:   validEndpointSlice,
			expectedEndpoints:    updateSucceedEndpoints,
			expectedInErrorState: false, // we should reset error state
			expectErr:            nil,
		},
		{
			desc: "disable degraded mode, not error state, include invalid endpoints that would trigger error state before API calls",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = false
				ts.resetErrorState()
			},
			negName:              "neg-5",
			testEndpointSlices:   nodeMissingEndpointSlices,
			expectedEndpoints:    initialEndpoints,
			expectedInErrorState: true,
			expectErr:            negtypes.ErrEPNodeMissing,
		},
		{
			desc: "disable degraded mode, and in error state, include invalid endpoints that would trigger error state before API calls",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = false
				ts.setErrorState()
			},
			negName:              "neg-6",
			testEndpointSlices:   nodeMissingEndpointSlices,
			expectedEndpoints:    initialEndpoints,
			expectedInErrorState: true,
			expectErr:            negtypes.ErrEPNodeMissing,
		},
		{
			desc: "disable degraded mode, and not error state, no invalid endpoints",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = false
				ts.resetErrorState()
			},
			negName:              "neg-7",
			testEndpointSlices:   validEndpointSlice,
			expectedEndpoints:    updateSucceedEndpoints,
			expectedInErrorState: false,
			expectErr:            nil,
		},
		{
			desc: "disable degraded mode, and in error state, no invalid endpoints",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = false
				ts.setErrorState()
			},
			negName:              "neg-8",
			testEndpointSlices:   validEndpointSlice,
			expectedEndpoints:    updateSucceedEndpoints,
			expectedInErrorState: false,
			expectErr:            nil,
		},
		{
			desc: "enable degraded mode, and not in error state, include invalid endpoints that would trigger error state after API calls",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = true
				ts.resetErrorState()
			},
			negName:              "neg-9",
			testEndpointSlices:   ipOutOfCIDREndpointSlices,
			expectedEndpoints:    updateFailedEndpoints,
			expectedInErrorState: true,
			expectErr:            nil,
		},
		{
			desc: "disable degraded mode, and not in error state, include invalid endpoints that would trigger error state after API calls",
			modify: func(ts *transactionSyncer) {
				ts.enableDegradedMode = false
				ts.resetErrorState()
			},
			negName:              "neg-10",
			testEndpointSlices:   ipOutOfCIDREndpointSlices,
			expectedEndpoints:    updateFailedEndpoints,
			expectedInErrorState: true,
			expectErr:            nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Create initial NetworkEndpointGroups in cloud
			var objRefs []negv1beta1.NegObjectReference
			for zone, endpoint := range testEndpointMap {
				fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: tc.negName, Version: meta.VersionGA}, zone, klog.TODO())
				fakeCloud.AttachNetworkEndpoints(tc.negName, zone, []*composite.NetworkEndpoint{endpoint}, meta.VersionGA, klog.TODO())
				neg, err := fakeCloud.GetNetworkEndpointGroup(tc.negName, zone, meta.VersionGA, klog.TODO())
				if err != nil {
					t.Fatalf("failed to get neg from fake cloud: %s", err)
				}
				objRefs = append(objRefs, negv1beta1.NegObjectReference{SelfLink: neg.SelfLink})
			}
			neg := &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.negName,
					Namespace: testServiceNamespace,
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: objRefs,
				},
			}
			_, s, err := newTestTransactionSyncer(fakeCloud, negtypes.VmIpPortEndpointType, false)
			if err != nil {
				t.Fatalf("failed to initialize transaction syncer: %v", err)
			}
			s.NegSyncerKey.NegName = tc.negName
			s.needInit = false
			addPodsToLister(s.podLister, getDefaultEndpointSlices())
			for i := 1; i <= 4; i++ {
				s.nodeLister.Add(&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("instance%v", i),
					},
					Spec: corev1.NodeSpec{
						PodCIDR:  fmt.Sprintf("10.100.%v.0/24", i),
						PodCIDRs: []string{fmt.Sprintf("200%v:db8::/48", i), fmt.Sprintf("10.100.%v.0/24", i)},
					},
				})
			}
			testLabels := map[string]string{
				"run": "foo",
			} // this should match to pod labels
			s.serviceLister.Add(&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testServiceNamespace,
					Name:      testServiceName,
				},
				Spec: corev1.ServiceSpec{
					Selector: testLabels,
				},
			})
			for _, eps := range tc.testEndpointSlices {
				s.endpointSliceLister.Add(eps)
			}
			s.svcNegLister.Add(neg)
			// mark syncer as started without starting the syncer routine
			(s.syncer.(*syncer)).stopped = false
			tc.modify(s)

			subnetToNegMapping := map[string]string{defaultTestSubnet: tc.negName}
			out, _, _, err := retrieveExistingZoneNetworkEndpointMap(subnetToNegMapping, zoneGetter, fakeCloud, meta.VersionGA, negtypes.L7Mode, false, klog.TODO(), s.negMetrics, false)
			if err != nil {
				t.Errorf("errored retrieving existing network endpoints")
			}
			if !reflect.DeepEqual(initialEndpoints, out) {
				t.Errorf("endpoints should not be changed before sync:\ngot %+v,\n expected %+v", out, tc.expectedEndpoints)
			}

			err = s.syncInternal()
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("syncInternal returned %v, expected %v", err, tc.expectErr)
			}
			err = wait.PollImmediate(time.Second, 3*time.Second, func() (bool, error) {
				out, _, _, err = retrieveExistingZoneNetworkEndpointMap(subnetToNegMapping, zoneGetter, fakeCloud, meta.VersionGA, negtypes.L7Mode, false, klog.TODO(), s.negMetrics, false)
				if err != nil {
					return false, nil
				}
				if !reflect.DeepEqual(tc.expectedEndpoints, out) {
					return false, nil
				}
				if getErrorState(s) != tc.expectedInErrorState {
					return false, nil
				}
				return true, nil
			})
			if err != nil {
				t.Errorf("endpoints are different from expected:\ngot %+v,\n expected %+v", out, tc.expectedEndpoints)
			}
		})
	}
}

func getErrorState(s *transactionSyncer) bool {
	s.syncLock.Lock()
	errorState := s.inErrorState()
	s.syncLock.Unlock()
	return errorState
}

func TestCheckEndpointBatchErr(t *testing.T) {
	requestError := &googleapi.Error{
		Code: http.StatusBadRequest,
	}
	serverError := &googleapi.Error{
		Code: http.StatusInternalServerError,
	}

	testCases := []struct {
		desc              string
		err               error
		endpointOperation transactionOp
		expectErr         error
	}{
		{
			desc:              "Not googleapi error",
			err:               errors.New("Not googleapi.Error"),
			endpointOperation: attachOp,
			expectErr:         negtypes.ErrInvalidAPIResponse,
		},
		{
			desc:              "Server error, status code 500",
			err:               serverError,
			endpointOperation: attachOp,
			expectErr:         serverError,
		},
		{
			desc:              "Invalid endpoint batch for endpoint attach, status code 400",
			err:               requestError,
			endpointOperation: attachOp,
			expectErr:         negtypes.ErrInvalidEPAttach,
		},
		{
			desc:              "Invalid endpoint batch for endpoint detach, status code 400",
			err:               requestError,
			endpointOperation: detachOp,
			expectErr:         negtypes.ErrInvalidEPDetach,
		},
		{
			desc:              "Wrapped googleapi server error",
			err:               fmt.Errorf("%w: wrapped error", serverError),
			endpointOperation: attachOp,
			expectErr:         serverError,
		},
		{
			desc:              "Wrapped googleapi attach request error",
			err:               fmt.Errorf("%w: wrapped error", requestError),
			endpointOperation: attachOp,
			expectErr:         negtypes.ErrInvalidEPAttach,
		},
		{
			desc:              "Double wrapped googleapi server error",
			err:               fmt.Errorf("%w: %w", serverError, errors.New("wrapped error")),
			endpointOperation: attachOp,
			expectErr:         serverError,
		},
		{
			desc:              "Double wrapped googleapi request error",
			err:               fmt.Errorf("%w: %w", requestError, errors.New("wrapped error")),
			endpointOperation: attachOp,
			expectErr:         negtypes.ErrInvalidEPAttach,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			endpointBatchErr := checkEndpointBatchErr(tc.err, tc.endpointOperation)
			if !errors.Is(endpointBatchErr, tc.expectErr) {
				t.Errorf("checkEndpointBatchErr() = %v, expected %v", endpointBatchErr, tc.expectErr)
			}
		})
	}
}

func TestGetEndpointPodLabelMap(t *testing.T) {
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()
	for i := 1; i <= 10; i++ {
		podLister.Add(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testServiceNamespace,
				Name:      fmt.Sprintf("pod-%s-%d", testInstance1, i),
				Labels: map[string]string{
					"foo-key": "foo",
					"bar-key": "bar",
				},
			},
		})
	}
	for i := 1; i <= 10; i++ {
		podLister.Add(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testServiceNamespace,
				Name:      fmt.Sprintf("pod-%s-%d", testInstance2, i),
				Labels: map[string]string{
					"foo-key": "foo",
					"bar-key": "bar",
				},
			},
		})
	}

	lpConfig := labels.PodLabelPropagationConfig{
		Labels: []labels.Label{
			{
				Key:               "foo-key",
				MaxLabelSizeBytes: 30,
			},
			{
				Key:               "bar-key",
				MaxLabelSizeBytes: 30,
			},
		},
	}

	var (
		endpointSet1, endpointPodMap1 = generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
		endpointSet2, endpointPodMap2 = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
		endpointSet3, endpointPodMap3 = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
	)

	for _, tc := range []struct {
		desc   string
		input  func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap)
		expect func() labels.EndpointPodLabelMap
	}{
		{
			desc: "empty inputs",
			input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				return map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{}, negtypes.EndpointPodMap{}
			},
			expect: func() labels.EndpointPodLabelMap {
				return labels.EndpointPodLabelMap{}
			},
		},
		{
			desc: "Add endpoints in diferent zones",
			input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(endpointSet1),
					{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(endpointSet2),
				}
				retMap := negtypes.EndpointPodMap{}
				retMap = unionEndpointMap(retMap, endpointPodMap1)
				retMap = unionEndpointMap(retMap, endpointPodMap2)
				return retSet, retMap
			},
			expect: func() labels.EndpointPodLabelMap {
				endpointSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(endpointSet1),
					testZone2: negtypes.NewNetworkEndpointSet().Union(endpointSet2),
				}
				return generateEndpointPodLabelMap(endpointSet, labels.PodLabelMap{
					"foo-key": "foo",
					"bar-key": "bar",
				})
			},
		},
		{
			desc: "Add endpoints in diferent zones with pod not found",
			input: func() (map[negtypes.NEGLocation]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
					{Zone: testZone1}: negtypes.NewNetworkEndpointSet().Union(endpointSet1),
					{Zone: testZone2}: negtypes.NewNetworkEndpointSet().Union(endpointSet2).Union(endpointSet3),
				}
				retMap := negtypes.EndpointPodMap{}
				retMap = unionEndpointMap(retMap, endpointPodMap1)
				retMap = unionEndpointMap(retMap, endpointPodMap2)
				retMap = unionEndpointMap(retMap, endpointPodMap3)
				return retSet, retMap
			},
			expect: func() labels.EndpointPodLabelMap {
				endpointSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(endpointSet1),
					testZone2: negtypes.NewNetworkEndpointSet().Union(endpointSet2),
				}
				return generateEndpointPodLabelMap(endpointSet, labels.PodLabelMap{
					"foo-key": "foo",
					"bar-key": "bar",
				})
			},
		},
	} {
		endpoints, endpointPodMap := tc.input()
		expectMap := tc.expect()
		endpointPodLabelMap := getEndpointPodLabelMap(endpoints, endpointPodMap, podLister, lpConfig, nil, klog.TODO(), testContext.NegMetrics)
		if diff := cmp.Diff(endpointPodLabelMap, expectMap); diff != "" {
			t.Errorf("For test case %s: got endpointPodLabelMap %+v, want %+v, diff %s", tc.desc, endpointPodLabelMap, expectMap, diff)
		}
	}
}

func TestCollectLabelStats(t *testing.T) {
	t.Parallel()

	testIP1 := "1.2.3.4"
	testIP2 := "1.2.3.5"
	testIP3 := "1.2.3.6"
	testIP4 := "1.2.3.7"
	testPort := int64(80)
	endpoint1 := negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))}
	endpoint2 := negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance2, Port: strconv.Itoa(int(testPort))}
	endpoint3 := negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))}
	endpoint4 := negtypes.NetworkEndpoint{IP: testIP4, Node: negtypes.TestInstance4, Port: strconv.Itoa(int(testPort))}

	for _, tc := range []struct {
		desc              string
		curLabelMap       labels.EndpointPodLabelMap
		addLabelMap       labels.EndpointPodLabelMap
		targetEndpointMap map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		expect            metricscollector.LabelPropagationStats
	}{
		{
			desc:              "Empty inputs",
			curLabelMap:       labels.EndpointPodLabelMap{},
			addLabelMap:       labels.EndpointPodLabelMap{},
			targetEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			expect: metricscollector.LabelPropagationStats{
				EndpointsWithAnnotation: 0,
				NumberOfEndpoints:       0,
			},
		},
		{
			desc: "No new endpoints to be added",
			curLabelMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			addLabelMap: labels.EndpointPodLabelMap{},
			targetEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
			},
			expect: metricscollector.LabelPropagationStats{
				EndpointsWithAnnotation: 1,
				NumberOfEndpoints:       2,
			},
		},
		{
			desc: "Some endpoints to be added",
			curLabelMap: labels.EndpointPodLabelMap{
				endpoint1: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			addLabelMap: labels.EndpointPodLabelMap{
				endpoint3: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			targetEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone1}: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
			},
			expect: metricscollector.LabelPropagationStats{
				EndpointsWithAnnotation: 2,
				NumberOfEndpoints:       4,
			},
		},
		{
			desc:        "Only newly added endpoints",
			curLabelMap: labels.EndpointPodLabelMap{},
			addLabelMap: labels.EndpointPodLabelMap{
				endpoint3: labels.PodLabelMap{
					"foo": "bar",
				},
			},
			targetEndpointMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: testZone2}: negtypes.NewNetworkEndpointSet(
					endpoint3,
					endpoint4,
				),
			},
			expect: metricscollector.LabelPropagationStats{
				EndpointsWithAnnotation: 1,
				NumberOfEndpoints:       2,
			},
		},
	} {
		out := collectLabelStats(tc.curLabelMap, tc.addLabelMap, tc.targetEndpointMap)
		if diff := cmp.Diff(out, tc.expect); diff != "" {
			t.Errorf("For test case %s: (-want +got): \n%s", tc.desc, diff)
		}
	}
}

func TestGetNonDefaultSubnetNEGName(t *testing.T) {
	t.Parallel()
	fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE, negtypes.NewTestContext().NegMetrics)
	testNegTypes := []negtypes.NetworkEndpointType{
		negtypes.VmIpEndpointType,
		negtypes.VmIpPortEndpointType,
	}

	testCases := []struct {
		desc              string
		customNEGName     string
		expectedL4NegName string
		expectedL7NegName string
		expectL4Error     bool
		expectL7Error     bool
	}{
		{
			desc:              "auto-generated NEG name",
			expectedL4NegName: "k8s2-s7nrwkif-test-ns-test-name-cc51aa-qvmwlr7g",
			expectedL7NegName: "k8s1-clusteri-test-ns-test-name-80-cc51aa-137ee03a",
			expectL4Error:     false,
			expectL7Error:     false,
		},
		{
			desc:              "custom NEG name not exceeding character limit",
			customNEGName:     "custom-neg",
			expectedL7NegName: "custom-neg-cc51aa",
			expectL4Error:     true,
			expectL7Error:     false,
		},
		{
			desc:          " custom NEG name exceeding character limit",
			customNEGName: "012345678901234567890123456789012345678901234567890123456", // 57 characters
			expectL4Error: true,
			expectL7Error: true,
		},
	}

	for _, testNegType := range testNegTypes {
		for _, tc := range testCases {
			t.Run(tc.desc, func(t *testing.T) {
				_, syncer, err := newTestTransactionSyncer(fakeCloud, testNegType, tc.customNEGName != "")
				if err != nil {
					t.Fatalf("failed to initialize transaction syncer: %v", err)
				}
				if tc.customNEGName != "" {
					syncer.NegSyncerKey.NegName = tc.customNEGName
				}
				got, err := syncer.getNonDefaultSubnetNEGName(additionalTestSubnet)
				t.Logf("NEG name: %q, custom Name: %v", syncer.NegSyncerKey.NegName, syncer.customName)
				if err == nil {
					if testNegType == negtypes.VmIpEndpointType && tc.expectL4Error {
						t.Errorf("For NEG type %q, got err == nil, expected err != nil", testNegType)
					}
					if testNegType == negtypes.VmIpPortEndpointType && tc.expectL7Error {
						t.Errorf("For NEG type %q, got err == nil, expected err != nil", testNegType)
					}
				}
				if err != nil {
					if testNegType == negtypes.VmIpEndpointType && !tc.expectL4Error {
						t.Errorf("For NEG type %q, got err = %v, expected err == nil", testNegType, err)
					}
					if testNegType == negtypes.VmIpPortEndpointType && !tc.expectL7Error {
						t.Errorf("For NEG type %q, got err = %v, expected err == nil", testNegType, err)
					}
				}

				if testNegType == negtypes.VmIpEndpointType && !tc.expectL4Error && got != tc.expectedL4NegName {
					t.Errorf("For NEG type %q, got NEG name %q, expected %q", testNegType, got, tc.expectedL4NegName)
				}

				if testNegType == negtypes.VmIpPortEndpointType && !tc.expectL7Error && got != tc.expectedL7NegName {
					t.Errorf("For NEG type %q, got NEG name %q, expected %q", testNegType, got, tc.expectedL7NegName)
				}
			})
		}
	}
}

func TestSyncL4NEGs(t *testing.T) {
	testNodeIP1 := "1.2.3.1"
	testNodeIP2 := "1.2.3.2"
	testNodeIP3 := "1.2.3.3"
	testNodeIP4 := "1.2.3.4"
	testNodeIP5 := "1.2.3.5"

	testCases := []struct {
		desc                   string
		existingGCEEndpoints   map[negtypes.NEGLocation][]*composite.NetworkEndpoint
		endpointSlices         []*discovery.EndpointSlice
		inProgressTransactions map[negtypes.NetworkEndpoint]transactionEntry
		expectedEndpoints      map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		enableL4DetachCancel   bool
	}{
		{
			// test if the syncer can attach endpoints to L4 NEG.
			desc: "add endpoints",
			existingGCEEndpoints: map[negtypes.NEGLocation][]*composite.NetworkEndpoint{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: {{}},
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: {{}},
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: {{}},
			},
			endpointSlices: getDefaultEndpointSlices(),
			expectedEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP1, Node: negtypes.TestInstance1},
					negtypes.NetworkEndpoint{IP: testNodeIP2, Node: negtypes.TestInstance2},
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP3, Node: negtypes.TestInstance3},
					negtypes.NetworkEndpoint{IP: testNodeIP4, Node: negtypes.TestInstance4},
				),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
			},
		},
		{
			// test if the syncer can remove endpoints from L4 NEG.
			desc: "remove endpoints",
			existingGCEEndpoints: map[negtypes.NEGLocation][]*composite.NetworkEndpoint{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance1,
						IpAddress: testNodeIP1,
					},
					{
						Instance:  negtypes.TestInstance2,
						IpAddress: testNodeIP2,
					},
				},
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance3,
						IpAddress: testNodeIP3,
					},
				},
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: {
					{},
				},
			},
			endpointSlices: getTestEmptyEndpointSlices(testServiceName, testServiceNamespace),
			expectedEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
			},
		},
		{
			// test if the syncer can add and remove endpoints from L4 NEG at the same time.
			desc: "add/remove endpoints",
			existingGCEEndpoints: map[negtypes.NEGLocation][]*composite.NetworkEndpoint{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance1,
						IpAddress: testNodeIP1,
					},
				},
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance3,
						IpAddress: testNodeIP3,
					},
					{
						Instance:  negtypes.TestInstance5, // should be removed
						IpAddress: testNodeIP5,
					},
				},
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: {},
			},
			endpointSlices: getDefaultEndpointSlices(),
			expectedEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP1, Node: negtypes.TestInstance1},
					negtypes.NetworkEndpoint{IP: testNodeIP2, Node: negtypes.TestInstance2},
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP3, Node: negtypes.TestInstance3},
					negtypes.NetworkEndpoint{IP: testNodeIP4, Node: negtypes.TestInstance4},
				),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
			},
		},
		{
			// test if the syncer properly filters endpoints with active transactions.
			desc: "dont affect endpoints with active transactions",
			existingGCEEndpoints: map[negtypes.NEGLocation][]*composite.NetworkEndpoint{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance1,
						IpAddress: testNodeIP1,
					},
				},
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance3,
						IpAddress: testNodeIP3,
					},
					{
						Instance:  negtypes.TestInstance5, // should be removed but has a transaction
						IpAddress: testNodeIP5,
					},
				},
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: {},
			},
			endpointSlices: getDefaultEndpointSlices(),
			inProgressTransactions: map[negtypes.NetworkEndpoint]transactionEntry{
				{
					IP:   testNodeIP2,
					Node: negtypes.TestInstance2,
				}: {
					Operation: attachOp,
					Zone:      negtypes.TestZone1,
					Subnet:    defaultTestSubnet,
				},
				{
					IP:   testNodeIP5,
					Node: negtypes.TestInstance5,
				}: {
					Operation: detachOp,
					Zone:      negtypes.TestZone2,
					Subnet:    defaultTestSubnet,
				},
			},
			expectedEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP1, Node: negtypes.TestInstance1},
					//TestInstance2 has a running transaction so it should not be re-added
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP3, Node: negtypes.TestInstance3},
					negtypes.NetworkEndpoint{IP: testNodeIP4, Node: negtypes.TestInstance4},
					negtypes.NetworkEndpoint{IP: testNodeIP5, Node: negtypes.TestInstance5},
				),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
			},
		},
		{
			// check if the syncer can do a 'detach cancel' - if there is a 'detach' transaction for a node it can be re-attached
			desc:                 "allow detach cancel",
			enableL4DetachCancel: true,
			existingGCEEndpoints: map[negtypes.NEGLocation][]*composite.NetworkEndpoint{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance1,
						IpAddress: testNodeIP1,
					},
				},
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: {
					{
						Instance:  negtypes.TestInstance3,
						IpAddress: testNodeIP3,
					},
				},
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: {},
			},
			endpointSlices: getDefaultEndpointSlices(),
			inProgressTransactions: map[negtypes.NetworkEndpoint]transactionEntry{
				{
					IP:   testNodeIP2,
					Node: negtypes.TestInstance2,
				}: {
					Operation: detachOp,
					Zone:      negtypes.TestZone1,
					Subnet:    defaultTestSubnet,
				},
				{
					IP:   testNodeIP4,
					Node: negtypes.TestInstance4,
				}: {
					Operation: attachOp,
					Zone:      negtypes.TestZone2,
					Subnet:    defaultTestSubnet,
				},
			},
			expectedEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: negtypes.TestZone1, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP1, Node: negtypes.TestInstance1},
					negtypes.NetworkEndpoint{IP: testNodeIP2, Node: negtypes.TestInstance2}, // shoud be re-attached
				),
				{Zone: negtypes.TestZone2, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: testNodeIP3, Node: negtypes.TestInstance3},
					// instance4 should not be re-attached since attach is in progress in transactions
				),
				{Zone: negtypes.TestZone3, Subnet: defaultTestSubnet}: negtypes.NewNetworkEndpointSet(),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			nodeInformer := zonegetter.FakeNodeInformer()
			zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
			zoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
			if err != nil {
				t.Fatalf("failed to initialize zone getter: %v", err)
			}
			fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
			negtypes.MockNetworkEndpointAPIs(fakeGCE)
			fakeCloud := negtypes.NewAdapter(fakeGCE, negtypes.NewTestContext().NegMetrics)

			testEndpointMap := tc.existingGCEEndpoints
			if testEndpointMap == nil {
				testEndpointMap = make(map[negtypes.NEGLocation][]*composite.NetworkEndpoint)
			}

			// Create initial NetworkEndpointGroups in cloud
			var objRefs []negv1beta1.NegObjectReference
			for negLocation, endpoints := range testEndpointMap {
				fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testL4NegName, Version: meta.VersionGA, NetworkEndpointType: "GCE_VM_IP"}, negLocation.Zone, klog.TODO())
				if len(endpoints) > 0 {
					fakeCloud.AttachNetworkEndpoints(testL4NegName, negLocation.Zone, endpoints, meta.VersionGA, klog.TODO())
				}
				neg, err := fakeCloud.GetNetworkEndpointGroup(testL4NegName, negLocation.Zone, meta.VersionGA, klog.TODO())
				if err != nil {
					t.Fatalf("failed to get neg from fake cloud: %s", err)
				}

				objRefs = append(objRefs, negv1beta1.NegObjectReference{SelfLink: neg.SelfLink, SubnetURL: defaultTestSubnetURL, NetworkEndpointType: negv1beta1.VmIpEndpointType})
			}
			neg := &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNegName,
					Namespace: testServiceNamespace,
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: objRefs,
				},
			}
			testContext := negtypes.NewTestContext()
			testContext.NodeInformer = nodeInformer
			_, s, err := newTestTransactionSyncerWithCustomContext(fakeCloud, negtypes.VmIpEndpointType, false, testContext)
			if err != nil {
				t.Fatalf("failed to initialize transaction syncer: %v", err)
			}
			s.enableL4NEGDetachCancel = tc.enableL4DetachCancel
			s.needInit = false

			for _, eps := range tc.endpointSlices {
				s.endpointSliceLister.Add(eps)
			}
			s.svcNegLister.Add(neg)
			// mark syncer as started without starting the syncer routine
			(s.syncer.(*syncer)).stopped = false
			// add transactions to the syncer if any were defined in the test case
			if len(tc.inProgressTransactions) > 0 {
				for key, tr := range tc.inProgressTransactions {
					s.transactions.Put(key, tr)
				}
			}

			err = s.syncInternal()
			if err != nil {
				t.Errorf("syncInternal returned error: %v", err)
			}
			// give a little time for the syncer attach/detach goroutines to finish
			time.Sleep(50 * time.Millisecond)

			out, _, _, err := retrieveExistingZoneNetworkEndpointMap(map[string]string{defaultTestSubnet: testL4NegName}, zoneGetter, fakeCloud, meta.VersionGA, negtypes.L4LocalMode, false, klog.TODO(), s.negMetrics, false)
			if err != nil {
				t.Errorf("errored retrieving existing network endpoints: %v", err)
			}

			if !reflect.DeepEqual(tc.expectedEndpoints, out) {
				t.Errorf("endpoints were modified after syncInternal:\n got      %+v,\n expected %+v", out, tc.expectedEndpoints)
			}
		})
	}

}

func TestReAddDrainingEndpointsThatAreInTargetMap(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc              string
		addEndpoints      map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		targetMap         map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
		drainingEndpoints map[negtypes.NetworkEndpoint]string
		expectEndpoints   map[negtypes.NEGLocation]negtypes.NetworkEndpointSet
	}{
		{
			desc:              "empty inputs",
			addEndpoints:      map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			targetMap:         map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			drainingEndpoints: map[negtypes.NetworkEndpoint]string{},
			expectEndpoints:   map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
		},
		{
			desc: "endpoint in targetMap and drainingEndpoints, health is not HEALTHY",
			addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(),
			},
			targetMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.1.1.1", Node: "node1"}),
			},
			drainingEndpoints: map[negtypes.NetworkEndpoint]string{
				{IP: "1.1.1.1", Node: "node1"}: "DRAINING",
			},
			expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.1.1.1", Node: "node1"}),
			},
		},
		{
			desc: "endpoint in targetMap but not in drainingEndpoints",
			addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(),
			},
			targetMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.1.1.1", Node: "node1"}),
			},
			drainingEndpoints: map[negtypes.NetworkEndpoint]string{},
			expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(),
			},
		},
		{
			desc:         "endpoint in targetMap and currentMap (so not in addEndpoints), but is draining",
			addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{},
			targetMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.1.1.1", Node: "node1"}),
			},
			drainingEndpoints: map[negtypes.NetworkEndpoint]string{
				{IP: "1.1.1.1", Node: "node1"}: "DRAINING",
			},
			expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.1.1.1", Node: "node1"}),
			},
		},
		{
			desc: "multiple endpoints in same zoneSubnet, some draining, some not",
			addEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(negtypes.NetworkEndpoint{IP: "1.1.1.2", Node: "node2"}),
			},
			targetMap: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "1.1.1.1", Node: "node1"},
					negtypes.NetworkEndpoint{IP: "1.1.1.2", Node: "node2"},
					negtypes.NetworkEndpoint{IP: "1.1.1.3", Node: "node3"},
				),
			},
			drainingEndpoints: map[negtypes.NetworkEndpoint]string{
				{IP: "1.1.1.1", Node: "node1"}: "DRAINING",
			},
			expectEndpoints: map[negtypes.NEGLocation]negtypes.NetworkEndpointSet{
				{Zone: "zone1", Subnet: "subnet1"}: negtypes.NewNetworkEndpointSet(
					negtypes.NetworkEndpoint{IP: "1.1.1.1", Node: "node1"},
					negtypes.NetworkEndpoint{IP: "1.1.1.2", Node: "node2"},
				),
			},
		},
	}

	for _, tc := range testCases {
		reAddDrainingEndpointsThatAreInTargetMap(tc.addEndpoints, tc.targetMap, tc.drainingEndpoints, klog.TODO())
		if !reflect.DeepEqual(tc.addEndpoints, tc.expectEndpoints) {
			t.Errorf("For case %q, expect addEndpoints to be %+v, but got %+v", tc.desc, tc.expectEndpoints, tc.addEndpoints)
		}
	}
}

func newTestTransactionSyncer(fakeGCE negtypes.NetworkEndpointGroupCloud, negType negtypes.NetworkEndpointType, customName bool) (negtypes.NegSyncer, *transactionSyncer, error) {
	netInfo := network.NetworkInfo{IsDefault: true, NetworkURL: fakeGCE.NetworkURL(), SubnetworkURL: fakeGCE.SubnetworkURL()}
	return newCustomTestTransactionSyncer(fakeGCE, negType, customName, zonegetter.FakeNodeTopologyInformer(), netInfo, nil)
}

func newTestTransactionSyncerWithNetInfo(fakeGCE negtypes.NetworkEndpointGroupCloud, negType negtypes.NetworkEndpointType, customName bool, netInfo network.NetworkInfo) (negtypes.NegSyncer, *transactionSyncer, error) {
	return newCustomTestTransactionSyncer(fakeGCE, negType, customName, zonegetter.FakeNodeTopologyInformer(), netInfo, nil)
}

func newTestTransactionSyncerWithTopologyInformer(fakeGCE negtypes.NetworkEndpointGroupCloud, negType negtypes.NetworkEndpointType, customName bool, nodeTopologyInformer cache.SharedIndexInformer) (negtypes.NegSyncer, *transactionSyncer, error) {
	netInfo := network.NetworkInfo{IsDefault: true, NetworkURL: fakeGCE.NetworkURL(), SubnetworkURL: fakeGCE.SubnetworkURL()}
	return newCustomTestTransactionSyncer(fakeGCE, negType, customName, nodeTopologyInformer, netInfo, nil)
}

func newTestTransactionSyncerWithCustomContext(fakeGCE negtypes.NetworkEndpointGroupCloud, negType negtypes.NetworkEndpointType, customName bool, customTestContext *negtypes.TestContext) (negtypes.NegSyncer, *transactionSyncer, error) {
	netInfo := network.NetworkInfo{IsDefault: true, NetworkURL: fakeGCE.NetworkURL(), SubnetworkURL: fakeGCE.SubnetworkURL()}
	return newCustomTestTransactionSyncer(fakeGCE, negType, customName, zonegetter.FakeNodeTopologyInformer(), netInfo, customTestContext)
}

func newCustomTestTransactionSyncer(fakeGCE negtypes.NetworkEndpointGroupCloud, negType negtypes.NetworkEndpointType, customName bool, nodeTopologyInformer cache.SharedIndexInformer, netInfo network.NetworkInfo, customTestContext *negtypes.TestContext) (negtypes.NegSyncer, *transactionSyncer, error) {
	var testContext *negtypes.TestContext
	if customTestContext != nil {
		testContext = customTestContext
	} else {
		testContext = negtypes.NewTestContext()
	}

	svcPort := negtypes.NegSyncerKey{
		Namespace: testServiceNamespace,
		Name:      testServiceName,
		NegType:   negType,
		PortTuple: negtypes.SvcPortTuple{
			Port:       80,
			TargetPort: "8080",
		},
		NegName: testNegName,
	}

	var mode negtypes.EndpointsCalculatorMode
	if negType == negtypes.VmIpEndpointType {
		svcPort.NegName = testL4NegName
		svcPort.PortTuple.Port = 0
		svcPort.PortTuple.TargetPort = ""
		svcPort.PortTuple.Name = string(negtypes.VmIpEndpointType)
		mode = negtypes.L4LocalMode
		svcPort.EpCalculatorMode = mode
	}

	// TODO(freehan): use real readiness reflector
	reflector := &readiness.NoopReflector{}
	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	fakeZoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, nodeTopologyInformer, defaultTestSubnetURL, false)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize zone getter: %v", err)
	}

	negNamer := testContext.NegNamer
	if svcPort.NegType == negtypes.VmIpEndpointType {
		negNamer = testContext.L4Namer
	}

	negsyncer := NewTransactionSyncer(svcPort,
		record.NewFakeRecorder(100),
		fakeGCE,
		fakeZoneGetter,
		testContext.PodInformer.GetIndexer(),
		testContext.ServiceInformer.GetIndexer(),
		testContext.EndpointSliceInformer.GetIndexer(),
		testContext.NodeInformer.GetIndexer(),
		testContext.SvcNegInformer.GetIndexer(),
		reflector,
		GetEndpointsCalculator(testContext.PodInformer.GetIndexer(), testContext.NodeInformer.GetIndexer(), testContext.ServiceInformer.GetIndexer(),
			fakeZoneGetter, svcPort, mode, klog.TODO(), testContext.EnableDualStackNEG, metricscollector.FakeSyncerMetrics(), &network.NetworkInfo{IsDefault: true, SubnetworkURL: test.DefaultTestSubnetURL}, negtypes.L4InternalLB, testContext.NegMetrics),
		string(kubeSystemUID),
		testContext.SvcNegClient,
		metricscollector.FakeSyncerMetrics(),
		customName,
		klog.TODO(),
		labels.PodLabelPropagationConfig{},
		testContext.EnableDualStackNEG,
		netInfo,
		negNamer,
		testContext.NegMetrics,
	)
	transactionSyncer := negsyncer.(*syncer).core.(*transactionSyncer)
	indexers := map[string]cache.IndexFunc{
		endpointslices.EndpointSlicesByServiceIndex: endpointslices.EndpointSlicesByServiceFunc,
	}
	transactionSyncer.endpointSliceLister.AddIndexers(indexers)
	return negsyncer, transactionSyncer, nil
}

func generateTransaction(table networkEndpointTransactionTable, entry transactionEntry, initialIp net.IP, num int, instance string, targetPort string) {
	endpointSet := generateEndpointSet(initialIp, num, instance, targetPort)
	for _, encodedEndpoint := range endpointSet.List() {
		table.Put(encodedEndpoint, entry)
	}
}

func generateEndpointSet(initialIp net.IP, num int, instance string, targetPort string) negtypes.NetworkEndpointSet {
	ret, _ := generateEndpointSetAndMap(initialIp, num, instance, targetPort)
	return ret
}

func generateEndpointSetAndMap(initialIp net.IP, num int, instance string, targetPort string) (negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
	retSet := negtypes.NewNetworkEndpointSet()
	retMap := negtypes.EndpointPodMap{}
	ip := initialIp.To4()
	for i := 1; i <= num; i++ {
		if i%256 == 0 {
			ip[2]++
		}
		ip[3]++

		endpoint := negtypes.NetworkEndpoint{IP: ip.String(), Node: instance, Port: targetPort}
		retSet.Insert(endpoint)
		retMap[endpoint] = types.NamespacedName{Namespace: testServiceNamespace, Name: fmt.Sprintf("pod-%s-%d", instance, i)}
	}
	return retSet, retMap
}

func generateEndpointPodLabelMap(endpointSet map[string]negtypes.NetworkEndpointSet, podLabelMap labels.PodLabelMap) labels.EndpointPodLabelMap {
	endpointPodLabelMap := labels.EndpointPodLabelMap{}
	for _, endpoints := range endpointSet {
		for endpoint := range endpoints {
			endpointPodLabelMap[endpoint] = podLabelMap
		}
	}
	return endpointPodLabelMap
}

func unionEndpointMap(m1, m2 negtypes.EndpointPodMap) negtypes.EndpointPodMap {
	for k, v := range m2 {
		m1[k] = v
	}
	return m1
}

func generateEndpointBatch(endpointSet negtypes.NetworkEndpointSet, endpointPodLabelMap labels.EndpointPodLabelMap) map[negtypes.NetworkEndpoint]*composite.NetworkEndpoint {
	ret, _ := makeEndpointBatch(endpointSet, negtypes.VmIpPortEndpointType, endpointPodLabelMap, klog.TODO())
	return ret
}

type testSyncer struct {
	*syncer
	SyncCount int
}

func (s *testSyncer) Sync() bool {
	s.SyncCount++
	return s.syncer.Sync()
}

type testRetryHandler struct {
	ts         *testSyncer
	RetryCount int
}

func (r *testRetryHandler) Retry() error {
	r.RetryCount++
	r.ts.Sync()
	return nil
}

func (r *testRetryHandler) Reset() {
}

// negMeta references a GCE NEG resource
type negMeta struct {
	SyncerKey negtypes.NegSyncerKey
	// Name is the name of the NEG
	Name string
	// Zone is the zone of the NEG resource
	Zone string
}

type testReflector struct {
	*readiness.NoopReflector
	keys     []negtypes.NegSyncerKey
	negNames []string

	pollMap map[negMeta]negtypes.EndpointPodMap
}

func (tr *testReflector) Flush() {
	tr.keys = []negtypes.NegSyncerKey{}
	tr.negNames = []string{}
	tr.pollMap = make(map[negMeta]negtypes.EndpointPodMap)
}

func (tr *testReflector) CommitPods(syncerKey negtypes.NegSyncerKey, negName string, zone string, endpointMap negtypes.EndpointPodMap) {
	tr.keys = append(tr.keys, syncerKey)
	tr.negNames = append(tr.negNames, negName)
	key := negMeta{
		SyncerKey: syncerKey,
		Name:      negName,
		Zone:      zone,
	}
	tr.pollMap[key] = endpointMap
}

func validateTransactionTableEquality(t *testing.T, desc string, table, expectTable networkEndpointTransactionTable) {
	for _, key := range table.Keys() {
		expectEntry, ok := expectTable.Get(key)
		if !ok {
			t.Errorf("For test case %q, do not endpointSets key %q to exists", desc, key)
			continue
		}
		gotEntry, _ := table.Get(key)
		if !reflect.DeepEqual(expectEntry, gotEntry) {
			t.Errorf("For test case %q, expectEntry of key %q to be %v, but got %v", desc, key, expectEntry, gotEntry)
		}
	}

	// Check if there are missing entries that expected to exist in output
	for _, key := range expectTable.Keys() {
		_, ok := table.Get(key)
		if !ok {
			t.Errorf("For test case %q, endpointSets transaction key %q to exists, but got nil", desc, key)
			continue
		}
	}
}

// waitForTransactions waits for transactions to be completed
func waitForTransactions(syncer *transactionSyncer) error {
	return wait.PollImmediate(time.Microsecond, 5*time.Second, func() (bool, error) {
		if len(syncer.transactions.Keys()) == 0 {
			return true, nil
		}
		return false, nil
	})
}

// negObjectReferences returns objectReferences for NEG CRs from NEG Objects.
// The returned map uses the NEG selflink as a key and the NegObjRef as the value
func negObjectReferences(cloud negtypes.NetworkEndpointGroupCloud, state negv1beta1.NegState, zones sets.String, negName string) (map[string]negv1beta1.NegObjectReference, error) {
	negObjs := make(map[string]negv1beta1.NegObjectReference)
	for zone := range zones {
		neg, err := cloud.GetNetworkEndpointGroup(negName, zone, meta.VersionGA, klog.TODO())
		if err != nil {
			return nil, err
		}
		negRef := getNegObjectReference(neg, state)
		negObjs[neg.SelfLink] = negRef
	}
	return negObjs, nil
}

// getNegObjectReference returns objectReference for NEG CRs from NEG Object
func getNegObjectReference(neg *composite.NetworkEndpointGroup, negState negv1beta1.NegState) negv1beta1.NegObjectReference {
	negRef := negv1beta1.NegObjectReference{
		Id:                  fmt.Sprint(neg.Id),
		SelfLink:            neg.SelfLink,
		NetworkEndpointType: negv1beta1.NetworkEndpointType(neg.NetworkEndpointType),
	}
	if flags.F.EnableMultiSubnetClusterPhase1 {
		negRef.State = negState
		negRef.SubnetURL = neg.Subnetwork
	}
	return negRef
}

// checks the NEG Description on the cloud NEG Object and verifies with expected
// description from the syncer.
func checkNegDescription(t *testing.T, syncer *transactionSyncer, desc string) {
	expectedNegDesc := utils.NegDescription{
		ClusterUID:  syncer.kubeSystemUID,
		Namespace:   syncer.NegSyncerKey.Namespace,
		ServiceName: syncer.NegSyncerKey.Name,
		Port:        fmt.Sprint(syncer.NegSyncerKey.PortTuple.Port),
	}
	actualNegDesc, err := utils.NegDescriptionFromString(desc)
	if err != nil {
		t.Errorf("Invalid neg description: %s", err)
	}

	if !reflect.DeepEqual(*actualNegDesc, expectedNegDesc) {
		t.Errorf("Unexpected neg description %s, expected %s", desc, expectedNegDesc.String())
	}
}

// checkCondition looks for the condition of the specified type and validates it has the expectedStatus.
// It will also validate that the transition timestamp is updated as expected, which is specified by expectTransitionTSUpdate.
func checkCondition(t *testing.T, conditions []negv1beta1.Condition, conditionType string, previousTS metav1.Time, expectedStatus corev1.ConditionStatus, expectTransitionTSUpdate bool) metav1.Time {
	var condition negv1beta1.Condition
	found := false
	for _, c := range conditions {
		if c.Type == conditionType {
			found = true
			condition = c
			break
		}
	}

	if !found {
		t.Errorf("conditions did not include a condition for type %s", conditionType)
		return metav1.Time{}
	}

	if condition.Status != expectedStatus {
		t.Errorf("condition %s status should be set to %+v but was %+v", conditionType, expectedStatus, condition.Status)
	}

	if expectTransitionTSUpdate && !previousTS.Before(&condition.LastTransitionTime) {
		t.Errorf("condition %s LastTransitionTime should have been updated", conditionType)
	} else if !expectTransitionTSUpdate && !previousTS.Equal(&condition.LastTransitionTime) {
		t.Errorf("condition %s LastTransitionTime should not have been updated", conditionType)

	}

	if condition.Reason == "" {
		t.Errorf("condition %s cannot have an empty reason", conditionType)
	}

	if condition.Message == "" && expectedStatus != corev1.ConditionTrue {
		t.Errorf("condition %s cannot have an empty message", conditionType)
	} else if condition.Message != "" && expectedStatus == corev1.ConditionTrue {
		t.Errorf("condition %s should not have a message since status is ConditionTrue", conditionType)
	}
	return condition.LastTransitionTime
}

// createNegCR generates a NegCR with given neg name, and if statusPopulated is true, conditions are initialized in the ConditionTrue state
func createNegCR(testNegName string, creationTS metav1.Time, populateInitialized, populateSynced bool, negRefs []negv1beta1.NegObjectReference) *negv1beta1.ServiceNetworkEndpointGroup {

	neg := &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:              testNegName,
			Namespace:         testServiceNamespace,
			CreationTimestamp: creationTS,
		},
	}

	var conditions []negv1beta1.Condition
	if populateInitialized {
		conditions = append(conditions, negv1beta1.Condition{
			Type:               negv1beta1.Initialized,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: creationTS,
			Reason:             negtypes.NegInitializationSuccessful,
		})
	}
	if populateSynced {
		conditions = append(conditions, negv1beta1.Condition{
			Type:               negv1beta1.Synced,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: creationTS,
			Reason:             negtypes.NegInitializationSuccessful,
		})
	}

	neg.Status.Conditions = conditions
	neg.Status.NetworkEndpointGroups = negRefs

	return neg
}

// checkCRParams is to be used with checkNegCRWithParams
type checkCRParams struct {
	negCR                *negv1beta1.ServiceNetworkEndpointGroup //required
	previousLastSyncTime metav1.Time                             //required
	// previousZones contains all the active and inactive zones that previously existed. - optional
	// activeZones contains the current active zones - required
	// inactiveZones contains the current inactive zones - optional
	previousZones, activeZones, inactiveZones sets.String
	// previousSubnets are the subnets that existed during the last sync sync - optional
	// currentSubnets are the current subnets that exist. - required
	previousSubnets, currentSubnets              []nodetopologyv1.SubnetConfig
	expectPopulatedNegRefs, expectSyncTimeUpdate bool // required
	// subnetToNegName must include the default Subnet to default NEG name
	subnetToNegName map[nodetopologyv1.SubnetConfig]string // required
}

// checkNegCR validates the NegObjectReferences and the LastSyncTime.
// It will not validate the conditions fields but ensures at most 2 conditions exist.
// All fields in the params object are required when testing with multiple subnets
func checkNegCRWithParams(t *testing.T, cloud negtypes.NetworkEndpointGroupCloud, params checkCRParams) {
	t.Helper()
	if params.expectSyncTimeUpdate && !params.previousLastSyncTime.Before(&params.negCR.Status.LastSyncTime) {
		t.Errorf("Expected Neg CR to have an updated LastSyncTime")
	} else if !params.expectSyncTimeUpdate && !params.negCR.Status.LastSyncTime.IsZero() && !params.previousLastSyncTime.Equal(&params.negCR.Status.LastSyncTime) {
		t.Errorf("Expected Neg CR to not have an updated LastSyncTime")
	}

	var expectedNegRefs map[string]negv1beta1.NegObjectReference
	if params.expectPopulatedNegRefs {
		expectedNegRefs = generateExpectedNegObjReferences(t, cloud, params.previousSubnets, params.currentSubnets, params.previousZones, params.activeZones, params.inactiveZones, params.subnetToNegName)
	}

	var foundNegObjs []string
	if len(params.negCR.Status.NetworkEndpointGroups) != len(expectedNegRefs) {
		t.Errorf("Expected Neg CR to have %d corresponding neg object references, but has %d", len(expectedNegRefs), len(params.negCR.Status.NetworkEndpointGroups))
	}

	for _, negObj := range params.negCR.Status.NetworkEndpointGroups {
		if expectedObj, ok := expectedNegRefs[negObj.SelfLink]; ok {
			foundNegObjs = append(foundNegObjs, negObj.SelfLink)
			if negObj != expectedObj {
				t.Errorf("Expected Neg Object %+v to be %+v", negObj, expectedObj)
			}
		} else {
			t.Errorf("Unexpected neg object in Neg CR: %+v", negObj)
		}
	}

	if len(foundNegObjs) != len(expectedNegRefs) {
		t.Errorf("Expected to have %d neg objects, but only found these negs %+v ", len(expectedNegRefs), foundNegObjs)
	}

	if len(params.negCR.Status.Conditions) > 2 {
		t.Errorf("Expected to have at most 2 conditions, found %d", len(params.negCR.Status.Conditions))
	}
}

// checkNegCR validates the NegObjectReferences and the LastSyncTime. It will not validate the
// conditions fields but ensures at most 2 conditions exist. checkNegCR should only be used when
// only the default subnet is used. The NegObjRefs will be validated that NEGs exist in all
// provided zones and that those in the active zones are in the ACTIVE state while those in the
// inactive zones are in the INACTIVE state.
func checkNegCR(t *testing.T, negCR *negv1beta1.ServiceNetworkEndpointGroup, previousLastSyncTime metav1.Time, activeZones, inactiveZones sets.String, expectPopulatedNegRefs, expectSyncTimeUpdate bool, cloud negtypes.NetworkEndpointGroupCloud) {
	t.Helper()

	defaultSubnet, err := nodetopology.SubnetConfigFromSubnetURL(cloud.SubnetworkURL())
	if err != nil {
		t.Fatal("failed to generate the default subnet config from subnetwork url")
	}

	defaultSubnetConfig := []nodetopologyv1.SubnetConfig{defaultSubnet}
	subnetToNegName := map[nodetopologyv1.SubnetConfig]string{
		defaultSubnet: negCR.Name,
	}

	params := checkCRParams{
		negCR:                  negCR,
		previousLastSyncTime:   previousLastSyncTime,
		activeZones:            activeZones,
		inactiveZones:          inactiveZones,
		currentSubnets:         defaultSubnetConfig,
		expectPopulatedNegRefs: expectPopulatedNegRefs,
		expectSyncTimeUpdate:   expectSyncTimeUpdate,
		subnetToNegName:        subnetToNegName,
	}

	checkNegCRWithParams(t, cloud, params)
}

// generateExpectedNegObjReferences generates expected refs as follows
//   - NEGs from subnets that are in the originalSubnet but not in the current subnet are marked TO_BE_DELETED
//   - NEGs in inactive zones from current subnets are marked as INACTIVE
//   - NEGs in active zones from current subnets are marked as ACTIVE
func generateExpectedNegObjReferences(t *testing.T, cloud negtypes.NetworkEndpointGroupCloud, originalSubnets, currentSubnets []nodetopologyv1.SubnetConfig, originalZones, activeZones, inactiveZones sets.String, subnetToNegName map[nodetopologyv1.SubnetConfig]string) map[string]negv1beta1.NegObjectReference {
	t.Helper()

	expectedNegRefs := make(map[string]negv1beta1.NegObjectReference)
	originalSubnetMap := map[nodetopologyv1.SubnetConfig]struct{}{}
	// use the original subnets, original zones -> mark all as to be deleted
	for _, subnetConfig := range originalSubnets {
		negName := subnetToNegName[subnetConfig]
		ret, err := negObjectReferences(cloud, negv1beta1.ToBeDeletedState, originalZones, negName)
		if err != nil {
			t.Fatalf("Failed to get negObjRef: %v", err)
		}
		for k, v := range ret {
			expectedNegRefs[k] = v
		}
		originalSubnetMap[subnetConfig] = struct{}{}
	}

	// use current subnets inactive zones -> mark as inactive
	for _, subnetConfig := range currentSubnets {
		negName := subnetToNegName[subnetConfig]
		if _, exists := originalSubnetMap[subnetConfig]; exists {
			// If the subnet is new, then the NEGs should not be created in the inactive state
			ret, err := negObjectReferences(cloud, negv1beta1.InactiveState, inactiveZones, negName)
			if err != nil {
				t.Fatalf("Failed to get negObjRef: %v", err)
			}
			for k, v := range ret {
				expectedNegRefs[k] = v
			}
		}

		ret, err := negObjectReferences(cloud, negv1beta1.ActiveState, activeZones, negName)
		if err != nil {
			t.Fatalf("Failed to get negObjRef: %v", err)
		}
		for k, v := range ret {
			expectedNegRefs[k] = v
		}
	}

	return expectedNegRefs
}
