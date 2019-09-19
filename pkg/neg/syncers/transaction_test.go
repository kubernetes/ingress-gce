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
	"fmt"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"

	"google.golang.org/api/compute/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// test zone and instances in the zones
	testZone1     = "zone1"
	testInstance1 = "instance1"
	testInstance2 = "instance2"
	testZone2     = "zone2"
	testInstance3 = "instance3"
	testInstance4 = "instance4"
	testNamespace = "ns"
	testService   = "svc"
)

func TestTransactionSyncNetworkEndpoints(t *testing.T) {
	t.Parallel()

	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	fakeCloud := negtypes.NewAdapter(fakeGCE)
	_, transactionSyncer := newTestTransactionSyncer(fakeCloud)

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
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{},
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
		},
		{
			"remove some endpoints",
			map[string]negtypes.NetworkEndpointSet{},
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
		},
		{
			"add duplicate endpoints",
			map[string]negtypes.NetworkEndpointSet{
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{},
			map[string]negtypes.NetworkEndpointSet{
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
		},
		{
			"add and remove endpoints",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
		},
		{
			"add more endpoints",
			map[string]negtypes.NetworkEndpointSet{
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{},
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add and remove endpoints in both zones",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.4.1"), 10, testInstance4, "8080")),
			},
		},
	}

	if err := transactionSyncer.ensureNetworkEndpointGroups(); err != nil {
		t.Errorf("Expect error == nil, but got %v", err)
	}

	for _, tc := range testCases {
		err := transactionSyncer.syncNetworkEndpoints(tc.addEndpoints, tc.removeEndpoints)
		if err != nil {
			t.Errorf("For case %q, endpointSets error == nil, but got %v", tc.desc, err)
		}

		if err := waitForTransactions(transactionSyncer); err != nil {
			t.Errorf("For case %q, endpointSets error == nil, but got %v", tc.desc, err)
		}

		for zone, endpoints := range tc.expectEndpoints {
			list, err := fakeCloud.ListNetworkEndpoints(transactionSyncer.negName, zone, false)
			if err != nil {
				t.Errorf("For case %q,, endpointSets error == nil, but got %v", tc.desc, err)
			}

			endpointSet := negtypes.NewNetworkEndpointSet()
			for _, ep := range list {
				endpointSet.Insert(negtypes.NetworkEndpoint{IP: ep.NetworkEndpoint.IpAddress, Node: ep.NetworkEndpoint.Instance, Port: strconv.FormatInt(ep.NetworkEndpoint.Port, 10)})
			}

			if !endpoints.Equal(endpointSet) {
				t.Errorf("For case %q, in zone %q, endpointSets endpoints == %v, but got %v", tc.desc, zone, endpoints, endpointSet)
			}
		}
	}
}

func TestCommitTransaction(t *testing.T) {
	t.Parallel()
	s, transactionSyncer := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())))
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
		endpointMap      map[negtypes.NetworkEndpoint]*compute.NetworkEndpoint
		table            func() networkEndpointTransactionTable
		expect           func() networkEndpointTransactionTable
		expectSyncCount  int
		expectRetryCount int
		expectNeedInit   bool
	}{
		{
			"empty inputs",
			nil,
			map[negtypes.NetworkEndpoint]*compute.NetworkEndpoint{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			1,
			0,
			false,
		},
		{
			"attach 10 endpoints on 1 instance successfully",
			nil,
			generateEndpointBatch(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
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
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080"))),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
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
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080"))),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			4,
			0,
			false,
		},
		{
			"error and retry",
			fmt.Errorf("dummy error"),
			map[negtypes.NetworkEndpoint]*compute.NetworkEndpoint{},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			5,
			1,
			true,
		},
		{
			"error and retry #2",
			fmt.Errorf("dummy error"),
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080"))),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			6,
			2,
			true,
		},
		{
			"detach 20 endpoints on 2 instance but missing transaction entries on 1 instance",
			nil,
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080"))),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
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
			generateEndpointBatch(negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080"))),
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
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

func TestReconcileTransactions(t *testing.T) {
	testCases := []struct {
		desc        string
		endpointMap map[string]negtypes.NetworkEndpointSet
		table       func() networkEndpointTransactionTable
		expect      func() networkEndpointTransactionTable
	}{
		{
			"empty inputs",
			map[string]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			func() networkEndpointTransactionTable { return NewTransactionTable() },
		},
		{
			"1 endpoint, empty transaction table",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 1, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			func() networkEndpointTransactionTable { return NewTransactionTable() },
		},
		{
			"10 endpoints, empty transaction table",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			func() networkEndpointTransactionTable { return NewTransactionTable() },
		},
		{
			"10 endpoints and 5 attaching transactions are expected",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints and 10 attaching transactions are expected",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints, 5 attaching transactions are expected",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints, 5 attaching and 10 detaching transactions are expected",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
		},
		{
			"endpointSets 10 endpoints, but unwanted endpoints are being attached",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
		},
		{
			"10 endpoints and 5 attaching transaction expected, 5 attaching transactions need reconcile",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				return table
			},
		},
		{
			"10 endpoints expected, 5 detaching transactions need reconcile",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints and 5 attaching transaction expected, 5 detaching transactions need reconcile",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"transaction entry has the wrong zone information",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
		},
		{
			"complex case 1: multiple zone and instances. No transaction",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				return table
			},
		},
		{
			"complex case 2: detaching transactions need reconciliation",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
		},
		{
			"complex case 2: both attaching and detaching transactions need reconciliation",
			map[string]negtypes.NetworkEndpointSet{
				testZone1: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: negtypes.NewNetworkEndpointSet().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				return table
			},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
		},
	}

	for _, tc := range testCases {
		copy := copyMap(tc.endpointMap)
		table := tc.table()
		reconcileTransactions(tc.endpointMap, table)

		// Check if endpointMap was modified
		if !reflect.DeepEqual(copy, tc.endpointMap) {
			t.Errorf("For test case %q, does not endpointSets endpointMap to change", tc.desc)
		}
		// Check if the existing entries are matching expected
		validateTransactionTableEquality(t, tc.desc, table, tc.expect())
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
					Operation:     attachOp,
					NeedReconcile: false,
					Zone:          testZone1,
				}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     attachOp,
					NeedReconcile: true,
					Zone:          testZone2,
				}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     detachOp,
					NeedReconcile: true,
					Zone:          testZone1,
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
					Operation:     attachOp,
					NeedReconcile: false,
					Zone:          testZone1,
				}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     attachOp,
					NeedReconcile: true,
					Zone:          testZone2,
				}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     detachOp,
					NeedReconcile: true,
					Zone:          testZone1,
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
					Operation:     attachOp,
					NeedReconcile: false,
					Zone:          testZone1,
				}, net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     attachOp,
					NeedReconcile: true,
					Zone:          testZone2,
				}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     detachOp,
					NeedReconcile: true,
					Zone:          testZone1,
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
					Operation:     detachOp,
					NeedReconcile: false,
					Zone:          testZone1,
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
					Operation:     detachOp,
					NeedReconcile: false,
					Zone:          testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     attachOp,
					NeedReconcile: true,
					Zone:          testZone1,
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
		mergeTransactionIntoZoneEndpointMap(tc.endpointMap, tc.table())
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
			"both emtpy",
			map[string]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable { return NewTransactionTable() },
			map[string]negtypes.NetworkEndpointSet{},
		},
		{
			"emtpy map",
			map[string]negtypes.NetworkEndpointSet{},
			func() networkEndpointTransactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation:     detachOp,
					NeedReconcile: false,
					Zone:          testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{
					Operation:     attachOp,
					NeedReconcile: true,
					Zone:          testZone1,
				}, net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")
				return table
			},
			map[string]negtypes.NetworkEndpointSet{},
		},
		{
			"emtpy transaction",
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
			"emtpy transaction",
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
		filterEndpointByTransaction(input, tc.table())
		if !reflect.DeepEqual(tc.endpointMap, tc.expectEndpointMap) {
			t.Errorf("For test case %q, endpointSets endpoint map to be %+v, but got %+v", tc.desc, tc.expectEndpointMap, tc.endpointMap)
		}
	}
}

func TestCommitPods(t *testing.T) {
	t.Parallel()
	_, transactionSyncer := newTestTransactionSyncer(negtypes.NewAdapter(gce.NewFakeGCECloud(gce.DefaultTestClusterValues())))
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
		if len(expectOutput) != 0 && !(negNameSet.Len() == 1 && negNameSet.Has(transactionSyncer.negName)) {
			t.Errorf("For test case %q, expect neg name to be %v, but got %v", tc.desc, transactionSyncer.negName, negNameSet.List())
		}

		if !reflect.DeepEqual(expectOutput, reflector.endpointMaps) {
			t.Errorf("For test case %q, expect endpoint map to be %v, but got %v", tc.desc, expectOutput, reflector.endpointMaps)
		}
	}
}

func newTestTransactionSyncer(fakeGCE negtypes.NetworkEndpointGroupCloud) (negtypes.NegSyncer, *transactionSyncer) {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namer := namer_util.NewNamer(clusterID, "")
	ctxConfig := context.ControllerContextConfig{
		Namespace:             apiv1.NamespaceAll,
		ResyncPeriod:          1 * time.Second,
		DefaultBackendSvcPort: defaultBackend,
	}
	context := context.NewControllerContext(kubeClient, nil, backendConfigClient, nil, nil, namer, ctxConfig)
	svcPort := negtypes.NegSyncerKey{
		Namespace:  testNamespace,
		Name:       testService,
		Port:       80,
		TargetPort: "8080",
	}

	// TODO(freehan): use real readiness reflector
	reflector := &readiness.NoopReflector{}

	negsyncer := NewTransactionSyncer(svcPort,
		testNegName,
		record.NewFakeRecorder(100),
		fakeGCE,
		negtypes.NewFakeZoneGetter(),
		context.PodInformer.GetIndexer(),
		context.ServiceInformer.GetIndexer(),
		context.EndpointInformer.GetIndexer(),
		reflector)
	transactionSyncer := negsyncer.(*syncer).core.(*transactionSyncer)
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
		retMap[endpoint] = types.NamespacedName{Namespace: testNamespace, Name: fmt.Sprintf("pod-%s-%d", instance, i)}
	}
	return retSet, retMap
}

func unionEndpointMap(m1, m2 negtypes.EndpointPodMap) negtypes.EndpointPodMap {
	for k, v := range m2 {
		m1[k] = v
	}
	return m1
}

func generateEndpointBatch(endpointSet negtypes.NetworkEndpointSet) map[negtypes.NetworkEndpoint]*compute.NetworkEndpoint {
	ret, _ := makeEndpointBatch(endpointSet)
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
