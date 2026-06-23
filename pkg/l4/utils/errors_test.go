package utils

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"google.golang.org/api/googleapi"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestIsNetworkMismatchGCEError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{
			err:  fmt.Errorf("The network tier of external IP is STANDARD, that of Address must be the same."),
			want: true,
		},
		{
			err:  fmt.Errorf("The network tier of external IP is PREMIUM, that of Address must be the same."),
			want: true,
		},
		{
			err:  fmt.Errorf("The network tier of external IP is , that of Address must be the same."),
			want: false,
		},
		{
			err:  fmt.Errorf("The network tier of external IP is"),
			want: false,
		},
		{
			err:  fmt.Errorf("Some dummy string"),
			want: false,
		},
	} {
		if got := IsNetworkTierMismatchGCEError(tc.err); got != tc.want {
			t.Errorf("IsNetworkTierMismatchGCEError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestIsNetworkMismatchError(t *testing.T) {
	netTierMismatchError := NewNetworkTierErr("forwarding-rule", "premium", "standard")
	for _, tc := range []struct {
		description string
		err         error
		want        bool
	}{
		{
			description: "Good error is wrapped",
			err:         fmt.Errorf("err: %w", netTierMismatchError),
			want:        true,
		},
		{
			description: "Good error is NetworkTierErr type",
			err:         netTierMismatchError,
			want:        true,
		},
		{
			description: "Wrong error is not NetworkTierErr type",
			err:         fmt.Errorf("Wrong error."),
			want:        false,
		},
	} {
		if got := IsNetworkTierError(tc.err); got != tc.want {
			t.Errorf("IsNetworkTierError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestIsConstraintViolationError(t *testing.T) {
	testCases := []struct {
		desc string
		err  error
		want bool
	}{
		{
			desc: "nil error",
			err:  nil,
			want: false,
		},
		{
			desc: "random error",
			err:  errors.New("random error"),
			want: false,
		},
		{
			desc: "other google api error",
			err: &googleapi.Error{
				Code:    http.StatusBadRequest,
				Message: "invalid arguments",
			},
			want: false,
		},
		{
			desc: "constraint violation google api error",
			err: &googleapi.Error{
				Code:    http.StatusPreconditionFailed,
				Message: "Constraint constraints/compute.restrictLoadBalancerCreationForTypes violated for projects/cf-gcpai-sales-buddy-l-t4. Forwarding Rule projects/cf-gcpai-sales-buddy-l-t4/regions/europe-west4/forwardingRules/k8s2-tcp-mv3ejptg-anthos-identity-servic-gke-oidc-envo-eswdpmuk of type INTERNAL_TCP_UDP is not allowed.",
			},
			want: true,
		},
		{
			desc: "status 412 but not constraint violation",
			err: &googleapi.Error{
				Code:    http.StatusPreconditionFailed,
				Message: "precondition failed for some other reason",
			},
			want: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			if got := IsConstraintViolationError(tC.err); got != tC.want {
				t.Errorf("IsConstraintViolationError(%v) = %v, want %v", tC.err, got, tC.want)
			}
		})
	}
}

func TestIsIPOutOfRangeError(t *testing.T) {
	testCases := []struct {
		desc string
		err  error
		want bool
	}{
		{
			desc: "nil error",
			err:  nil,
			want: false,
		},
		{
			desc: "other googleapi error",
			err:  &googleapi.Error{Code: http.StatusBadRequest, Message: "Some other error"},
			want: false,
		},
		{
			desc: "not a googleapi error",
			err:  fmt.Errorf("Requested internal IP address is outside the network/subnetwork range"),
			want: false,
		},
		{
			desc: "correct googleapi error",
			err:  &googleapi.Error{Code: http.StatusBadRequest, Message: "Invalid value for field 'resource.ipAddress': '10.24.120.53'. Requested internal IP address is outside the network/subnetwork range., invalid"},
			want: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			if got := IsIPOutOfRangeError(tc.err); got != tc.want {
				t.Errorf("IsIPOutOfRangeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestGetErrorType(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc    string
		err     error
		errType string
	}{
		{desc: "nil error", err: nil},
		{desc: "Forbidden googleapi error", err: &googleapi.Error{Code: http.StatusForbidden}, errType: http.StatusText(http.StatusForbidden)},
		{desc: "Forbidden googleapi error wrapped", err: fmt.Errorf("Got error: %w", &googleapi.Error{Code: http.StatusForbidden}), errType: http.StatusText(http.StatusForbidden)},
		{desc: "k8s notFound error", err: k8serrors.NewNotFound(schema.GroupResource{}, ""), errType: "k8s " + string(v1.StatusReasonNotFound)},
		{desc: "k8s notFound error wrapped", err: fmt.Errorf("Got error: %w", k8serrors.NewNotFound(schema.GroupResource{}, "")), errType: "k8s " + string(v1.StatusReasonNotFound)},
		{desc: "k8s notFound error embedded with %v", err: fmt.Errorf("Got error: %v", k8serrors.NewNotFound(schema.GroupResource{}, "")), errType: ""},
		{desc: "unknown error", err: fmt.Errorf("Got unknown error"), errType: ""},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if errType := GetErrorType(tc.err); errType != tc.errType {
				t.Errorf("Unexpected errType %q, want %q", errType, tc.errType)
			}
		})
	}
}
