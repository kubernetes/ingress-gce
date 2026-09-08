package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	clientgofeatures "k8s.io/client-go/features"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/l4/resources"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/ingress-gce/pkg/utils/consistency"
	"k8s.io/klog/v2"
)

func TestFinalizerWasRemovedUnexpectedly(t *testing.T) {
	testCases := []struct {
		desc           string
		oldService     *v1.Service
		newService     *v1.Service
		finalizerName  string
		expectedResult bool
	}{
		{
			desc:           "Clean service",
			oldService:     &v1.Service{},
			newService:     &v1.Service{},
			finalizerName:  "random",
			expectedResult: false,
		},
		{
			desc: "Empty finalizers",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			finalizerName:  "random",
			expectedResult: false,
		},
		{
			desc: "Changed L4 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.LegacyILBFinalizer, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", "gke.networking.io/l4-ilb-v1-fake"},
				},
			},
			finalizerName:  common.LegacyILBFinalizer,
			expectedResult: true,
		},
		{
			desc: "Removed L4 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.LegacyILBFinalizer, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			finalizerName:  common.LegacyILBFinalizer,
			expectedResult: true,
		},
		{
			desc: "Added L4 ILB v2 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", common.ILBFinalizerV2},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Service with NetLB Finalizer hasn't changed",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", common.NetLBFinalizerV2},
				},
			},
			finalizerName:  common.NetLBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Finalizer was removed but given name is wrong",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Finalizer was removed and service to be deleted",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{Time: time.Date(2024, 12, 30, 0, 0, 0, 0, time.Local)},
					Finalizers:        []string{common.ILBFinalizerV2},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotResult := finalizerWasRemovedUnexpectedly(tc.oldService, tc.newService, tc.finalizerName)
			if gotResult != tc.expectedResult {
				t.Errorf("finalizerWasRemoved(oldSvc=%v, newSvc=%v, finalizer=%s) returned %v, but expected %v", tc.oldService, tc.newService, tc.finalizerName, gotResult, tc.expectedResult)
			}
		})
	}
}

func TestConditionsEqual(t *testing.T) {
	testCases := []struct {
		desc           string
		left           []metav1.Condition
		right          []metav1.Condition
		expectedResult bool
	}{
		{
			desc:           "Both empty",
			left:           []metav1.Condition{},
			right:          []metav1.Condition{},
			expectedResult: true,
		},
		{
			desc:           "One nil, one empty",
			left:           nil,
			right:          []metav1.Condition{},
			expectedResult: true,
		},
		{
			desc: "Different lengths",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			right:          []metav1.Condition{},
			expectedResult: false,
		},
		{
			desc: "Same conditions, same order",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ReasonA", Message: "MsgA"},
				{Type: "Synced", Status: metav1.ConditionFalse, Reason: "ReasonB", Message: "MsgB"},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ReasonA", Message: "MsgA"},
				{Type: "Synced", Status: metav1.ConditionFalse, Reason: "ReasonB", Message: "MsgB"},
			},
			expectedResult: true,
		},
		{
			desc: "Same conditions, different order",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Synced", Status: metav1.ConditionFalse},
			},
			right: []metav1.Condition{
				{Type: "Synced", Status: metav1.ConditionFalse},
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			expectedResult: true,
		},
		{
			desc: "Missing condition type in right",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Synced", Status: metav1.ConditionTrue},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Other", Status: metav1.ConditionTrue},
			},
			expectedResult: false,
		},
		{
			desc: "Different Status",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse},
			},
			expectedResult: false,
		},
		{
			desc: "Different Reason",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Foo"},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Bar"},
			},
			expectedResult: false,
		},
		{
			desc: "Different Message",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Message: "Foo"},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Message: "Bar"},
			},
			expectedResult: false,
		},
		{
			desc: "Extra fields in left ignored (LastTransitionTime is not checked)",
			left: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: metav1.Now()},
			},
			right: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
			},
			expectedResult: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotResult := conditionsEqual(tc.left, tc.right)
			if gotResult != tc.expectedResult {
				t.Errorf("conditionsEqual() = %v, expected %v", gotResult, tc.expectedResult)
			}
		})
	}
}

func TestMergeConditions(t *testing.T) {
	testCases := []struct {
		desc          string
		existing      []metav1.Condition
		newConditions []metav1.Condition
		expected      []metav1.Condition
	}{
		{
			desc:          "Empty existing and new",
			existing:      nil,
			newConditions: nil,
			expected:      nil,
		},
		{
			desc:          "Add new conditions to empty existing",
			existing:      nil,
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
		},
		{
			desc:          "Replace existing condition",
			existing:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Old"}},
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "New"}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "New"}},
		},
		{
			desc:          "Add new condition to existing",
			existing:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			newConditions: []metav1.Condition{{Type: "Synced", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}, {Type: "Synced", Status: metav1.ConditionTrue}},
		},
		{
			desc: "Mixed update and add",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse},
				{Type: "Old", Status: metav1.ConditionTrue},
			},
			newConditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "New", Status: metav1.ConditionTrue},
			},
			expected: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Old", Status: metav1.ConditionTrue},
				{Type: "New", Status: metav1.ConditionTrue},
			},
		},
		{
			desc:          "Case sensitive types",
			existing:      []metav1.Condition{{Type: "ready", Status: metav1.ConditionFalse}},
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "ready", Status: metav1.ConditionFalse}, {Type: "Ready", Status: metav1.ConditionTrue}},
		},
		{
			desc:          "Duplicate types in newConditions (last wins)",
			existing:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}},
			newConditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse}, {Type: "Ready", Status: metav1.ConditionTrue}},
			expected:      []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := mergeConditions(tc.existing, tc.newConditions, nil)
			if !conditionsEqual(got, tc.expected) {
				t.Errorf("mergeConditions() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestMergeConditions_Remove(t *testing.T) {
	testCases := []struct {
		desc               string
		existing           []metav1.Condition
		newConditions      []metav1.Condition
		conditionsToRemove []string
		expected           []metav1.Condition
	}{
		{
			desc:               "Remove existing condition",
			existing:           []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue}},
			newConditions:      nil,
			conditionsToRemove: []string{"Ready"},
			expected:           nil,
		},
		{
			desc: "Remove one, keep one, add one",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue},
				{Type: "Synced", Status: metav1.ConditionFalse},
			},
			newConditions:      []metav1.Condition{{Type: "Programmed", Status: metav1.ConditionTrue}},
			conditionsToRemove: []string{"Ready"},
			expected: []metav1.Condition{
				{Type: "Synced", Status: metav1.ConditionFalse},
				{Type: "Programmed", Status: metav1.ConditionTrue},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := mergeConditions(tc.existing, tc.newConditions, tc.conditionsToRemove)
			if !conditionsEqual(got, tc.expected) {
				t.Errorf("mergeConditions() = %v, expected %v", got, tc.expected)
			}
		})
	}
}

func TestMergeConditions_PreserveLastTransitionTime(t *testing.T) {
	t1 := metav1.NewTime(time.Now().Add(-10 * time.Minute))
	t2 := metav1.NewTime(time.Now())

	testCases := []struct {
		desc          string
		existing      []metav1.Condition
		newConditions []metav1.Condition
		expected      []metav1.Condition
	}{
		{
			desc: "Preserve transition time when status is same",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t1},
			},
			newConditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t2},
			},
			expected: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t1},
			},
		},
		{
			desc: "Update transition time when status changes",
			existing: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, LastTransitionTime: t1},
			},
			newConditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t2},
			},
			expected: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, LastTransitionTime: t2},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := mergeConditions(tc.existing, tc.newConditions, nil)
			if len(got) != len(tc.expected) {
				t.Fatalf("Got %d conditions, expected %d", len(got), len(tc.expected))
			}
			for i, gotCond := range got {
				expectedCond := tc.expected[i]
				if gotCond.Type != expectedCond.Type {
					t.Errorf("Condition %d type mismatch: got %s, expected %s", i, gotCond.Type, expectedCond.Type)
				}
				if gotCond.Status != expectedCond.Status {
					t.Errorf("Condition %d status mismatch: got %v, expected %v", i, gotCond.Status, expectedCond.Status)
				}
				if !gotCond.LastTransitionTime.Equal(&expectedCond.LastTransitionTime) {
					t.Errorf("Condition %d LastTransitionTime mismatch: got %v, expected %v", i, gotCond.LastTransitionTime, expectedCond.LastTransitionTime)
				}
			}
		})
	}
}

func TestMergeConditions_Sorting(t *testing.T) {
	newConditions := []metav1.Condition{
		{Type: "C", Status: metav1.ConditionTrue},
		{Type: "A", Status: metav1.ConditionTrue},
		{Type: "B", Status: metav1.ConditionTrue},
	}
	expectedOrder := []string{"A", "B", "C"}

	got := mergeConditions(nil, newConditions, nil)

	if len(got) != len(expectedOrder) {
		t.Fatalf("Got %d conditions, expected %d", len(got), len(expectedOrder))
	}

	for i, cond := range got {
		if cond.Type != expectedOrder[i] {
			t.Errorf("At index %d: got Type %q, expected %q", i, cond.Type, expectedOrder[i])
		}
	}
}

// newTestConsistencyStore returns a real ConsistencyStore for services, wired
// to a fake informer store whose ResourceVersion the test controls.
func newTestConsistencyStore(rv string) (consistency.ConsistencyStore, *consistency.FakeRVGetter) {
	getter := consistency.NewFakeRVGetter(rv)
	store := consistency.NewConsistencyStore(map[schema.GroupResource]consistency.LastSyncRVGetter{servicesGroupResource: getter})
	return store, getter
}

// TestClearServiceRecord covers the handler of the informer delete event:
// the plain object, the tombstone an informer delivers when the final state
// is unknown, and the UID that keeps a deletion event for an old incarnation
// from dropping the record of a service recreated since.
func TestClearServiceRecord(t *testing.T) {
	svc := &v1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "test-svc", UID: "test-uid"}}
	ref := serviceRef(svc.Namespace, svc.Name)

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
			store, _ := newTestConsistencyStore("100")
			store.WroteAt(ref, svc.UID, "150")

			clearServiceRecord(store, tc.deleted)

			if gotCleared := store.EnsureReady(ref) == nil; gotCleared != tc.wantCleared {
				t.Errorf("clearServiceRecord(store, %T): record cleared = %t, want %t", tc.deleted, gotCleared, tc.wantCleared)
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

func TestNewServiceConsistencyStore_DisabledAndUnsupported(t *testing.T) {
	informer := informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0).Core().V1().Services().Informer()

	if store := NewServiceConsistencyStore(informer, false, klog.TODO()); !consistency.IsNoop(store) {
		t.Errorf("NewServiceConsistencyStore(enabled=false) = %T, want no-op store", store)
	}

	orig := clientgofeatures.FeatureGates()
	t.Cleanup(func() { clientgofeatures.ReplaceFeatureGates(orig) })
	clientgofeatures.ReplaceFeatureGates(fakeAtomicFIFOGates{enabled: false})

	if store := NewServiceConsistencyStore(informer, true, klog.TODO()); !consistency.IsNoop(store) {
		t.Errorf("NewServiceConsistencyStore(enabled=true, AtomicFIFO=false) = %T, want no-op store", store)
	}
}

func TestNewServiceConsistencyStore_ClearsRecordOnInformerDelete(t *testing.T) {
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
	store := NewServiceConsistencyStore(serviceInformer, true, klog.TODO())
	if consistency.IsNoop(store) {
		t.Fatalf("NewServiceConsistencyStore(enabled=true) returned no-op store, want real store")
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
	ref := serviceRef(svc.Namespace, svc.Name)
	store.WroteAt(ref, svc.UID, "999999")

	// Deleting the Service through the client triggers the informer's
	// DeleteFunc registered by NewServiceConsistencyStore, dropping the record.
	if err := kubeClient.CoreV1().Services(svc.Namespace).Delete(t.Context(), svc.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete(%s/%s) = %v, want nil", svc.Namespace, svc.Name, err)
	}
	if err := wait.PollUntilContextTimeout(t.Context(), 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return store.EnsureReady(ref) == nil, nil
	}); err != nil {
		t.Errorf("record for %v was not cleared by the informer delete handler: %v", ref, store.EnsureReady(ref))
	}
}

func TestServiceWriteHelpersRecordInConsistencyStore(t *testing.T) {
	t.Parallel()

	baseSvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "test-ns",
			Name:            "test-svc",
			UID:             "test-uid",
			ResourceVersion: "100",
			Finalizers:      []string{common.ILBFinalizerV2, common.NetLBFinalizerV2},
			Annotations:     map[string]string{resources.L4ResourceAnnotationKeys[0]: "old-fw"},
		},
	}

	for _, tc := range []struct {
		name  string
		write func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error
	}{
		{
			name: "patchServiceMetadata",
			write: func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error {
				meta := svc.ObjectMeta.DeepCopy()
				meta.Annotations = map[string]string{"example.com/key": "val"}
				return patchServiceMetadata(ctx, store, svc, *meta)
			},
		},
		{
			name: "updateL4ResourcesAnnotations",
			write: func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error {
				return updateL4ResourcesAnnotations(ctx, store, svc, map[string]string{resources.L4ResourceAnnotationKeys[0]: "new-fw"}, klog.TODO())
			},
		},
		{
			name: "deleteAnnotation",
			write: func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error {
				return deleteAnnotation(ctx, store, svc, resources.L4ResourceAnnotationKeys[0], klog.TODO())
			},
		},
		{
			name: "ensureServiceFinalizer",
			write: func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error {
				return ensureServiceFinalizer(ctx, store, svc, "gke.networking.io/new-finalizer", klog.TODO())
			},
		},
		{
			name: "ensureDeleteServiceFinalizer",
			write: func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error {
				return ensureDeleteServiceFinalizer(ctx, store, svc, common.ILBFinalizerV2, klog.TODO())
			},
		},
		{
			name: "ensureServiceDeleteFinalizers",
			write: func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error {
				return ensureServiceDeleteFinalizers(ctx, store, svc, []string{common.ILBFinalizerV2, common.NetLBFinalizerV2}, klog.TODO())
			},
		},
		{
			name: "updateServiceStatus",
			write: func(ctx *ingctx.ControllerContext, store consistency.ConsistencyStore, svc *v1.Service) error {
				status := &v1.LoadBalancerStatus{Ingress: []v1.LoadBalancerIngress{{IP: "10.0.0.1"}}}
				return updateServiceStatus(ctx, store, svc, status, nil, nil, klog.TODO())
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stored := baseSvc.DeepCopy()
			stored.ResourceVersion = "150"
			ctx := &ingctx.ControllerContext{KubeClient: fake.NewSimpleClientset(stored)}
			store, getter := newTestConsistencyStore("100")
			ref := serviceRef(baseSvc.Namespace, baseSvc.Name)

			if err := tc.write(ctx, store, baseSvc.DeepCopy()); err != nil {
				t.Fatalf("%s() = %v, want nil", tc.name, err)
			}
			if err := store.EnsureReady(ref); !consistency.IsStall(err) {
				t.Errorf("EnsureReady(%v) after %s() = %v, want a consistency stall at ResourceVersion 150", ref, tc.name, err)
			}
			getter.SetRV("150")
			if err := store.EnsureReady(ref); err != nil {
				t.Errorf("EnsureReady(%v) after informer caught up = %v, want nil", ref, err)
			}
		})
	}
}

func TestServiceWriteHelpers_SkipFailedWrite(t *testing.T) {
	t.Parallel()

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
	ctx := &ingctx.ControllerContext{KubeClient: kubeClient}
	store, _ := newTestConsistencyStore("100")

	status := &v1.LoadBalancerStatus{Ingress: []v1.LoadBalancerIngress{{IP: "10.0.0.1"}}}
	if err := updateServiceStatus(ctx, store, svc, status, nil, nil, klog.TODO()); err == nil {
		t.Fatal("updateServiceStatus() = nil, want reactor error")
	}
	ref := serviceRef(svc.Namespace, svc.Name)
	if err := store.EnsureReady(ref); err != nil {
		t.Errorf("EnsureReady(%v) after failed updateServiceStatus() = %v, want nil", ref, err)
	}
}
