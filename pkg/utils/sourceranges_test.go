package utils

import (
	"net"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestServiceSourceRanges(t *testing.T) {
	testCases := []struct {
		desc                 string
		specRanges           []string
		annotations          map[string]string
		expectedSourceRanges []string
		expectError          error
	}{
		{
			desc:                 "Should return allow all for no specs or annotations",
			expectedSourceRanges: []string{"0.0.0.0/0"},
		},
		{
			desc:                 "Should parse ranges from spec",
			specRanges:           []string{"192.168.0.1/10", "132.8.0.1/8"},
			expectedSourceRanges: []string{"192.128.0.0/10", "132.0.0.0/8"}, // only significant bits are left
		},
		{
			desc: "Should parse ranges from annotations, if no spec value",
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: "192.168.0.1/10,132.8.0.1/8",
			},
			expectedSourceRanges: []string{"192.128.0.0/10", "132.0.0.0/8"}, // only significant bits are left
		},
		{
			desc:       "Should ignore annotation if spec is present",
			specRanges: []string{"192.168.0.1/10", "132.8.0.1/8"},
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: "1.2.3 1.2.3", // should not return error, even if annotation is invalid
			},
			expectedSourceRanges: []string{"192.128.0.0/10", "132.0.0.0/8"}, // only significant bits are left
		},
		{
			desc:       "Should return special error for invalid spec value",
			specRanges: []string{"1.0.1.2"}, // wrong CIDR, because no mask
			expectError: &InvalidLoadBalancerSourceRangesSpecError{
				LoadBalancerSourceRangesSpec: []string{"1.0.1.2"},
				ParseErr:                     &net.ParseError{Type: "CIDR address", Text: "1.0.1.2"},
			},
		},
		{
			desc: "Should return special error for invalid annotation value",
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: "1.2.3.4 1.2.3.4", // should be comma-separated
			},
			expectError: &InvalidLoadBalancerSourceRangesAnnotationError{
				LoadBalancerSourceRangesAnnotation: "1.2.3.4 1.2.3.4",
				ParseErr:                           &net.ParseError{Type: "CIDR address", Text: "1.2.3.4 1.2.3.4"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			svc := &v1.Service{
				Spec: v1.ServiceSpec{
					LoadBalancerSourceRanges: tc.specRanges,
				},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tc.annotations,
				},
			}

			sourceRanges, err := ServiceSourceRanges(svc)
			errDiff := cmp.Diff(tc.expectError, err)
			if errDiff != "" {
				t.Errorf("Expected error %v, got %v, diff: %v", tc.expectError, err, errDiff)
			}

			sort.Strings(tc.expectedSourceRanges)
			sort.Strings(sourceRanges)
			diff := cmp.Diff(tc.expectedSourceRanges, sourceRanges)
			if diff != "" {
				t.Errorf("Expected source ranges: %v, got ranges %v, diff: %s", tc.expectedSourceRanges, sourceRanges, diff)
			}
		})
	}
}
