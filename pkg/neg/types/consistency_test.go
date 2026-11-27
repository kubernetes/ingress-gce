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

package types

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgofeatures "k8s.io/client-go/features"
	k8stesting "k8s.io/client-go/testing"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	negfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	svcneginformers "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions"
	"k8s.io/ingress-gce/pkg/utils/consistency"
	"k8s.io/klog/v2"
)

const (
	consistencyTestNamespace = "test-ns"
	consistencyTestNegName   = "test-neg"
)

// newConsistencyTestSvcNeg returns a SvcNeg CR at the given ResourceVersion.
func newConsistencyTestSvcNeg(rv string) *negv1beta1.ServiceNetworkEndpointGroup {
	return &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       consistencyTestNamespace,
			Name:            consistencyTestNegName,
			UID:             "neg-uid",
			ResourceVersion: rv,
		},
	}
}

// newConsistencyTestStore returns a real ConsistencyStore for SvcNeg CRs,
// wired to a fake informer store whose ResourceVersion the test controls.
func newConsistencyTestStore(rv string) (consistency.ConsistencyStore, *consistency.FakeRVGetter) {
	getter := consistency.NewFakeRVGetter(rv)
	store := consistency.NewConsistencyStore(map[schema.GroupResource]consistency.LastSyncRVGetter{
		SvcNegGroupResource: getter,
	}, klog.TODO())
	return store, getter
}

// TestRecordingSvcNegClient covers the client wrapper: a write through any
// of its write verbs must leave a record the store then stalls on, without
// the call site recording by hand.
func TestRecordingSvcNegClient(t *testing.T) {
	t.Parallel()

	ref := SvcNegRef(consistencyTestNamespace, consistencyTestNegName)

	for _, tc := range []struct {
		desc string
		// existing is in the fake clientset before the write. The fake does
		// not assign ResourceVersions, so the version 200 of the existing
		// (or created) CR is what the write returns.
		existing []runtime.Object
		write    func(ctx context.Context, client svcnegclient.Interface) error
	}{
		{
			desc: "Create",
			write: func(ctx context.Context, client svcnegclient.Interface) error {
				_, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).Create(ctx, newConsistencyTestSvcNeg("200"), metav1.CreateOptions{})
				return err
			},
		},
		{
			desc:     "Update",
			existing: []runtime.Object{newConsistencyTestSvcNeg("200")},
			write: func(ctx context.Context, client svcnegclient.Interface) error {
				_, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).Update(ctx, newConsistencyTestSvcNeg("200"), metav1.UpdateOptions{})
				return err
			},
		},
		{
			desc:     "UpdateStatus",
			existing: []runtime.Object{newConsistencyTestSvcNeg("200")},
			write: func(ctx context.Context, client svcnegclient.Interface) error {
				_, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).UpdateStatus(ctx, newConsistencyTestSvcNeg("200"), metav1.UpdateOptions{})
				return err
			},
		},
		{
			desc:     "Patch",
			existing: []runtime.Object{newConsistencyTestSvcNeg("200")},
			write: func(ctx context.Context, client svcnegclient.Interface) error {
				_, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).Patch(ctx, consistencyTestNegName, types.MergePatchType, []byte(`{"status":{}}`), metav1.PatchOptions{})
				return err
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// The informer has observed ResourceVersion 100.
			store, getter := newConsistencyTestStore("100")
			client := NewRecordingSvcNegClient(negfake.NewSimpleClientset(tc.existing...), store)

			if err := tc.write(t.Context(), client); err != nil {
				t.Fatalf("%s() through the recording client = %v, want nil", tc.desc, err)
			}

			if err := store.EnsureReady(ref); !consistency.IsStall(err) {
				t.Errorf("EnsureReady(%v) after %s() = %v, want a consistency stall: the write returned ResourceVersion 200 and the informer is at 100", ref, tc.desc, err)
			}
			getter.SetRV("200")
			if err := store.EnsureReady(ref); err != nil {
				t.Errorf("EnsureReady(%v) after the informer observed the write = %v, want nil", ref, err)
			}
		})
	}
}

// TestRecordingSvcNegClientIgnoresFailedWrite verifies that a write that
// returns an error leaves no record, whatever object comes back with it.
func TestRecordingSvcNegClientIgnoresFailedWrite(t *testing.T) {
	t.Parallel()

	ref := SvcNegRef(consistencyTestNamespace, consistencyTestNegName)
	// The informer has observed ResourceVersion 100.
	store, _ := newConsistencyTestStore("100")
	fakeClient := negfake.NewSimpleClientset(newConsistencyTestSvcNeg("200"))
	fakeClient.PrependReactor("patch", "servicenetworkendpointgroups", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, newConsistencyTestSvcNeg("200"), errors.New("patch failed")
	})
	client := NewRecordingSvcNegClient(fakeClient, store)

	_, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(consistencyTestNamespace).Patch(t.Context(), consistencyTestNegName, types.MergePatchType, []byte(`{"status":{}}`), metav1.PatchOptions{})
	if err == nil {
		t.Fatal("Patch() through the recording client = nil, want the reactor's error")
	}

	if err := store.EnsureReady(ref); err != nil {
		t.Errorf("EnsureReady(%v) after a failed Patch() = %v, want nil: a failed write must not be recorded", ref, err)
	}
}

func TestSvcNegPreconditionRV(t *testing.T) {
	t.Parallel()

	enabledStore, _ := newConsistencyTestStore("100")

	for _, tc := range []struct {
		desc  string
		store consistency.ConsistencyStore
		want  string
	}{
		{
			desc:  "consistency checking enabled",
			store: enabledStore,
			want:  "200",
		},
		{
			desc:  "consistency checking disabled",
			store: consistency.NewNoopConsistencyStore(),
			want:  "",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			if got := SvcNegPreconditionRV(tc.store, newConsistencyTestSvcNeg("200")); got != tc.want {
				t.Errorf("SvcNegPreconditionRV() = %q, want %q", got, tc.want)
			}
		})
	}
}

type fakeAtomicFIFOGates struct {
	enabled bool
}

func (g fakeAtomicFIFOGates) Enabled(key clientgofeatures.Feature) bool {
	if key == clientgofeatures.AtomicFIFO {
		return g.enabled
	}
	return true
}

func TestNewSvcNegConsistencyStore(t *testing.T) {
	informer := svcneginformers.NewSharedInformerFactory(negfake.NewSimpleClientset(), 0).Networking().V1beta1().ServiceNetworkEndpointGroups().Informer()

	if store := NewSvcNegConsistencyStore(informer, false, klog.TODO()); !consistency.IsNoop(store) {
		t.Errorf("NewSvcNegConsistencyStore(enabled=false) = %T, want no-op store", store)
	}
	if store := NewSvcNegConsistencyStore(nil, true, klog.TODO()); !consistency.IsNoop(store) {
		t.Errorf("NewSvcNegConsistencyStore(informer=nil, enabled=true) = %T, want no-op store", store)
	}
	if store := NewSvcNegConsistencyStore(informer, true, klog.TODO()); consistency.IsNoop(store) {
		t.Errorf("NewSvcNegConsistencyStore(enabled=true) = %T, want functioning store", store)
	}

	orig := clientgofeatures.FeatureGates()
	t.Cleanup(func() { clientgofeatures.ReplaceFeatureGates(orig) })
	clientgofeatures.ReplaceFeatureGates(fakeAtomicFIFOGates{enabled: false})

	if store := NewSvcNegConsistencyStore(informer, true, klog.TODO()); !consistency.IsNoop(store) {
		t.Errorf("NewSvcNegConsistencyStore(enabled=true, AtomicFIFO=false) = %T, want no-op store", store)
	}
}
