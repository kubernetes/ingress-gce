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
	"testing"

	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

func TestTransactionTable(t *testing.T) {
	table := NewTransactionTable()

	// Verify table are empty initially
	ret := table.Keys()
	if len(ret) != 0 {
		t.Errorf("Expect no keys, but got %v", ret)
	}

	_, ok := table.Get(negtypes.NetworkEndpoint{IP: "Non exists"})
	if ok {
		t.Errorf("Expect ok = false, but got %v", ok)
	}

	testNum := 10
	ipPrefix := "ip"
	portPrefix := "port"
	nodePrefix := "node"
	zonePrefix := "zone"
	testKeyMap := map[negtypes.NetworkEndpoint]transactionEntry{}

	// Insert entries into transaction table
	for i := 0; i < testNum; i++ {
		key := negtypes.NetworkEndpoint{IP: fmt.Sprintf("%s%d", ipPrefix, i), Port: fmt.Sprintf("%s%d", portPrefix, i), Node: fmt.Sprintf("%s%d", nodePrefix, i)}
		entry := transactionEntry{
			attachOp,
			false,
			fmt.Sprintf("%s%d", zonePrefix, i),
		}
		table.Put(key, entry)
		testKeyMap[key] = entry
	}

	verifyTable(t, table, testKeyMap)

	// Update half of the entries in the transaction table
	for i := 0; i < testNum/2; i++ {
		key := negtypes.NetworkEndpoint{IP: fmt.Sprintf("%s%d", ipPrefix, i), Port: fmt.Sprintf("%s%d", portPrefix, i), Node: fmt.Sprintf("%s%d", nodePrefix, i)}
		newEntry := transactionEntry{
			detachOp,
			true,
			fmt.Sprintf("%s%d", zonePrefix, i),
		}
		table.Put(key, newEntry)
		testKeyMap[key] = newEntry
	}

	verifyTable(t, table, testKeyMap)
}

func verifyTable(t *testing.T, table networkEndpointTransactionTable, expectTransactionMap map[negtypes.NetworkEndpoint]transactionEntry) {
	keys := table.Keys()
	if len(expectTransactionMap) != len(keys) {
		t.Errorf("Expect keys length to be %v, but got %v", len(expectTransactionMap), len(keys))
	}

	for _, key := range keys {
		entry, ok := table.Get(key)
		if !ok {
			t.Errorf("Expect key %q to exist in transaction table, but got %v", key, ok)
		}
		expectEntry, ok := expectTransactionMap[key]
		if !ok {
			t.Errorf("Expect key %q to exist in testKeyMap, but got %v", key, ok)
		}

		if entry != expectEntry {
			t.Errorf("Expect entry to be %v, but got %v", expectEntry, entry)
		}
	}
}

func genNetworkEndpoint(key string) negtypes.NetworkEndpoint {
	return negtypes.NetworkEndpoint{
		IP:   key,
		Port: key,
		Node: key,
	}
}
