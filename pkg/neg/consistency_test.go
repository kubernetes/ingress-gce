/*
Copyright 2026 The Kubernetes Authors.

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

package neg

import (
	"errors"
	"sync"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	negfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/utils/consistency"
)

// fakeRVGetter stands in for an informer's LastSyncResourceVersion so a test
// can hold the cache behind a recorded write and then let it catch up.
type fakeRVGetter struct {
	mu sync.Mutex
	rv string
}

func (f *fakeRVGetter) LastSyncResourceVersion() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rv
}

func (f *fakeRVGetter) setRV(rv string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rv = rv
}

// newSvcNegConsistencyStore returns a store wired to a controllable SvcNeg
// informer version, matching how cmd/glbc builds the NEG controller's store.
func newSvcNegConsistencyStore(rv string) (consistency.ConsistencyStore, *fakeRVGetter) {
	getter := &fakeRVGetter{rv: rv}
	store := consistency.NewConsistencyStore(map[schema.GroupResource]consistency.LastSyncRVGetter{
		negtypes.SvcNegGroupResource: getter,
	})
	return store, getter
}

const (
	consistencyTestNamespace = "test-ns"
	consistencyTestService   = "test-svc"
	consistencyTestNegName   = "test-neg"
)

// TestEnsureSvcNegCRStallsOnStaleCache covers the gate added to ensureSvcNegCR:
// with a write recorded ahead of the informer, the sync must report a
// ConsistencyError rather than deciding create-vs-update from a stale read,
// and must proceed once the informer catches up.
func TestEnsureSvcNegCRStallsOnStaleCache(t *testing.T) {
	t.Parallel()

	manager, _, testContext, err := NewTestSyncerManager(fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("failed to create test syncer manager: %v", err)
	}
	store, getter := newSvcNegConsistencyStore("100")
	manager.consistencyStore = store

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: consistencyTestNamespace,
			Name:      consistencyTestService,
			UID:       "svc-uid",
		},
	}
	if err := testContext.ServiceInformer.GetIndexer().Add(svc); err != nil {
		t.Fatalf("failed to add service to the informer: %v", err)
	}

	svcKey := serviceKey{namespace: consistencyTestNamespace, name: consistencyTestService}
	portInfo := negtypes.PortInfo{
		PortTuple: negtypes.SvcPortTuple{Port: 80},
		NegName:   consistencyTestNegName,
	}

	// A write the informer has not observed yet: the sync must stall.
	negtypes.RecordSvcNegWrite(store, &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       consistencyTestNamespace,
			Name:            consistencyTestNegName,
			UID:             "neg-uid",
			ResourceVersion: "200",
		},
	})

	err = manager.ensureSvcNegCR(svcKey, portInfo)
	var consistencyErr *consistency.ConsistencyError
	if !errors.As(err, &consistencyErr) {
		t.Fatalf("ensureSvcNegCR() with a stale cache = %v, want *consistency.ConsistencyError", err)
	}

	// Once the informer has caught up the sync proceeds: whatever it does
	// next, it must no longer be reporting staleness.
	getter.setRV("200")
	if err := manager.ensureSvcNegCR(svcKey, portInfo); errors.As(err, &consistencyErr) {
		t.Errorf("ensureSvcNegCR() after the cache caught up = %v, want it to proceed", err)
	}
}

// TestClearSvcNegConsistencyRecordOnDelete covers the informer DeleteFunc
// that drops a deleted CR's record: the plain object, the tombstone an
// informer delivers when the final state is unknown, and the UID guard that
// keeps a deletion event for an old incarnation from dropping the record of a
// recreated CR. Clearing on the informer event rather than at the delete call
// sites is what also covers deletions this controller did not perform.
func TestClearSvcNegConsistencyRecordOnDelete(t *testing.T) {
	t.Parallel()

	negCR := &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       consistencyTestNamespace,
			Name:            consistencyTestNegName,
			UID:             "neg-uid",
			ResourceVersion: "200",
		},
	}
	ref := negtypes.SvcNegRef(consistencyTestNamespace, consistencyTestNegName)

	for _, tc := range []struct {
		desc        string
		deleted     interface{}
		wantCleared bool
	}{
		{
			desc:        "plain object clears the record",
			deleted:     negCR,
			wantCleared: true,
		},
		{
			desc:        "tombstone is unwrapped and clears the record",
			deleted:     cache.DeletedFinalStateUnknown{Key: consistencyTestNamespace + "/" + consistencyTestNegName, Obj: negCR},
			wantCleared: true,
		},
		{
			desc: "deletion of an old incarnation leaves a recreated CR's record",
			deleted: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{Namespace: consistencyTestNamespace, Name: consistencyTestNegName, UID: "old-uid"},
			},
			wantCleared: false,
		},
		{
			desc:        "unexpected type is ignored",
			deleted:     "not-a-cr",
			wantCleared: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			store, _ := newSvcNegConsistencyStore("100")
			negtypes.RecordSvcNegWrite(store, negCR)
			if err := store.EnsureReady(ref); err == nil {
				t.Fatalf("EnsureReady(%v) before the delete event = nil, want stale so the test exercises a real record", ref)
			}

			clearSvcNegConsistencyRecord(store, tc.deleted)

			err := store.EnsureReady(ref)
			if tc.wantCleared && err != nil {
				t.Errorf("EnsureReady(%v) after the delete event = %v, want nil: the record should have been cleared", ref, err)
			}
			if !tc.wantCleared && err == nil {
				t.Errorf("EnsureReady(%v) after the delete event = nil, want the record to survive", ref)
			}
		})
	}
}

// TestRecordingSvcNegClient covers the client wrapper: every write going
// through it must leave a record the store then stalls on, without any call
// site recording by hand.
func TestRecordingSvcNegClient(t *testing.T) {
	t.Parallel()

	ref := negtypes.SvcNegRef(consistencyTestNamespace, consistencyTestNegName)
	newCR := func(rv string) *negv1beta1.ServiceNetworkEndpointGroup {
		return &negv1beta1.ServiceNetworkEndpointGroup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       consistencyTestNamespace,
				Name:            consistencyTestNegName,
				UID:             "neg-uid",
				ResourceVersion: rv,
			},
		}
	}

	for _, tc := range []struct {
		desc string
		// seed puts the CR into the fake clientset first, for the writes
		// that need an existing object. The fake does not assign
		// ResourceVersions, so the seeded (or created) RV "200" is what
		// the write returns and the wrapper records.
		seed  bool
		write func(t *testing.T, client svcnegclient.Interface)
	}{
		{
			desc: "Create is recorded",
			write: func(t *testing.T, client svcnegclient.Interface) {
				if _, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).Create(t.Context(), newCR("200"), metav1.CreateOptions{}); err != nil {
					t.Fatalf("Create() = %v, want nil", err)
				}
			},
		},
		{
			desc: "Update is recorded",
			seed: true,
			write: func(t *testing.T, client svcnegclient.Interface) {
				if _, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).Update(t.Context(), newCR("200"), metav1.UpdateOptions{}); err != nil {
					t.Fatalf("Update() = %v, want nil", err)
				}
			},
		},
		{
			desc: "Patch is recorded",
			seed: true,
			write: func(t *testing.T, client svcnegclient.Interface) {
				if _, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).Patch(t.Context(), consistencyTestNegName, apitypes.MergePatchType, []byte(`{"status":{}}`), metav1.PatchOptions{}); err != nil {
					t.Fatalf("Patch() = %v, want nil", err)
				}
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			store, getter := newSvcNegConsistencyStore("100")
			var fakeClient *negfake.Clientset
			if tc.seed {
				fakeClient = negfake.NewSimpleClientset(newCR("200"))
			} else {
				fakeClient = negfake.NewSimpleClientset()
			}
			client := negtypes.NewRecordingSvcNegClient(fakeClient, store)
			if _, ok := client.(svcnegclient.Interface); !ok {
				t.Fatalf("recording client does not implement svcnegclient.Interface")
			}

			tc.write(t, client)

			if err := store.EnsureReady(ref); err == nil {
				t.Errorf("EnsureReady(%v) after a write through the recording client = nil, want stale: the write's ResourceVersion 200 is ahead of the informer at 100", ref)
			}
			getter.setRV("200")
			if err := store.EnsureReady(ref); err != nil {
				t.Errorf("EnsureReady(%v) after the informer caught up = %v, want nil", ref, err)
			}
		})
	}
}
