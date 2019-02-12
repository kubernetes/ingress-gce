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
	"net"
	"reflect"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
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

func TestReconcileTransactions(t *testing.T) {
	testCases := []struct {
		desc        string
		endpointMap map[string]sets.String
		table       func() transactionTable
		expect      func() transactionTable
	}{
		{
			"empty inputs",
			map[string]sets.String{},
			func() transactionTable { return NewTransactionTable() },
			func() transactionTable { return NewTransactionTable() },
		},
		{
			"1 endpoint, empty transaction table",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 1, testInstance1, "8080")),
			},
			func() transactionTable { return NewTransactionTable() },
			func() transactionTable { return NewTransactionTable() },
		},
		{
			"10 endpoints, empty transaction table",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable { return NewTransactionTable() },
			func() transactionTable { return NewTransactionTable() },
		},
		{
			"10 endpoints and 5 attaching transactions are expected",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints and 10 attaching transactions are expected",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints, 5 attaching transactions are expected",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints, 5 attaching and 10 detaching transactions are expected",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
		},
		{
			"expect 10 endpoints, but unwanted endpoints are being attached",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.3.1"), 5, testInstance3, "8080")
				return table
			},
		},
		{
			"10 endpoints and 5 attaching transaction expected, 5 attaching transactions need reconcile",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.2.1"), 5, testInstance2, "8080")
				return table
			},
		},
		{
			"10 endpoints expected, 5 detaching transactions need reconcile",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"10 endpoints and 5 attaching transaction expected, 5 detaching transactions need reconcile",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
		},
		{
			"transaction entry has the wrong zone information",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: attachOp, NeedReconcile: true}, net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")
				return table
			},
		},
		{
			"complex case 1: multiple zone and instances. No transaction",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				return table
			},
		},
		{
			"complex case 2: detaching transactions need reconciliation",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				return table
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: true}, net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")
				return table
			},
		},
		{
			"complex case 2: both attaching and detaching transactions need reconciliation",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{Zone: testZone1, Operation: attachOp, NeedReconcile: false}, net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")
				generateTransaction(table, transactionEntry{Zone: testZone2, Operation: detachOp, NeedReconcile: false}, net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")
				return table
			},
			func() transactionTable {
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
			t.Errorf("For test case %q, does not expect endpointMap to change", tc.desc)
		}

		// Check if the existing entries are matching expected
		expectTable := tc.expect()
		for _, key := range table.Keys() {
			expectEntry, ok := expectTable.Get(key)
			if !ok {
				t.Errorf("For test case %q, do not expect key %q to exists", tc.desc, key)
				continue
			}
			gotEntry, _ := table.Get(key)
			if !reflect.DeepEqual(expectEntry, gotEntry) {
				t.Errorf("For test case %q, expectEntry of key %q to be %v, but got %v", tc.desc, key, expectEntry, gotEntry)
			}
		}

		// Check if there are missing entries that expected to exist in output
		for _, key := range expectTable.Keys() {
			_, ok := table.Get(key)
			if !ok {
				t.Errorf("For test case %q, expect transaction key %q to exists, but got nil", tc.desc, key)
				continue
			}
		}
	}
}

func TestMergeTransactionIntoZoneEndpointMap(t *testing.T) {
	testCases := []struct {
		desc              string
		endpointMap       map[string]sets.String
		table             func() transactionTable
		expectEndpointMap map[string]sets.String
	}{
		{
			"empty map and transactions",
			map[string]sets.String{},
			func() transactionTable { return NewTransactionTable() },
			map[string]sets.String{},
		},
		{
			"empty transactions",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable { return NewTransactionTable() },
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"empty map",
			map[string]sets.String{},
			func() transactionTable {
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
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add existing endpoints",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable {
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
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add non-existing endpoints",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable {
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
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 20, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 20, testInstance3, "8080")),
			},
		},
		{
			"remove existing endpoints",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable {
				table := NewTransactionTable()
				generateTransaction(table, transactionEntry{
					Operation:     detachOp,
					NeedReconcile: false,
					Zone:          testZone1,
				}, net.ParseIP("1.1.1.1"), 5, testInstance1, "8080")
				return table
			},
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"add non-existing endpoints and remove existing endpoints",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable {
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
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")).Union(generateEndpointSet(net.ParseIP("1.1.2.1"), 10, testInstance2, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
	}

	for _, tc := range testCases {
		mergeTransactionIntoZoneEndpointMap(tc.endpointMap, tc.table())
		if !reflect.DeepEqual(tc.endpointMap, tc.expectEndpointMap) {
			t.Errorf("For test case %q, expect endpoint map to be %+v, but got %+v", tc.desc, tc.expectEndpointMap, tc.endpointMap)
		}
	}
}

func TestFilterEndpointByTransaction(t *testing.T) {
	testCases := []struct {
		desc              string
		endpointMap       map[string]sets.String
		table             func() transactionTable
		expectEndpointMap map[string]sets.String
	}{
		{
			"both emtpy",
			map[string]sets.String{},
			func() transactionTable { return NewTransactionTable() },
			map[string]sets.String{},
		},
		{
			"emtpy map",
			map[string]sets.String{},
			func() transactionTable {
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
			map[string]sets.String{},
		},
		{
			"emtpy transaction",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable { return NewTransactionTable() },
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.1"), 10, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
		{
			"emtpy transaction",
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
			func() transactionTable { return NewTransactionTable() },
			map[string]sets.String{
				testZone1: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.1.6"), 5, testInstance1, "8080")),
				testZone2: sets.NewString().Union(generateEndpointSet(net.ParseIP("1.1.3.1"), 10, testInstance3, "8080")),
			},
		},
	}

	for _, tc := range testCases {
		input := tc.endpointMap
		filterEndpointByTransaction(input, tc.table())
		if !reflect.DeepEqual(tc.endpointMap, tc.expectEndpointMap) {
			t.Errorf("For test case %q, expect endpoint map to be %+v, but got %+v", tc.desc, tc.expectEndpointMap, tc.endpointMap)
		}
	}
}

func NewTestTransactionSyncer() negtypes.NegSyncer {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	namer := utils.NewNamer(clusterID, "")
	ctxConfig := context.ControllerContextConfig{
		NEGEnabled:              true,
		BackendConfigEnabled:    false,
		Namespace:               apiv1.NamespaceAll,
		ResyncPeriod:            1 * time.Second,
		DefaultBackendSvcPortID: defaultBackend,
	}
	context := context.NewControllerContext(kubeClient, backendConfigClient, nil, namer, ctxConfig)
	svcPort := NegSyncerKey{
		Namespace:  testNamespace,
		Name:       testService,
		Port:       80,
		TargetPort: "8080",
	}

	return NewTransactionSyncer(svcPort,
		testNegName,
		record.NewFakeRecorder(100),
		negtypes.NewFakeNetworkEndpointGroupCloud("test-subnetwork", "test-newtork"),
		negtypes.NewFakeZoneGetter(),
		context.ServiceInformer.GetIndexer(),
		context.EndpointInformer.GetIndexer())
}

func copyMap(endpointMap map[string]sets.String) map[string]sets.String {
	ret := map[string]sets.String{}
	for k, v := range endpointMap {
		ret[k] = sets.NewString(v.List()...)
	}
	return ret
}

func generateTransaction(table transactionTable, entry transactionEntry, initialIp net.IP, num int, instance string, targetPort string) {
	endpointSet := generateEndpointSet(initialIp, num, instance, targetPort)
	for _, encodedEndpoint := range endpointSet.List() {
		table.Put(encodedEndpoint, entry)
	}
}

func generateEndpointSet(initialIp net.IP, num int, instance string, targetPort string) sets.String {
	ret := sets.NewString()
	ip := initialIp.To4()
	for i := 1; i <= num; i++ {
		if i%256 == 0 {
			ip[2]++
		}
		ip[3]++
		ret.Insert(encodeEndpoint(ip.String(), instance, targetPort))
	}
	return ret
}
