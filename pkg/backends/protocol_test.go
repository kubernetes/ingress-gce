package backends_test

import (
	"testing"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/backends"
)

func TestGetProtocol(t *testing.T) {
	tcpPort := api_v1.ServicePort{
		Protocol: api_v1.ProtocolTCP,
	}
	udpPort := api_v1.ServicePort{
		Protocol: api_v1.ProtocolUDP,
	}
	emptyPort := api_v1.ServicePort{}

	testCases := []struct {
		ports []api_v1.ServicePort
		want  string
		desc  string
	}{
		{
			desc:  "no ports",
			ports: []api_v1.ServicePort{},
			want:  backends.ProtocolTCP,
		},
		{
			desc:  "udp single",
			ports: []api_v1.ServicePort{udpPort},
			want:  backends.ProtocolUDP,
		},
		{
			desc:  "udp multiple",
			ports: []api_v1.ServicePort{udpPort, udpPort},
			want:  backends.ProtocolUDP,
		},
		{
			desc:  "tcp single",
			ports: []api_v1.ServicePort{tcpPort},
			want:  backends.ProtocolTCP,
		},
		{
			desc:  "tcp multiple",
			ports: []api_v1.ServicePort{tcpPort, tcpPort},
			want:  backends.ProtocolTCP,
		},
		{
			desc:  "tcp default",
			ports: []api_v1.ServicePort{emptyPort},
			want:  backends.ProtocolTCP,
		},
		{
			desc:  "mixed, udp first",
			ports: []api_v1.ServicePort{udpPort, tcpPort},
			want:  backends.ProtocolL3,
		},
		{
			desc:  "mixed, tcp first",
			ports: []api_v1.ServicePort{tcpPort, udpPort},
			want:  backends.ProtocolL3,
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got := backends.GetProtocol(tC.ports)

			if got != tC.want {
				t.Errorf("GetProtocol(_) = %v, want %v", got, tC.want)
			}
		})
	}
}
