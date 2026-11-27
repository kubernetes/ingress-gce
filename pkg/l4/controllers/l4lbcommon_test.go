package controllers

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/l4/resources"
	"k8s.io/ingress-gce/pkg/l4/serviceconsistency"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stored := baseSvc.DeepCopy()
			stored.ResourceVersion = "150"
			ctx := &ingctx.ControllerContext{KubeClient: fake.NewSimpleClientset(stored)}
			store, getter := consistency.NewStoreWithFakeRV(serviceconsistency.GroupResource, "100")
			ref := serviceconsistency.Ref(baseSvc.Namespace, baseSvc.Name)

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
