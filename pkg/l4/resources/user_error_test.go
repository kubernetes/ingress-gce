package resources_test

import (
	"fmt"
	"testing"

	"k8s.io/ingress-gce/pkg/l4/resources"

	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestIsUserError(t *testing.T) {
	testCases := []struct {
		err  error
		want bool
	}{
		{
			err:  nil,
			want: false,
		},
		{
			err:  fmt.Errorf("other err"),
			want: false,
		},
		{
			err:  utils.NewNetworkTierErr("forwardingRule", "STANDARD", "PREMIUM"),
			want: true,
		},
		{
			err:  utils.NewUnsupportedNetworkTierErr("forwardingRule", "STANDARD"),
			want: true,
		},
		{
			err:  utils.NewUserError(fmt.Errorf("err")),
			want: true,
		},
		{
			err:  &firewalls.FirewallXPNError{Internal: fmt.Errorf("internal err"), Message: "message"},
			want: true,
		},
		{
			err:  utils.NewIPConfigurationError("123.123.123.456", "bad ip"),
			want: true,
		},
		{
			err:  utils.NewInvalidLoadBalancerSourceRangesAnnotationError("annotation", fmt.Errorf("bad")),
			want: true,
		},
		{
			err:  utils.NewInvalidLoadBalancerSourceRangesSpecError(nil, fmt.Errorf("bad")),
			want: true,
		},
		{
			err:  utils.NewConflictingPortsConfigurationError("8080-8090", "conflicting ports"),
			want: true,
		},
	}
	for _, tC := range testCases {
		tC := tC
		testName := "nil"
		if tC.err != nil {
			testName = tC.err.Error()
		}

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			if got := resources.IsUserError(tC.err); got != tC.want {
				t.Errorf("IsUserError(%v) = %v, want %v", tC.err.Error(), got, tC.want)
			}
		})
	}
}
