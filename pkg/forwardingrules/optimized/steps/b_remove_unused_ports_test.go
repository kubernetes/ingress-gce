package steps_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules/optimized/steps"
)

func TestRemoveUnusedDiscretePorts(t *testing.T) {
	testCases := []struct {
		desc  string
		ports []api_v1.ServicePort
		frs   map[steps.ResourceName]*composite.ForwardingRule
		want  map[steps.ResourceName]*composite.ForwardingRule
	}{
		{
			desc:  "no rules",
			ports: []api_v1.ServicePort{{Port: 80}, {Port: 443}},
			frs:   map[steps.ResourceName]*composite.ForwardingRule{},
			want:  map[steps.ResourceName]*composite.ForwardingRule{},
		},
		{
			desc:  "all used, single rule",
			ports: []api_v1.ServicePort{{Port: 80}, {Port: 443}},
			frs: map[steps.ResourceName]*composite.ForwardingRule{
				"rule-1": {Ports: []string{"80", "443"}},
			},
			want: map[steps.ResourceName]*composite.ForwardingRule{
				"rule-1": {Ports: []string{"80", "443"}},
			},
		},
		{
			desc: "all used, multiprotocol and explicit",
			ports: []api_v1.ServicePort{
				{Port: 53, Protocol: api_v1.ProtocolTCP},
				{Port: 53, Protocol: api_v1.ProtocolUDP},
				{Port: 8080, Protocol: api_v1.ProtocolTCP},
			},
			frs: map[steps.ResourceName]*composite.ForwardingRule{
				"tcp": {Ports: []string{"8080", "53"}},
				"udp": {Ports: []string{"53"}},
			},
			want: map[steps.ResourceName]*composite.ForwardingRule{
				"tcp": {Ports: []string{"8080", "53"}},
				"udp": {Ports: []string{"53"}},
			},
		},
		{
			desc: "clear 8080, multiprotocol and explicit",
			ports: []api_v1.ServicePort{
				{Port: 53, Protocol: api_v1.ProtocolTCP},
				{Port: 53, Protocol: api_v1.ProtocolUDP},
			},
			frs: map[steps.ResourceName]*composite.ForwardingRule{
				"tcp": {Ports: []string{"8080", "53"}},
				"udp": {Ports: []string{"53"}},
			},
			want: map[steps.ResourceName]*composite.ForwardingRule{
				"tcp": {Ports: []string{"53"}},
				"udp": {Ports: []string{"53"}},
			},
		},
		{
			desc: "leave two frs",
			ports: []api_v1.ServicePort{
				{Port: 11},
				{Port: 21},
			},
			frs: map[steps.ResourceName]*composite.ForwardingRule{
				"rule-1": {Ports: []string{"11", "12", "13", "14", "15"}},
				"rule-2": {Ports: []string{"21", "22", "23", "24", "25"}},
				"rule-3": {Ports: []string{"31", "32", "33", "34", "35"}},
			},
			want: map[steps.ResourceName]*composite.ForwardingRule{
				"rule-1": {Ports: []string{"11"}},
				"rule-2": {Ports: []string{"21"}},
				"rule-3": {Ports: []string{}},
			},
		},
	}
	for _, tC := range testCases {
		tC := tC
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()

			// Act
			if err := steps.RemoveUnusedDiscretePorts(tC.ports, tC.frs); err != nil {
				t.Fatalf("RemoveUnusedDiscretePorts(_) returned error: %v, want nil", err)
			}

			// Assert
			if diff := cmp.Diff(tC.want, tC.frs); diff != "" {
				t.Errorf("want != got, (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestRemoveUnusedDiscretePortsWithInvalidPort(t *testing.T) {
	// Arrange
	// this shouldn't be possible - GCE API shouldn't return invalid ports.
	frs := map[steps.ResourceName]*composite.ForwardingRule{
		"rule-1": {Ports: []string{"invalid"}},
	}

	// Act
	err := steps.RemoveUnusedDiscretePorts([]api_v1.ServicePort{{Port: 80}}, frs)

	// Assert
	if err == nil {
		t.Fatalf("RemoveUnusedDiscretePorts(_) returned nil, want error")
	}
}
