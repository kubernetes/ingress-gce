package l4lb

import (
	context2 "context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog/v2"
)

func GetDefaultAnnotations() map[string]string {
	return map[string]string{
		"other_annotations":                     "ok",
		"networking.gke.io/load-balancer-type":  "Internal",
		"service.kubernetes.io/backend-service": "ok",
	}
}

// NewL4ILBService creates a Service of type LoadBalancer with the Internal annotation.
func NewL4ILBService(onlyLocal bool, port int) *v1.Service {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc1",
			Namespace:   "default",
			Annotations: GetDefaultAnnotations(),
		},
		Spec: v1.ServiceSpec{
			Type:            v1.ServiceTypeLoadBalancer,
			SessionAffinity: v1.ServiceAffinityClientIP,
			Ports: []v1.ServicePort{
				{Name: "testport", Port: int32(port), Protocol: "TCP"},
			},
		},
	}
	if onlyLocal {
		svc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	}
	return svc
}

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

func TestUpdateServiceInformation(t *testing.T) {
	testCases := []struct {
		desc                string
		enableDualStack     bool
		newStatus           *v1.LoadBalancerStatus
		newAnnotations      map[string]string
		expectedAnnotations map[string]string
	}{
		{
			desc:            "Add annotations",
			enableDualStack: true,
			newStatus:       nil,
			newAnnotations: map[string]string{
				"service.kubernetes.io/firewall-rule-ipv6": "ok",
				"service.kubernetes.io/backend-service":    "ok",
				"networking.gke.io/load-balancer-type":     "Internal",
			},
			expectedAnnotations: map[string]string{
				"other_annotations":                        "ok",
				"networking.gke.io/load-balancer-type":     "Internal",
				"service.kubernetes.io/backend-service":    "ok",
				"service.kubernetes.io/firewall-rule-ipv6": "ok",
			},
		},
		{
			desc:            "Reset annotations",
			enableDualStack: false,
			newStatus:       nil,
			newAnnotations:  map[string]string{},
			expectedAnnotations: map[string]string{
				"other_annotations":                    "ok",
				"networking.gke.io/load-balancer-type": "Internal",
			},
		},
		{
			desc:            "Only change status",
			enableDualStack: false,
			newStatus: &v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP: "127.0.0.1",
					},
				},
			},
			newAnnotations:      GetDefaultAnnotations(),
			expectedAnnotations: GetDefaultAnnotations(),
		},
		{
			desc:            "Change status and reset Annotations",
			enableDualStack: false,
			newStatus: &v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP: "127.0.0.1",
					},
				},
			},
			newAnnotations: map[string]string{},
			expectedAnnotations: map[string]string{
				"other_annotations":                    "ok",
				"networking.gke.io/load-balancer-type": "Internal",
			},
		},
		{
			desc:            "Remove annotations",
			enableDualStack: true,
			newStatus:       nil,
			newAnnotations: map[string]string{
				"service.kubernetes.io/firewall-rule-ipv6": "ok",
			},
			expectedAnnotations: map[string]string{
				"other_annotations":                        "ok",
				"networking.gke.io/load-balancer-type":     "Internal",
				"service.kubernetes.io/firewall-rule-ipv6": "ok",
			},
		},
	}

	logger := klog.TODO()

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {

			l4c := newServiceController(t, newFakeGCE())

			svc := NewL4ILBService(true, 8080)
			addILBService(l4c, svc)

			newSvc, err := l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, metav1.GetOptions{})

			// l4c.processServiceCreateOrUpdate(svc, logger)
			err = updateServiceInformation(l4c.ctx, tc.enableDualStack, newSvc, tc.newStatus, tc.newAnnotations, logger)

			newSvc, err = l4c.client.CoreV1().Services(svc.Namespace).Get(context2.TODO(), svc.Name, metav1.GetOptions{})
			if tc.newStatus != nil {
				assert.Equal(t, *tc.newStatus, newSvc.Status.LoadBalancer)
			}
			assert.Equal(t, tc.expectedAnnotations, newSvc.ObjectMeta.Annotations)
			assert.NoError(t, err)
		})
	}
}
