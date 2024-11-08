package backends_test

import (
	"testing"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/composite"
)

func TestGetProtocol(t *testing.T) {
	tcpPort := api_v1.ServicePort{
		Name:     "TCP Port",
		Protocol: api_v1.ProtocolTCP,
	}
	udpPort := api_v1.ServicePort{
		Name:     "UDP Port",
		Protocol: api_v1.ProtocolUDP,
	}
	l3BackendService := &composite.BackendService{
		Protocol: backends.L3Protocol,
	}
	tcpBackendService := &composite.BackendService{
		Protocol: "TCP",
	}
	udpBackendService := &composite.BackendService{
		Protocol: "UDP",
	}

	testCases := []struct {
		ports            []api_v1.ServicePort
		bs               *composite.BackendService
		expectedProtocol string
		desc             string
	}{
		{
			ports:            []api_v1.ServicePort{},
			expectedProtocol: backends.L3Protocol,
			desc:             "No Backend, no ports",
		},
		{
			ports:            nil,
			expectedProtocol: backends.L3Protocol,
			desc:             "No Backend, no ports",
		},
		{
			ports:            nil,
			bs:               tcpBackendService,
			expectedProtocol: string(api_v1.ProtocolTCP),
			desc:             "TCP Backend, no ports",
		},
		{
			ports:            nil,
			bs:               udpBackendService,
			expectedProtocol: string(api_v1.ProtocolUDP),
			desc:             "UDP Backend, no ports",
		},
		{
			ports:            nil,
			bs:               l3BackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "L3 Backend, no ports",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
			},
			expectedProtocol: backends.L3Protocol,
			desc:             "No Backend, UDP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
			},
			expectedProtocol: backends.L3Protocol,
			desc:             "No Backend, TCP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
				tcpPort,
			},
			expectedProtocol: backends.L3Protocol,
			desc:             "No Backend, Mixed protocols, first UDP",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
				udpPort,
			},
			expectedProtocol: backends.L3Protocol,
			desc:             "No Backend, Mixed protocols, first TCP",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
			},
			bs:               udpBackendService,
			expectedProtocol: string(api_v1.ProtocolUDP),
			desc:             "UDP Backend, UDP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
			},
			bs:               udpBackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "UDP Backend, TCP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
				tcpPort,
			},
			bs:               udpBackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "UDP Backend, Mixed protocols, first UDP",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
				udpPort,
			},
			bs:               udpBackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "UDP Backend, Mixed protocols, first TCP",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
			},
			bs:               tcpBackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "TCP Backend, UDP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
			},
			bs:               tcpBackendService,
			expectedProtocol: string(api_v1.ProtocolTCP),
			desc:             "TCP Backend, TCP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
				tcpPort,
			},
			bs:               tcpBackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "TCP Backend, Mixed protocols, first UDP",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
				udpPort,
			},
			bs:               tcpBackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "TCP Backend, Mixed protocols, first TCP",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
			},
			bs:               l3BackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "L3 Backend, UDP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
			},
			bs:               l3BackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "L3 Backend, TCP protocol only",
		},
		{
			ports: []api_v1.ServicePort{
				udpPort,
				tcpPort,
			},
			bs:               l3BackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "L3 Backend, Mixed protocols, first UDP",
		},
		{
			ports: []api_v1.ServicePort{
				tcpPort,
				udpPort,
			},
			bs:               l3BackendService,
			expectedProtocol: backends.L3Protocol,
			desc:             "L3 Backend, Mixed protocols, first TCP",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			protocol := backends.GetProtocol(tc.ports, tc.bs)

			if protocol != tc.expectedProtocol {
				t.Errorf("GetProtocol = %v, want %v", protocol, tc.expectedProtocol)
			}
		})
	}
}
