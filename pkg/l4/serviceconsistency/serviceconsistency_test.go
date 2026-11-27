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

package serviceconsistency

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgofeatures "k8s.io/client-go/features"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/utils/consistency"
	"k8s.io/klog/v2"
)

// TestClearRecord covers the handler of the informer delete event: the plain
// object, the tombstone an informer delivers when the final state is unknown,
// and the UID that keeps a deletion event for an old incarnation from
// dropping the record of a service recreated since.
func TestClearRecord(t *testing.T) {
	svc := &v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "test-svc", UID: "test-uid"}}
	ref := Ref(svc.Namespace, svc.Name)

	for _, tc := range []struct {
		desc        string
		deleted     interface{}
		wantCleared bool
	}{
		{
			desc:        "service",
			deleted:     svc,
			wantCleared: true,
		},
		{
			desc:        "tombstone",
			deleted:     cache.DeletedFinalStateUnknown{Key: "test-ns/test-svc", Obj: svc},
			wantCleared: true,
		},
		{
			desc:        "old incarnation",
			deleted:     &v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "test-svc", UID: "old-uid"}},
			wantCleared: false,
		},
		{
			desc:        "unexpected type",
			deleted:     "not-a-service",
			wantCleared: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// A write at 150 that the informer, at 100, has not observed.
			store, _ := consistency.NewStoreWithFakeRV(GroupResource, "100")
			store.WroteAt(ref, svc.UID, "150")

			clearRecord(store, tc.deleted)

			if gotCleared := store.EnsureReady(ref) == nil; gotCleared != tc.wantCleared {
				t.Errorf("clearRecord(store, %T): record cleared = %t, want %t", tc.deleted, gotCleared, tc.wantCleared)
			}
		})
	}
}

// TestClearReplacedRecord covers the handler of the informer update event,
// which is how a relist reports a service that was deleted and recreated
// while the informer was not watching.
func TestClearReplacedRecord(t *testing.T) {
	const recordedUID = types.UID("test-uid")
	newSvc := func(uid types.UID) *v1.Service {
		return &v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "test-svc", UID: uid}}
	}
	ref := Ref("test-ns", "test-svc")

	for _, tc := range []struct {
		desc        string
		oldObj      interface{}
		newObj      interface{}
		wantCleared bool
	}{
		{
			desc:        "recreated under the same name",
			oldObj:      newSvc(recordedUID),
			newObj:      newSvc("new-uid"),
			wantCleared: true,
		},
		{
			desc:        "same incarnation updated",
			oldObj:      newSvc(recordedUID),
			newObj:      newSvc(recordedUID),
			wantCleared: false,
		},
		{
			desc:        "record is of the new incarnation",
			oldObj:      newSvc("old-uid"),
			newObj:      newSvc(recordedUID),
			wantCleared: false,
		},
		{
			desc:        "unexpected type",
			oldObj:      "not-a-service",
			newObj:      newSvc("new-uid"),
			wantCleared: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// A write at 150 that the informer, at 100, has not observed.
			store, _ := consistency.NewStoreWithFakeRV(GroupResource, "100")
			store.WroteAt(ref, recordedUID, "150")

			clearReplacedRecord(store, tc.oldObj, tc.newObj)

			if gotCleared := store.EnsureReady(ref) == nil; gotCleared != tc.wantCleared {
				t.Errorf("clearReplacedRecord(store, %T, %T): record cleared = %t, want %t", tc.oldObj, tc.newObj, gotCleared, tc.wantCleared)
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

func TestNewStore_DisabledAndUnsupported(t *testing.T) {
	informer := informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0).Core().V1().Services().Informer()

	if store := NewStore(informer, false, klog.TODO()); !consistency.IsNoop(store) {
		t.Errorf("NewStore(enabled=false) = %T, want no-op store", store)
	}

	orig := clientgofeatures.FeatureGates()
	t.Cleanup(func() { clientgofeatures.ReplaceFeatureGates(orig) })
	clientgofeatures.ReplaceFeatureGates(fakeAtomicFIFOGates{enabled: false})

	if store := NewStore(informer, true, klog.TODO()); !consistency.IsNoop(store) {
		t.Errorf("NewStore(enabled=true, AtomicFIFO=false) = %T, want no-op store", store)
	}
}

func TestNewStore_ClearsRecordOnInformerDelete(t *testing.T) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test-ns",
			Name:            "test-svc",
			UID:             "test-uid",
			ResourceVersion: "100",
		},
	}
	kubeClient := fake.NewSimpleClientset(svc)
	factory := informers.NewSharedInformerFactory(kubeClient, 0)
	serviceInformer := factory.Core().V1().Services().Informer()
	store := NewStore(serviceInformer, true, klog.TODO())
	if consistency.IsNoop(store) {
		t.Fatalf("NewStore(enabled=true) returned no-op store, want real store")
	}

	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })
	factory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, serviceInformer.HasSynced) {
		t.Fatal("failed to sync service informer")
	}

	// Wait until the initial Service is in the informer's indexer.
	if err := wait.PollUntilContextTimeout(t.Context(), 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		_, exists, err := serviceInformer.GetIndexer().GetByKey("test-ns/test-svc")
		return exists, err
	}); err != nil {
		t.Fatalf("waiting for service in informer: %v", err)
	}

	// Record a write ahead of any ResourceVersion the fake watch reports.
	ref := Ref(svc.Namespace, svc.Name)
	store.WroteAt(ref, svc.UID, "999999")

	// Deleting the Service through the client triggers the informer's
	// DeleteFunc registered by NewStore, dropping the record.
	if err := kubeClient.CoreV1().Services(svc.Namespace).Delete(t.Context(), svc.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete(%s/%s) = %v, want nil", svc.Namespace, svc.Name, err)
	}
	if err := wait.PollUntilContextTimeout(t.Context(), 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return store.EnsureReady(ref) == nil, nil
	}); err != nil {
		t.Errorf("record for %v was not cleared by the informer delete handler: %v", ref, store.EnsureReady(ref))
	}
}

func TestNewStore_ClearsRecordOnInformerUpdateToNewUID(t *testing.T) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test-ns",
			Name:            "test-svc",
			UID:             "test-uid",
			ResourceVersion: "100",
		},
	}
	kubeClient := fake.NewSimpleClientset(svc)
	factory := informers.NewSharedInformerFactory(kubeClient, 0)
	serviceInformer := factory.Core().V1().Services().Informer()
	store := NewStore(serviceInformer, true, klog.TODO())
	if consistency.IsNoop(store) {
		t.Fatalf("NewStore(enabled=true) returned no-op store, want real store")
	}

	stopCh := make(chan struct{})
	t.Cleanup(func() { close(stopCh) })
	factory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, serviceInformer.HasSynced) {
		t.Fatal("failed to sync service informer")
	}

	// Wait until the initial Service is in the informer's indexer.
	if err := wait.PollUntilContextTimeout(t.Context(), 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		_, exists, err := serviceInformer.GetIndexer().GetByKey("test-ns/test-svc")
		return exists, err
	}); err != nil {
		t.Fatalf("waiting for service in informer: %v", err)
	}

	// Record a write ahead of any ResourceVersion the fake watch reports.
	ref := Ref(svc.Namespace, svc.Name)
	store.WroteAt(ref, svc.UID, "999999")
	if err := store.EnsureReady(ref); !consistency.IsStall(err) {
		t.Fatalf("EnsureReady(%v) before the update = %v, want a consistency stall", ref, err)
	}

	// An update that carries another UID under the same name is what the
	// informer delivers for a service recreated behind its back. It triggers
	// the UpdateFunc registered by NewStore, dropping the record.
	recreated := svc.DeepCopy()
	recreated.UID = "recreated-uid"
	if _, err := kubeClient.CoreV1().Services(svc.Namespace).Update(t.Context(), recreated, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("Update(%s/%s) = %v, want nil", svc.Namespace, svc.Name, err)
	}
	if err := wait.PollUntilContextTimeout(t.Context(), 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return store.EnsureReady(ref) == nil, nil
	}); err != nil {
		t.Errorf("record for %v was not cleared by the informer update handler: %v", ref, store.EnsureReady(ref))
	}
}

func TestPatchMetadata_RecordsWrite(t *testing.T) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test-ns",
			Name:            "test-svc",
			UID:             "test-uid",
			ResourceVersion: "100",
		},
	}
	stored := svc.DeepCopy()
	stored.ResourceVersion = "150"
	kubeClient := fake.NewSimpleClientset(stored)
	store, getter := consistency.NewStoreWithFakeRV(GroupResource, "100")
	ref := Ref(svc.Namespace, svc.Name)

	newObjectMeta := svc.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = map[string]string{"example.com/key": "val"}
	if err := PatchMetadata(kubeClient.CoreV1(), store, svc, *newObjectMeta); err != nil {
		t.Fatalf("PatchMetadata() = %v, want nil", err)
	}
	if err := store.EnsureReady(ref); !consistency.IsStall(err) {
		t.Errorf("EnsureReady(%v) after PatchMetadata() = %v, want a consistency stall at ResourceVersion 150", ref, err)
	}
	getter.SetRV("150")
	if err := store.EnsureReady(ref); err != nil {
		t.Errorf("EnsureReady(%v) after informer caught up = %v, want nil", ref, err)
	}
}

func TestPatchMetadata_SkipsFailedWrite(t *testing.T) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test-ns",
			Name:            "test-svc",
			UID:             "test-uid",
			ResourceVersion: "100",
		},
	}
	stored := svc.DeepCopy()
	stored.ResourceVersion = "150"
	kubeClient := fake.NewSimpleClientset(stored)
	kubeClient.PrependReactor("patch", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, stored, errors.New("apiserver unavailable")
	})
	store, _ := consistency.NewStoreWithFakeRV(GroupResource, "100")

	newObjectMeta := svc.ObjectMeta.DeepCopy()
	newObjectMeta.Annotations = map[string]string{"example.com/key": "val"}
	if err := PatchMetadata(kubeClient.CoreV1(), store, svc, *newObjectMeta); err == nil {
		t.Fatal("PatchMetadata() = nil, want reactor error")
	}
	ref := Ref(svc.Namespace, svc.Name)
	if err := store.EnsureReady(ref); err != nil {
		t.Errorf("EnsureReady(%v) after failed PatchMetadata() = %v, want nil", ref, err)
	}
}
