package forwardingrules_test

import (
	"testing"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/l4/forwardingrules"
)

func TestNeeds(t *testing.T) {
	testCases := []struct {
		desc      string
		svcPorts  []api_v1.ServicePort
		wantTCP   bool
		wantUDP   bool
		wantMixed bool
	}{
		{
			desc:     "No ports",
			svcPorts: []api_v1.ServicePort{},
		},
		{
			desc: "TCP port",
			svcPorts: []api_v1.ServicePort{
				{Protocol: api_v1.ProtocolTCP},
			},
			wantTCP: true,
		},
		{
			desc: "TCP default port",
			svcPorts: []api_v1.ServicePort{
				{},
			},
			wantTCP: true,
		},
		{
			desc: "UDP port",
			svcPorts: []api_v1.ServicePort{
				{Protocol: api_v1.ProtocolUDP},
			},
			wantUDP: true,
		},
		{
			desc: "TCP and UDP ports",
			svcPorts: []api_v1.ServicePort{
				{Protocol: api_v1.ProtocolTCP},
				{Protocol: api_v1.ProtocolUDP},
			},
			wantTCP:   true,
			wantUDP:   true,
			wantMixed: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			gotTCP := forwardingrules.NeedsTCP(tC.svcPorts)
			if gotTCP != tC.wantTCP {
				t.Errorf("NeedsTCP(%v) = %v, want %v", tC.svcPorts, gotTCP, tC.wantTCP)
			}

			gotUDP := forwardingrules.NeedsUDP(tC.svcPorts)
			if gotUDP != tC.wantUDP {
				t.Errorf("NeedsUDP(%v) = %v, want %v", tC.svcPorts, gotUDP, tC.wantUDP)
			}

			gotMixed := forwardingrules.NeedsMixed(tC.svcPorts)
			if gotMixed != tC.wantMixed {
				t.Errorf("NeedsMixed(%v) = %v, want %v", tC.svcPorts, gotMixed, tC.wantMixed)
			}
		})
	}
}
