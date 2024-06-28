package l4lb

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/common"
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

type EmitEventMock struct {
	eventtype string
	reason    string
	message   string
}

type MockRecorder struct {
	Events *[]EmitEventMock
}

func (r MockRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	*r.Events = append(*r.Events, EmitEventMock{
		eventtype,
		reason,
		message,
	})
}

func (r MockRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	message := fmt.Sprintf(messageFmt, args...) // Use fmt.Sprintf to format the message
	*r.Events = append(*r.Events, EmitEventMock{
		eventtype,
		reason,
		message,
	})
}

func (r MockRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	message := fmt.Sprintf(messageFmt, args...) // Format the message
	*r.Events = append(*r.Events, EmitEventMock{
		eventtype,
		reason,
		message,
	})
}

func TestEmitIpFamiliesStackEvent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		ipFamilies    []api_v1.IPFamily
		service       *v1.Service
		expectedEvent EmitEventMock
	}{
		{
			name:       "L4 Single Stack",
			service:    test.NewL4ILBService(false, 8080),
			ipFamilies: []v1.IPFamily{},
			expectedEvent: EmitEventMock{
				eventtype: "Normal",
				reason:    "SyncLoadBalancerSuccessful",
				message:   "Successfully ensured L4 load balancer resources",
			},
		},
		{
			name:       "L4 Single Stack IPv4",
			service:    test.NewL4ILBService(false, 8080),
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol},
			expectedEvent: EmitEventMock{
				eventtype: "Normal",
				reason:    "SyncLoadBalancerSuccessful",
				message:   "Successfully ensured IPv4 load balancer resources",
			},
		},
		{
			name:       "L4 Dual Stack",
			service:    test.NewL4ILBDualStackService(8080, api_v1.ProtocolTCP, []api_v1.IPFamily{api_v1.IPv4Protocol, api_v1.IPv6Protocol}, api_v1.ServiceExternalTrafficPolicyTypeCluster),
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			expectedEvent: EmitEventMock{
				eventtype: "Normal",
				reason:    "SyncLoadBalancerSuccessful",
				message:   "Successfully ensured IPv4 IPv6 load balancer resources",
			},
		},
		{
			name:       "L4 Net Single Stack",
			service:    test.NewL4NetLBRBSService(8080),
			ipFamilies: []v1.IPFamily{},
			expectedEvent: EmitEventMock{
				eventtype: "Normal",
				reason:    "SyncLoadBalancerSuccessful",
				message:   "Successfully ensured L4 load balancer resources",
			},
		},
		{
			name:       "L4 Net Single Stack IPv4",
			service:    test.NewL4NetLBRBSService(8080),
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol},
			expectedEvent: EmitEventMock{
				eventtype: "Normal",
				reason:    "SyncLoadBalancerSuccessful",
				message:   "Successfully ensured IPv4 load balancer resources",
			},
		},
		{
			name:       "L4 Net Dual Stack",
			service:    test.NewL4NetLBRBSService(8080),
			ipFamilies: []v1.IPFamily{v1.IPv4Protocol, v1.IPv6Protocol},
			expectedEvent: EmitEventMock{
				eventtype: "Normal",
				reason:    "SyncLoadBalancerSuccessful",
				message:   "Successfully ensured IPv4 IPv6 load balancer resources",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture range variable for parallel tests

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			eventSlice := []EmitEventMock{}
			recorder := MockRecorder{Events: &eventSlice}
			l4c := newServiceController(t, newFakeGCE())

			test.MustCreateDualStackClusterSubnet(t, l4c.ctx.Cloud, "INTERNAL")

			l4c.ctx.Recorders[tc.service.Namespace] = recorder
			tc.service.Spec.IPFamilies = tc.ipFamilies
			addILBService(l4c, tc.service)
			addNEG(l4c, tc.service)
			l4c.processServiceCreateOrUpdate(tc.service, klog.TODO())

			require.Len(t, *recorder.Events, 1)
			assert.Equal(t, tc.expectedEvent, (*recorder.Events)[0])
		})
	}

}
