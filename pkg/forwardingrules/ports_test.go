package forwardingrules_test

import (
	"reflect"
	"testing"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/forwardingrules"
)

func TestGetPorts(t *testing.T) {
	mixedServicePorts := []api_v1.ServicePort{
		{
			Protocol:   api_v1.ProtocolTCP,
			Port:       1001,
			TargetPort: intstr.FromInt(1010),
		},
		{
			Protocol:   api_v1.ProtocolUDP,
			Port:       1001,
			TargetPort: intstr.FromString("namedport"),
		},
		{
			Protocol: api_v1.ProtocolTCP,
			Port:     8080,
		},
	}

	testCases := []struct {
		desc         string
		servicePorts []api_v1.ServicePort
		protocol     api_v1.Protocol
		want         []string
	}{
		{
			desc:         "empty",
			servicePorts: []api_v1.ServicePort{},
			protocol:     api_v1.ProtocolTCP,
			want:         []string{},
		},
		{
			desc:         "udp",
			servicePorts: mixedServicePorts,
			protocol:     api_v1.ProtocolUDP,
			want:         []string{"1001"},
		},
		{
			desc:         "tcp",
			servicePorts: mixedServicePorts,
			protocol:     api_v1.ProtocolTCP,
			want:         []string{"1001", "8080"},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := forwardingrules.GetPorts(tC.servicePorts, tC.protocol)
			if !reflect.DeepEqual(got, tC.want) {
				t.Errorf("GetPorts(_, _) = %v, want %v", got, tC.want)
			}
		})
	}
}
