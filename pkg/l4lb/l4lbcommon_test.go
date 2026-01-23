package l4lb

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/utils/common"
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
			got := mergeConditions(tc.existing, tc.newConditions)
			if !conditionsEqual(got, tc.expected) {
				t.Errorf("mergeConditions() = %v, expected %v", got, tc.expected)
			}
		})
	}
}
