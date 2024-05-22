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
	context2 "context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"
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
)

func TestTransactionSyncNetworkEndpoints(t *testing.T) {
	t.Parallel()

	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE)
	testNegTypes := []negtypes.NetworkEndpointType{
		negtypes.VmIpEndpointType,
		negtypes.VmIpPortEndpointType,
	}

	for _, testNegType := range testNegTypes {
		_, transactionSyncer := newTestTransactionSyncer(fakeCloud, testNegType, false)
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
				t.Errorf("Unexpected neg type %q, expected %q", neg.Type, testNegType)
			}
			if neg.Description == "" {
				t.Errorf("Neg Description should be populated when NEG CRD is enabled")
			}
		}

		testCases := []struct {
			desc            string
			addEndpoints    map[string]negtypes.NetworkEndpointSet
			removeEndpoints map[string]negtypes.NetworkEndpointSet
			expectEndpoints map[string]negtypes.NetworkEndpointSet
		}{
			{
				"empty input",
				map[string]negtypes.NetworkEndpointSet{},
				map[string]negtypes.NetworkEndpointSet{},
				map[string]negtypes.NetworkEndpointSet{},
			},
			{
				"add some endpoints",
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{},
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				"remove some endpoints",
				map[string]negtypes.NetworkEndpointSet{},
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				"add duplicate endpoints",
				map[string]negtypes.NetworkEndpointSet{
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{},
				map[string]negtypes.NetworkEndpointSet{
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
			{
				"add and remove endpoints",
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
				},
			},
			{
				"add more endpoints",
				map[string]negtypes.NetworkEndpointSet{
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{},
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
			},
			{
				"add and remove endpoints in both zones",
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, targetPort)),
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, targetPort)),
				},
				map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, targetPort)),
					testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, targetPort)),
				},
			},
		}

		for _, tc := range testCases {
			// TODO(gauravkghildiyal): When the DualStack Migrator is fully
			// implemented, check if we need to cover scenarios where `migrationZone`
			// is not empty.
			err := transactionSyncer.syncNetworkEndpoints(tc.addEndpoints, tc.removeEndpoints, labels.EndpointPodLabelMap{}, "")
			if err != nil {
				t.Errorf("For case %q, syncNetworkEndpoints() got %v, want nil", tc.desc, err)
			}

			if err := waitForTransactions(transactionSyncer); err != nil {
				t.Errorf("For case %q, waitForTransactions() got %v, want nil", tc.desc, err)
			}

			for zone, endpoints := range tc.expectEndpoints {
				list, err := fakeCloud.ListNetworkEndpoints(transactionSyncer.NegSyncerKey.NegName, zone, false, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
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
					t.Errorf("For case %q, in zone %q, negType %q, endpointSets endpoints == %v, but got %v, difference: \n(want - got) = %v\n(got - want) = %v", tc.desc, zone, testNegType, endpoints, endpointSet, endpoints.Difference(endpointSet), endpointSet.Difference(endpoints))
				}
			}
		}
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone1, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone2, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone3, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		transactionSyncer.cloud.DeleteNetworkEndpointGroup(transactionSyncer.NegName, negtypes.TestZone4, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
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
		addEndpoints            map[string]negtypes.NetworkEndpointSet
		endpointPodLabelMap     labels.EndpointPodLabelMap
		expectedNEAnnotation    labels.PodLabelMap
	}{
		{
			"empty input",
			true,
			negtypes.VmIpPortEndpointType,
			map[string]negtypes.NetworkEndpointSet{},
			labels.EndpointPodLabelMap{},
			nil,
		},
		{
			"add L4 endpoints with label map populated",
			true,
			negtypes.VmIpEndpointType,
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet1).Union(l4EndpointSet2),
				testZone2: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet3).Union(l4EndpointSet4),
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet1).Union(l4EndpointSet2),
				testZone2: negtypes.NewNetworkEndpointSet().Union(l4EndpointSet3).Union(l4EndpointSet4),
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
				testZone2: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
				testZone2: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet1).Union(l7EndpointSet2),
				testZone2: negtypes.NewNetworkEndpointSet().Union(l7EndpointSet3).Union(l7EndpointSet4),
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
		fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
		negtypes.MockNetworkEndpointAPIs(fakeGCE)
		fakeCloud := negtypes.NewAdapter(fakeGCE)
		_, transactionSyncer := newTestTransactionSyncer(fakeCloud, tc.negType, false)
		if err := transactionSyncer.ensureNetworkEndpointGroups(); err != nil {
			t.Errorf("Expect error == nil, but got %v", err)
		}
		err := transactionSyncer.syncNetworkEndpoints(tc.addEndpoints, map[string]negtypes.NetworkEndpointSet{}, tc.endpointPodLabelMap, "")
		if err != nil {
			t.Errorf("For case %q, syncNetworkEndpoints() got %v, want nil", tc.desc, err)
		}
		if err := waitForTransactions(transactionSyncer); err != nil {
			t.Errorf("For case %q, waitForTransactions() got %v, want nil", tc.desc, err)
		}

		for zone := range tc.addEndpoints {
			list, err := fakeCloud.ListNetworkEndpoints(transactionSyncer.NegSyncerKey.NegName, zone, false, transactionSyncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
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
	s, transactionSyncer := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), negtypes.VmIpPortEndpointType, false)
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
		},
	}

	for _, tc := range testCases {
		transactionSyncer.transactions = tc.table()
		transactionSyncer.commitTransaction(tc.err, tc.endpointMap)
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
		endpointMap       map[string]negtypes.NetworkEndpointSet
		table             func() networkEndpointTransactionTable
		expectEndpointMap map[string]negtypes.NetworkEndpointSet
	}{
		{
			"empty map and transactions",
			map[string]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[string]negtypes.NetworkEndpointSet{},
		},
		{
			"empty transactions",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"empty map",
			map[string]negtypes.NetworkEndpointSet{},
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add existing endpoints",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add non-existing endpoints",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")),
			},
		},
		{
			"remove existing endpoints",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation: detachOp,

					Zone: testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add non-existing endpoints and remove existing endpoints",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
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
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
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
		endpointMap       map[string]negtypes.NetworkEndpointSet
		table             func() networkEndpointTransactionTable
		expectEndpointMap map[string]negtypes.NetworkEndpointSet
	}{
		{
			"both empty",
			map[string]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[string]negtypes.NetworkEndpointSet{},
		},
		{
			"empty map",
			map[string]negtypes.NetworkEndpointSet{},
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
			map[string]negtypes.NetworkEndpointSet{},
		},
		{
			"empty transaction",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"empty transaction",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
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

func TestCommitPods(t *testing.T) {
	t.Parallel()

	_, transactionSyncer := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())), negtypes.VmIpPortEndpointType, false)
	reflector := &testReflector{}
	transactionSyncer.reflector = reflector

	for _, tc := range []struct {
		desc         string
		input        func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap)
		expectOutput func() map[string]negtypes.EndpointPodMap
	}{
		{
			desc: "empty input",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				return nil, nil
			},
			expectOutput: func() map[string]negtypes.EndpointPodMap { return map[string]negtypes.EndpointPodMap{} },
		},
		{
			desc: "10 endpoints from 1 instance in 1 zone",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return map[string]negtypes.NetworkEndpointSet{testZone1: endpointSet}, endpointMap
			},
			expectOutput: func() map[string]negtypes.EndpointPodMap {
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return map[string]negtypes.EndpointPodMap{testZone1: endpointMap}
			},
		},
		{
			desc: "40 endpoints from 4 instances in 2 zone",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet(),
					testZone2: negtypes.NewNetworkEndpointSet(),
				}
				retMap := negtypes.EndpointPodMap{}
				endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				return retSet, retMap
			},
			expectOutput: func() map[string]negtypes.EndpointPodMap {
				retMap := map[string]negtypes.EndpointPodMap{
					testZone1: {},
					testZone2: {},
				}
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retMap[testZone1] = unionEndpointMap(retMap[testZone1], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retMap[testZone1] = unionEndpointMap(retMap[testZone1], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retMap[testZone2] = unionEndpointMap(retMap[testZone2], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retMap[testZone2] = unionEndpointMap(retMap[testZone2], endpointMap)
				return retMap
			},
		},
		{
			desc: "40 endpoints from 4 instances in 2 zone, but half of the endpoints does not have corresponding pod mapping",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet(),
					testZone2: negtypes.NewNetworkEndpointSet(),
				}
				retMap := negtypes.EndpointPodMap{}

				endpointSet, _ := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)

				endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)

				endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)

				endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 5, testInstance4, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)
				return retSet, retMap
			},
			expectOutput: func() map[string]negtypes.EndpointPodMap {
				retMap := map[string]negtypes.EndpointPodMap{
					testZone1: {},
					testZone2: {},
				}
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				retMap[testZone1] = unionEndpointMap(retMap[testZone1], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				retMap[testZone1] = unionEndpointMap(retMap[testZone1], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				retMap[testZone2] = unionEndpointMap(retMap[testZone2], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 5, testInstance4, "8080")
				retMap[testZone2] = unionEndpointMap(retMap[testZone2], endpointMap)
				return retMap
			},
		},
		{
			desc: "40 endpoints from 4 instances in 2 zone, and more endpoints are in pod mapping",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet(),
					testZone2: negtypes.NewNetworkEndpointSet(),
				}
				retMap := negtypes.EndpointPodMap{}

				endpointSet, _ := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 15, testInstance1, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)

				endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 15, testInstance2, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)

				endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 15, testInstance3, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)

				endpointSet, _ = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 15, testInstance4, "8080")
				retMap = unionEndpointMap(retMap, endpointMap)
				return retSet, retMap
			},
			expectOutput: func() map[string]negtypes.EndpointPodMap {
				retMap := map[string]negtypes.EndpointPodMap{
					testZone1: {},
					testZone2: {},
				}
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retMap[testZone1] = unionEndpointMap(retMap[testZone1], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retMap[testZone1] = unionEndpointMap(retMap[testZone1], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retMap[testZone2] = unionEndpointMap(retMap[testZone2], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retMap[testZone2] = unionEndpointMap(retMap[testZone2], endpointMap)
				return retMap
			},
		},
		{
			desc: "40 endpoints from 4 instances in 2 zone, but some nodes do not have endpoint pod mapping",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet(),
					testZone2: negtypes.NewNetworkEndpointSet(),
				}
				retMap := negtypes.EndpointPodMap{}
				endpointSet, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				retSet[testZone1] = retSet[testZone1].Union(endpointSet)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				retMap = unionEndpointMap(retMap, endpointMap)
				endpointSet, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")
				retSet[testZone2] = retSet[testZone2].Union(endpointSet)
				return retSet, retMap
			},
			expectOutput: func() map[string]negtypes.EndpointPodMap {
				retMap := map[string]negtypes.EndpointPodMap{
					testZone1: {},
					testZone2: {},
				}
				_, endpointMap := generateEndpointSetAndMap(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				retMap[testZone1] = unionEndpointMap(retMap[testZone1], endpointMap)
				_, endpointMap = generateEndpointSetAndMap(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				retMap[testZone2] = unionEndpointMap(retMap[testZone2], endpointMap)
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

		if !reflect.DeepEqual(expectOutput, reflector.endpointMaps) {
			t.Errorf("For test case %q, expect endpoint map to be %v, but got %v", tc.desc, expectOutput, reflector.endpointMaps)
		}
	}
}

func TestTransactionSyncerWithNegCR(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testSubnetwork := cloud.ResourcePath("subnetwork", &meta.Key{Name: "test-subnetwork"})
	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
	testNegType := negtypes.VmIpPortEndpointType

	testCases := []struct {
		desc              string
		negExists         bool
		negDesc           string
		crStatusPopulated bool
		customName        bool
		expectErr         bool
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
			desc:              "Neg exists, custom name, without neg description",
			negExists:         true,
			negDesc:           "",
			crStatusPopulated: false,
			customName:        true,
			expectErr:         true,
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
			desc:      "Neg exists, with conflicting cluster id in neg description",
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
			desc:      "Neg exists, with conflicting namespace in neg description",
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
			desc:      "Neg exists, with conflicting service in neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: "service-2",
				Port:        "80",
			}.String(),
			crStatusPopulated: false,
			expectErr:         true,
		},
		{
			desc:      "Neg exists, with conflicting port in neg description",
			negExists: true,
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "81",
			}.String(),
			crStatusPopulated: false,
			expectErr:         true,
		},
		{
			desc:      "Neg exists, cr has populated status, but error during initialization",
			negExists: true,
			// Cause error by having a conflicting neg description
			negDesc: utils.NegDescription{
				ClusterUID:  kubeSystemUID,
				Namespace:   testServiceNamespace,
				ServiceName: testServiceName,
				Port:        "81",
			}.String(),
			crStatusPopulated: true,
			expectErr:         true,
		},
	}

	for _, tc := range testCases {
		_, syncer := newTestTransactionSyncer(fakeCloud, testNegType, tc.customName)
		negClient := syncer.svcNegClient
		t.Run(tc.desc, func(t *testing.T) {
			// fakeZoneGetter will list 3 zones for VM_IP_PORT NEGs.
			expectZones := sets.NewString(negtypes.TestZone1, negtypes.TestZone2, negtypes.TestZone4)

			var expectedNegRefs map[string]negv1beta1.NegObjectReference
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
				ret, _ := fakeCloud.AggregatedListNetworkEndpointGroup(syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
				expectedNegRefs = negObjectReferences(ret)
			}
			var refs []negv1beta1.NegObjectReference
			if tc.crStatusPopulated {
				for _, neg := range expectedNegRefs {
					refs = append(refs, neg)
				}
			}

			// Since timestamp gets truncated to the second, there is a chance that the timestamps will be the same as LastTransitionTime or LastSyncTime so use creation TS from an earlier date.
			creationTS := v1.Date(2020, time.July, 23, 0, 0, 0, 0, time.UTC)
			//Create NEG CR for Syncer to update status on
			origCR := createNegCR(testNegName, creationTS, tc.crStatusPopulated, tc.crStatusPopulated, refs)
			neg, err := negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context2.Background(), origCR, v1.CreateOptions{})
			if err != nil {
				t.Errorf("Failed to create test NEG CR: %s", err)
			}
			syncer.svcNegLister.Add(neg)

			err = syncer.ensureNetworkEndpointGroups()
			if !tc.expectErr && err != nil {
				t.Errorf("Expected no error, but got: %v", err)
			} else if tc.expectErr && err == nil {
				t.Errorf("Expected error, but got none")
			}

			negCR, err := negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context2.Background(), testNegName, v1.GetOptions{})
			if err != nil {
				t.Errorf("Failed to get NEG from neg client: %s", err)
			}
			ret, _ := fakeCloud.AggregatedListNetworkEndpointGroup(syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
			if len(expectedNegRefs) == 0 && !tc.expectErr {
				expectedNegRefs = negObjectReferences(ret)
			}
			// if error occurs, expect that neg object references are not populated
			if tc.expectErr && !tc.crStatusPopulated {
				expectedNegRefs = nil
			}

			checkNegCR(t, negCR, creationTS, expectZones, expectedNegRefs, false, tc.expectErr)
			if tc.expectErr {
				// If status is already populated, expect no change even when error occurs
				checkCondition(t, negCR.Status.Conditions, negv1beta1.Initialized, creationTS, corev1.ConditionFalse, true)
			} else if tc.crStatusPopulated {
				checkCondition(t, negCR.Status.Conditions, negv1beta1.Initialized, creationTS, corev1.ConditionTrue, false)
			} else {
				checkCondition(t, negCR.Status.Conditions, negv1beta1.Initialized, creationTS, corev1.ConditionTrue, true)
			}

			if tc.expectErr || tc.negExists {
				// Errored, so no expectation on created negs or negs were created beforehand
				return
			}

			// Verify the NEGs are created as expected
			retZones := sets.NewString()

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

		negClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Delete(context2.TODO(), testNegName, v1.DeleteOptions{})

		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone1, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone2, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone3, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
		syncer.cloud.DeleteNetworkEndpointGroup(testNegName, negtypes.TestZone4, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())

	}
}

func TestUpdateStatus(t *testing.T) {
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testSubnetwork := cloud.ResourcePath("subnetwork", &meta.Key{Name: "test-subnetwork"})
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
				fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
				_, syncer := newTestTransactionSyncer(fakeCloud, testNegType, false)
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

					_, err = fakeCloud.GetNetworkEndpointGroup(testNegName, testZone1, syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
					if err != nil {
						t.Errorf("failed to get neg from cloud: %s ", err)
					}
				}

				// Since timestamp gets truncated to the second, there is a chance that the timestamps will be the same as LastTransitionTime or LastSyncTime so use creation TS from an earlier date
				creationTS := v1.Date(2020, time.July, 23, 0, 0, 0, 0, time.UTC)
				origCR := createNegCR(testNegName, creationTS, tc.populateConditions[negv1beta1.Initialized], tc.populateConditions[negv1beta1.Synced], tc.negRefs)
				origCR, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Create(context2.Background(), origCR, v1.CreateOptions{})
				if err != nil {
					t.Errorf("Failed to create test NEG CR: %s", err)
				}
				syncer.svcNegLister.Add(origCR)

				syncer.updateStatus(syncErr)

				negCR, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(testServiceNamespace).Get(context2.Background(), testNegName, v1.GetOptions{})
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
	testSubnetwork := cloud.ResourcePath("subnetwork", &meta.Key{Name: "test-subnetwork"})
	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)
	testNegType := negtypes.VmIpPortEndpointType

	testCases := []struct {
		desc                           string
		zoneDeleted                    bool
		zoneAdded                      bool
		nodeWithNoProviderIDAdded      bool
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
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, syncer := newTestTransactionSyncer(fakeCloud, testNegType, false)
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
			ret, _ := fakeCloud.AggregatedListNetworkEndpointGroup(syncer.NegSyncerKey.GetAPIVersion(), klog.TODO())
			negRefMap := negObjectReferences(ret)
			var refs []negv1beta1.NegObjectReference
			for _, neg := range negRefMap {
				refs = append(refs, neg)
			}
			negCR := createNegCR(syncer.NegName, v1.Now(), true, true, refs)
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
	zoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)
	testNetwork := cloud.ResourcePath("network", &meta.Key{Name: "test-network"})
	testSubnetwork := cloud.ResourcePath("subnetwork", &meta.Key{Name: "test-subnetwork"})
	fakeCloud := negtypes.NewFakeNetworkEndpointGroupCloud(testSubnetwork, testNetwork)

	testIP1 := "10.100.1.1"
	testIP2 := "10.100.1.2"
	testIP3 := "10.100.2.1"
	testPort := int64(80)

	testEndpointSlices := getDefaultEndpointSlices()
	testEndpointSlices[0].Endpoints[0].NodeName = utilpointer.StringPtr("unknown-node")
	testEndpointMap := map[string]*composite.NetworkEndpoint{
		negtypes.TestZone1: &composite.NetworkEndpoint{
			Instance:  negtypes.TestInstance1,
			IpAddress: testIP1,
			Port:      testPort,
		},

		negtypes.TestZone2: &composite.NetworkEndpoint{
			Instance:  negtypes.TestInstance3,
			IpAddress: testIP2,
			Port:      testPort,
		},

		negtypes.TestZone4: &composite.NetworkEndpoint{
			Instance:  negtypes.TestUpgradeInstance1,
			IpAddress: testIP3,
			Port:      testPort,
		},
	}

	// Create initial NetworkEndpointGroups in cloud
	var objRefs []negv1beta1.NegObjectReference
	for zone, endpoint := range testEndpointMap {
		fakeCloud.CreateNetworkEndpointGroup(&composite.NetworkEndpointGroup{Name: testNegName, Version: meta.VersionGA}, zone, klog.TODO())
		fakeCloud.AttachNetworkEndpoints(testNegName, zone, []*composite.NetworkEndpoint{endpoint}, meta.VersionGA, klog.TODO())
		neg, err := fakeCloud.GetNetworkEndpointGroup(testNegName, zone, meta.VersionGA, klog.TODO())
		if err != nil {
			t.Fatalf("failed to get neg from fake cloud: %s", err)
		}

		objRefs = append(objRefs, negv1beta1.NegObjectReference{SelfLink: neg.SelfLink})
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

	_, s := newTestTransactionSyncer(fakeCloud, negtypes.VmIpPortEndpointType, false)
	s.needInit = false

	for _, eps := range testEndpointSlices {
		s.endpointSliceLister.Add(eps)
	}
	s.svcNegLister.Add(neg)
	// mark syncer as started without starting the syncer routine
	(s.syncer.(*syncer)).stopped = false

	err := s.syncInternal()
	if err == nil {
		t.Errorf("syncInternal returned nil, expected an error")
	}

	// Check that unknown zone did not cause endpoints to be removed
	out, _, err := retrieveExistingZoneNetworkEndpointMap(testNegName, zoneGetter, fakeCloud, meta.VersionGA, negtypes.L7Mode, false, klog.TODO())
	if err != nil {
		t.Errorf("errored retrieving existing network endpoints")
	}

	expectedEndpoints := map[string]negtypes.NetworkEndpointSet{
		negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: testIP1, Node: negtypes.TestInstance1, Port: strconv.Itoa(int(testPort))},
		),
		negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: testIP2, Node: negtypes.TestInstance3, Port: strconv.Itoa(int(testPort))},
		),
		negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
			negtypes.NetworkEndpoint{IP: testIP3, Node: negtypes.TestUpgradeInstance1, Port: strconv.Itoa(int(testPort))},
		),
	}

	if !reflect.DeepEqual(expectedEndpoints, out) {
		t.Errorf("endpoints were modified after syncInternal:\ngot %+v,\n expected %+v", out, expectedEndpoints)
	}
}

// TestEnableDegradedMode verifies if DegradedMode has been correctly enabled for L7 endpoint calculator
func TestEnableDegradedMode(t *testing.T) {
	t.Parallel()
	nodeInformer := zonegetter.FakeNodeInformer()
	zonegetter.PopulateFakeNodeInformer(nodeInformer, false)
	zoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE)
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

	initialEndpoints := map[string]negtypes.NetworkEndpointSet{
		negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
		),
		negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance3||80"),
		),
		negtypes.TestZone4: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.2.1||upgrade-instance1||80"),
		),
	}

	updateSucceedEndpoints := map[string]negtypes.NetworkEndpointSet{
		negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80")),
		negtypes.TestZone2: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.3.1||instance3||80")),
		negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
	}

	updateFailedEndpoints := map[string]negtypes.NetworkEndpointSet{
		negtypes.TestZone1: negtypes.NewNetworkEndpointSet(
			networkEndpointFromEncodedEndpoint("10.100.1.1||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.2||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.2.1||instance2||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.3||instance1||80"),
			networkEndpointFromEncodedEndpoint("10.100.1.4||instance1||80")),
		negtypes.TestZone2: negtypes.NewNetworkEndpointSet(),
		negtypes.TestZone4: negtypes.NewNetworkEndpointSet(),
	}

	testCases := []struct {
		desc                 string
		modify               func(ts *transactionSyncer)
		negName              string // to distinguish endpoints in differnt NEGs
		testEndpointSlices   []*discovery.EndpointSlice
		expectedEndpoints    map[string]negtypes.NetworkEndpointSet
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
			_, s := newTestTransactionSyncer(fakeCloud, negtypes.VmIpPortEndpointType, false)
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

			out, _, err := retrieveExistingZoneNetworkEndpointMap(tc.negName, zoneGetter, fakeCloud, meta.VersionGA, negtypes.L7Mode, false, klog.TODO())
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
				out, _, err = retrieveExistingZoneNetworkEndpointMap(tc.negName, zoneGetter, fakeCloud, meta.VersionGA, negtypes.L7Mode, false, klog.TODO())
				if err != nil {
					return false, nil
				}
				if !reflect.DeepEqual(tc.expectedEndpoints, out) {
					return false, nil
				}
				s.syncLock.Lock()
				errorState := s.inErrorState()
				s.syncLock.Unlock()
				if errorState != tc.expectedInErrorState {
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
		input  func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap)
		expect func() labels.EndpointPodLabelMap
	}{
		{
			desc: "empty inputs",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				return map[string]negtypes.NetworkEndpointSet{}, negtypes.EndpointPodMap{}
			},
			expect: func() labels.EndpointPodLabelMap {
				return labels.EndpointPodLabelMap{}
			},
		},
		{
			desc: "Add endpoints in diferent zones",
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(endpointSet1),
					testZone2: negtypes.NewNetworkEndpointSet().Union(endpointSet2),
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
			input: func() (map[string]negtypes.NetworkEndpointSet, negtypes.EndpointPodMap) {
				retSet := map[string]negtypes.NetworkEndpointSet{
					testZone1: negtypes.NewNetworkEndpointSet().Union(endpointSet1),
					testZone2: negtypes.NewNetworkEndpointSet().Union(endpointSet2).Union(endpointSet3),
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
		endpointPodLabelMap := getEndpointPodLabelMap(endpoints, endpointPodMap, podLister, lpConfig, nil, klog.TODO())
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
		targetEndpointMap map[string]negtypes.NetworkEndpointSet
		expect            metricscollector.LabelPropagationStats
	}{
		{
			desc:              "Empty inputs",
			curLabelMap:       labels.EndpointPodLabelMap{},
			addLabelMap:       labels.EndpointPodLabelMap{},
			targetEndpointMap: map[string]negtypes.NetworkEndpointSet{},
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
			targetEndpointMap: map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet(
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
			targetEndpointMap: map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet(
					endpoint1,
					endpoint2,
				),
				testZone2: negtypes.NewNetworkEndpointSet(
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
			targetEndpointMap: map[string]negtypes.NetworkEndpointSet{
				testZone2: negtypes.NewNetworkEndpointSet(
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

func newL4ILBTestTransactionSyncer(fakeGCE negtypes.NetworkEndpointGroupCloud, mode negtypes.EndpointsCalculatorMode) (negtypes.NegSyncer, *transactionSyncer) {
	negsyncer, ts := newTestTransactionSyncer(fakeGCE, negtypes.VmIpEndpointType, false)
	ts.endpointsCalculator = GetEndpointsCalculator(ts.podLister, ts.nodeLister, ts.serviceLister, ts.zoneGetter, ts.NegSyncerKey, mode, klog.TODO(), false, nil, &network.NetworkInfo{IsDefault: true})
	return negsyncer, ts
}

func newTestTransactionSyncer(fakeGCE negtypes.NetworkEndpointGroupCloud, negType negtypes.NetworkEndpointType, customName bool) (negtypes.NegSyncer, *transactionSyncer) {
	testContext := negtypes.NewTestContext()
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
	fakeZoneGetter := zonegetter.NewFakeZoneGetter(nodeInformer, defaultTestSubnetURL, false)

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
			fakeZoneGetter, svcPort, mode, klog.TODO(), testContext.EnableDualStackNEG, metricscollector.FakeSyncerMetrics(), &network.NetworkInfo{IsDefault: true}),
		string(kubeSystemUID),
		testContext.SvcNegClient,
		metricscollector.FakeSyncerMetrics(),
		customName,
		klog.TODO(),
		labels.PodLabelPropagationConfig{},
		testContext.EnableDualStackNEG,
		network.NetworkInfo{NetworkURL: fakeGCE.NetworkURL(), SubnetworkURL: fakeGCE.SubnetworkURL()},
	)
	transactionSyncer := negsyncer.(*syncer).core.(*transactionSyncer)
	indexers := map[string]cache.IndexFunc{
		endpointslices.EndpointSlicesByServiceIndex: endpointslices.EndpointSlicesByServiceFunc,
	}
	transactionSyncer.endpointSliceLister.AddIndexers(indexers)
	return negsyncer, transactionSyncer
}

func copyMap(endpointMap map[string]negtypes.NetworkEndpointSet) map[string]negtypes.NetworkEndpointSet {
	ret := map[string]negtypes.NetworkEndpointSet{}
	for k, v := range endpointMap {
		ret[k] = negtypes.NewNetworkEndpointSet(v.List()...)
	}
	return ret
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
	return
}

type testReflector struct {
	*readiness.NoopReflector
	keys     []negtypes.NegSyncerKey
	negNames []string

	endpointMaps map[string]negtypes.EndpointPodMap
}

func (tr *testReflector) Flush() {
	tr.keys = []negtypes.NegSyncerKey{}
	tr.negNames = []string{}
	tr.endpointMaps = map[string]negtypes.EndpointPodMap{}
}

func (tr *testReflector) CommitPods(syncerKey negtypes.NegSyncerKey, negName string, zone string, endpointMap negtypes.EndpointPodMap) {
	tr.keys = append(tr.keys, syncerKey)
	tr.negNames = append(tr.negNames, negName)
	tr.endpointMaps[zone] = endpointMap
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

// negObjectReferences returns objectReferences for NEG CRs from NEG Objects
func negObjectReferences(negs map[*meta.Key]*composite.NetworkEndpointGroup) map[string]negv1beta1.NegObjectReference {

	negObjs := make(map[string]negv1beta1.NegObjectReference)
	for _, neg := range negs {
		negObjs[neg.SelfLink] = negv1beta1.NegObjectReference{
			Id:                  fmt.Sprint(neg.Id),
			SelfLink:            neg.SelfLink,
			NetworkEndpointType: negv1beta1.NetworkEndpointType(neg.NetworkEndpointType),
		}
	}
	return negObjs
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

// checkNegCR validates the NegObjectReferences and the LastSyncTime. It will not validate the conditions fields but ensures at most 2 conditions exist
func checkNegCR(t *testing.T, negCR *negv1beta1.ServiceNetworkEndpointGroup, previousLastSyncTime metav1.Time, expectZones sets.String, expectedNegRefs map[string]negv1beta1.NegObjectReference, expectSyncTimeUpdate, expectErr bool) {
	if expectSyncTimeUpdate && !previousLastSyncTime.Before(&negCR.Status.LastSyncTime) {
		t.Errorf("Expected Neg CR to have an updated LastSyncTime")
	} else if !expectSyncTimeUpdate && !negCR.Status.LastSyncTime.IsZero() && !previousLastSyncTime.Equal(&negCR.Status.LastSyncTime) {
		t.Errorf("Expected Neg CR to not have an updated LastSyncTime")
	}

	var foundNegObjs []string
	if len(negCR.Status.NetworkEndpointGroups) != len(expectedNegRefs) {
		t.Errorf("Expected Neg CR to have %d corresponding neg object references, but has %d", len(expectedNegRefs), len(negCR.Status.NetworkEndpointGroups))
	}

	for _, negObj := range negCR.Status.NetworkEndpointGroups {
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

	if len(negCR.Status.Conditions) > 2 {
		t.Errorf("Expected to have at most 2 conditions, found %d", len(negCR.Status.Conditions))
	}
}
