package forwardingrules_test

import (
	"fmt"
	"testing"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
)

func TestGetPorts(t *testing.T) {
	mixedPorts := []api_v1.ServicePort{
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

	defaultProtocolPort := []api_v1.ServicePort{
		{
			Name: "default2tcp",
			Port: 1001,
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
			servicePorts: mixedPorts,
			protocol:     api_v1.ProtocolUDP,
			want:         []string{"1001"},
		},
		{
			desc:         "tcp",
			servicePorts: mixedPorts,
			protocol:     api_v1.ProtocolTCP,
			want:         []string{"1001", "8080"},
		},
		{
			desc:         "tcp default",
			servicePorts: defaultProtocolPort,
			protocol:     api_v1.ProtocolTCP,
			want:         []string{"1001"},
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := forwardingrules.GetPorts(tC.servicePorts, tC.protocol)
			if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tC.want) {
				t.Errorf("GetPorts(_, _) = %v, want %v", got, tC.want)
			}
		})
	}
}
