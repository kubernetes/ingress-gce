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
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"sync"
)

type networkEndpointTransactionTable interface {
	Keys() []negtypes.NetworkEndpoint
	Get(key negtypes.NetworkEndpoint) (transactionEntry, bool)
	Delete(key negtypes.NetworkEndpoint)
	Put(key negtypes.NetworkEndpoint, entry transactionEntry)
}

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

	lock sync.Mutex
}

func NewTransactionTable() networkEndpointTransactionTable {
	return &transactionTable{
		data: make(map[negtypes.NetworkEndpoint]transactionEntry),
	}
}

func (tt *transactionTable) Keys() []negtypes.NetworkEndpoint {
	tt.lock.Lock()
	defer tt.lock.Unlock()

	res := []negtypes.NetworkEndpoint{}
	for key := range tt.data {
		res = append(res, key)
	}
	return res
}

func (tt *transactionTable) Get(key negtypes.NetworkEndpoint) (transactionEntry, bool) {
	tt.lock.Lock()
	defer tt.lock.Unlock()

	ret, ok := tt.data[key]
	return ret, ok
}

func (tt *transactionTable) Delete(key negtypes.NetworkEndpoint) {
	tt.lock.Lock()
	defer tt.lock.Unlock()

	delete(tt.data, key)
}

func (tt *transactionTable) Put(key negtypes.NetworkEndpoint, entry transactionEntry) {
	tt.lock.Lock()
	defer tt.lock.Unlock()

	tt.data[key] = entry
}
