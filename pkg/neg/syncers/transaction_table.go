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

import negtypes "k8s.io/ingress-gce/pkg/neg/types"

const (
	attachOp = iota
	detachOp
)

type transactionOp int

func (op transactionOp) String() string {
	switch op {
	case attachOp:
		return "Attach"
	case detachOp:
		return "Detach"
	default:
		return "UnknownOperation"
	}
}

type transactionEntry struct {
	// Operation represents the operation type associated with each transaction
	Operation transactionOp
	// NeedReconcile indicates whether the entry needs to be reconciled.
	NeedReconcile bool
	// Zone represents the zone of the transaction
	Zone string
}

// transactionTable records ongoing NEG API operation per endpoint
// It uses the encoded endpoint as key and associate attributes of an transaction as value
// WARNING: transactionTable is not thread safe
type transactionTable struct {
	data map[negtypes.NetworkEndpoint]transactionEntry
}

func NewTransactionTable() transactionTable {
	return transactionTable{
		data: make(map[negtypes.NetworkEndpoint]transactionEntry),
	}
}

func (tt transactionTable) Keys() []negtypes.NetworkEndpoint {
	res := []negtypes.NetworkEndpoint{}
	for key := range tt.data {
		res = append(res, key)
	}
	return res
}

func (tt transactionTable) Get(key negtypes.NetworkEndpoint) (transactionEntry, bool) {
	ret, ok := tt.data[key]
	return ret, ok
}

func (tt transactionTable) Delete(key negtypes.NetworkEndpoint) {
	delete(tt.data, key)
}

func (tt transactionTable) Put(key negtypes.NetworkEndpoint, entry transactionEntry) {
	tt.data[key] = entry
}
