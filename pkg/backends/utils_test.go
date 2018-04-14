package backends

import (
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestServicePort(t *testing.T) {

	objMeta := meta_v1.ObjectMeta{
		Namespace:   "bar",
		Annotations: map[string]string{"http": "HTTP"},
	}

	testCases := []struct {
		ib              v1beta1.IngressBackend
		svc             v1.Service
		expectedSvcPort ServicePort
	}{
		{
			v1beta1.IngressBackend{ServiceName: "foo", ServicePort: intstr.FromInt(80)},
			v1.Service{
				ObjectMeta: objMeta,
				Spec: v1.ServiceSpec{
					Type:  v1.ServiceTypeNodePort,
					Ports: []v1.ServicePort{v1.ServicePort{Port: 80, NodePort: 30000}},
				},
			},
			ServicePort{
				SvcName:  types.NamespacedName{Namespace: "bar", Name: "foo"},
				SvcPort:  intstr.FromInt(80),
				NodePort: 30000,
				Protocol: "HTTP",
			},
		},
		{
			v1beta1.IngressBackend{ServiceName: "foo", ServicePort: intstr.FromInt(80)},
			v1.Service{
				ObjectMeta: objMeta,
				Spec: v1.ServiceSpec{
					Type:  v1.ServiceTypeNodePort,
					Ports: []v1.ServicePort{v1.ServicePort{Port: 443, NodePort: 30000, Protocol: "TCP"}, v1.ServicePort{Port: 80, NodePort: 30001}},
				},
			},
			ServicePort{
				SvcName:  types.NamespacedName{Namespace: "bar", Name: "foo"},
				SvcPort:  intstr.FromInt(80),
				NodePort: 30001,
				Protocol: "HTTP",
			},
		},
		{
			v1beta1.IngressBackend{ServiceName: "foo", ServicePort: intstr.FromInt(80)},
			v1.Service{
				ObjectMeta: objMeta,
				Spec: v1.ServiceSpec{
					Type:  v1.ServiceTypeClusterIP,
					Ports: []v1.ServicePort{v1.ServicePort{Port: 80, NodePort: 30000}},
				},
			},
			ServicePort{},
		},
	}

	for _, testCase := range testCases {
		res, _ := servicePort(testCase.ib, testCase.svc)
		if res != testCase.expectedSvcPort {
			t.Errorf("Result ServicePort: %v does not match expected: %v", res, testCase.expectedSvcPort)
		}
	}
}
