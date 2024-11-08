package forwardingrules_test

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/forwardingrules"
)

func TestNeedsTCP(t *testing.T) {
	testCases := []struct {
		desc     string
		svcPorts []v1.ServicePort
		want     bool
	}{
		{
			desc:     "No ports",
			svcPorts: []v1.ServicePort{},
			want:     false,
		},
		{
			desc: "TCP port",
			svcPorts: []v1.ServicePort{
				{Protocol: v1.ProtocolTCP},
			},
			want: true,
		},
		{
			desc: "UDP port",
			svcPorts: []v1.ServicePort{
				{Protocol: v1.ProtocolUDP},
			},
			want: false,
		},
		{
			desc: "TCP and UDP ports",
			svcPorts: []v1.ServicePort{
				{Protocol: v1.ProtocolTCP},
				{Protocol: v1.ProtocolUDP},
			},
			want: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := forwardingrules.NeedsTCP(tc.svcPorts)
			if got != tc.want {
				t.Errorf("NeedsTCP(%v) = %v, want %v", tc.svcPorts, got, tc.want)
			}
		})
	}
}

func TestNeedsUDP(t *testing.T) {
	testCases := []struct {
		desc     string
		svcPorts []v1.ServicePort
		want     bool
	}{
		{
			desc:     "No ports",
			svcPorts: []v1.ServicePort{},
			want:     false,
		},
		{
			desc: "TCP port",
			svcPorts: []v1.ServicePort{
				{Protocol: v1.ProtocolTCP},
			},
			want: false,
		},
		{
			desc: "UDP port",
			svcPorts: []v1.ServicePort{
				{Protocol: v1.ProtocolUDP},
			},
			want: true,
		},
		{
			desc: "TCP and UDP ports",
			svcPorts: []v1.ServicePort{
				{Protocol: v1.ProtocolTCP},
				{Protocol: v1.ProtocolUDP},
			},
			want: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := forwardingrules.NeedsUDP(tc.svcPorts)
			if got != tc.want {
				t.Errorf("NeedsUDP(%v) = %v, want %v", tc.svcPorts, got, tc.want)
			}
		})
	}
}
