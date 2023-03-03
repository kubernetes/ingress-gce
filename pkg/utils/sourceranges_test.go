package utils

import (
	"net"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIPv4ServiceSourceRanges(t *testing.T) {
	testCases := []struct {
		desc                     string
		specRanges               []string
		annotations              map[string]string
		expectedIPv4SourceRanges []string
		expectError              error
	}{
		{
			desc:                     "Should return allow all for no specs or annotations",
			expectedIPv4SourceRanges: []string{"0.0.0.0/0"},
		},
		{
			desc:                     "Should return allow all if only IPv6 CIDRs in Spec",
			specRanges:               []string{" 0::0/0 ", "2001:db8:3333:4444:5555:6666:7777:8888/32"},
			expectedIPv4SourceRanges: []string{"0.0.0.0/0"},
		},
		{
			desc:                     "Should get IPv4 addresses only, if mixed with IPv6",
			specRanges:               []string{"0::0/0", " 192.168.0.1/10 ", "2001:db8:3333:4444:5555:6666:7777:8888/32", " 132.8.0.1/8"},
			expectedIPv4SourceRanges: []string{"192.128.0.0/10", "132.0.0.0/8"}, // only significant bits are left, space are trimmed
		},
		{
			desc: "Should get IPv4 addresses from annotation, if mixed with IPv6 and no spec ranges",
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: "0::0/0, 192.168.0.1/10 ,2001:db8:3333:4444:5555:6666:7777:8888/32, 132.8.0.1/8 ",
			},
			expectedIPv4SourceRanges: []string{"192.128.0.0/10", "132.0.0.0/8"}, // only significant bits are left, spaces are trimmed
		},
		{
			desc:       "Should ignore annotation if spec is present",
			specRanges: []string{"0::0/0", " 192.168.0.1/10 ", "2001:db8:3333:4444:5555:6666:7777:8888/32 ", " 132.8.0.1/8"},
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: "1.2.3 1.2.3", // should not return error, even if annotation is invalid
			},
			expectedIPv4SourceRanges: []string{"192.128.0.0/10", "132.0.0.0/8"}, // only significant bits are left, spaces are trimmed
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

			ipv4Ranges, err := IPv4ServiceSourceRanges(svc)
			errDiff := cmp.Diff(tc.expectError, err)
			if errDiff != "" {
				t.Errorf("Expected error %v, got %v, diff: %v", tc.expectError, err, errDiff)
			}

			sort.Strings(tc.expectedIPv4SourceRanges)
			sort.Strings(ipv4Ranges)
			diff := cmp.Diff(tc.expectedIPv4SourceRanges, ipv4Ranges)
			if diff != "" {
				t.Errorf("Expected IPv4 ranges: %v, got ranges %v, diff: %s", tc.expectedIPv4SourceRanges, ipv4Ranges, diff)
			}
		})
	}
}

func TestIPv6ServiceSourceRanges(t *testing.T) {
	testCases := []struct {
		desc                     string
		specRanges               []string
		annotations              map[string]string
		expectedIPv6SourceRanges []string
		expectError              error
	}{
		{
			desc:                     "Should return allow all for no specs or annotations",
			expectedIPv6SourceRanges: []string{"0::0/0"},
		},
		{
			desc:                     "Should return allow all if only IPv4 CIDRs in Spec",
			specRanges:               []string{" 1.2.3.4/5 ", " 192.168.0.1/10 "},
			expectedIPv6SourceRanges: []string{"0::0/0"},
		},
		{
			desc:                     "Should get IPv6 addresses only, if mixed with IPv4",
			specRanges:               []string{" 2001:db8:4444:4444:5555:6666:7777:8888/10 ", " 192.168.0.1/10 ", "2001:db8:3333:4444:5555:6666:7777:8888/32", " 132.8.0.1/8"},
			expectedIPv6SourceRanges: []string{"2000::/10", "2001:db8::/32"}, // only significant bits are left, space are trimmed
		},
		{
			desc: "Should get IPv6 addresses from annotation, if mixed with IPv4 and no spec ranges",
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: " 2001:db8:4444:4444:5555:6666:7777:8888/10 , 192.168.0.1/10 ,2001:db8:3333:4444:5555:6666:7777:8888/32, 132.8.0.1/8 ",
			},
			expectedIPv6SourceRanges: []string{"2000::/10", "2001:db8::/32"}, // only significant bits are left, space are trimmed
		},
		{
			desc:       "Should ignore annotation if spec is present",
			specRanges: []string{" 2001:db8:4444:4444:5555:6666:7777:8888/10 ", " 192.168.0.1/10 ", "2001:db8:3333:4444:5555:6666:7777:8888/32 ", " 132.8.0.1/8"},
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: "2001:db8:3333:4444:5555:6666:7777:8888 1.2.3", // should not return error, even if annotation is invalid
			},
			expectedIPv6SourceRanges: []string{"2000::/10", "2001:db8::/32"}, // only significant bits are left, space are trimmed
		},
		{
			desc:       "Should return special error for invalid spec value",
			specRanges: []string{"2001:db8:4444:4444:5555:6666:7777:8888"}, // no mask
			expectError: &InvalidLoadBalancerSourceRangesSpecError{
				LoadBalancerSourceRangesSpec: []string{"2001:db8:4444:4444:5555:6666:7777:8888"},
				ParseErr:                     &net.ParseError{Type: "CIDR address", Text: "2001:db8:4444:4444:5555:6666:7777:8888"},
			},
		},
		{
			desc: "Should return special error for invalid annotation value",
			annotations: map[string]string{
				v1.AnnotationLoadBalancerSourceRangesKey: "2001:db8:4444:4444:5555:6666:7777:8888/14 2001:db8:4444:4444:5555:6666:7777:8888/15", // should be comma-separated
			},
			expectError: &InvalidLoadBalancerSourceRangesAnnotationError{
				LoadBalancerSourceRangesAnnotation: "2001:db8:4444:4444:5555:6666:7777:8888/14 2001:db8:4444:4444:5555:6666:7777:8888/15",
				ParseErr:                           &net.ParseError{Type: "CIDR address", Text: "2001:db8:4444:4444:5555:6666:7777:8888/14 2001:db8:4444:4444:5555:6666:7777:8888/15"},
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

			ipv6Ranges, err := IPv6ServiceSourceRanges(svc)
			errDiff := cmp.Diff(tc.expectError, err)
			if errDiff != "" {
				t.Errorf("Expected error %v, got %v, diff: %v", tc.expectError, err, errDiff)
			}

			sort.Strings(tc.expectedIPv6SourceRanges)
			sort.Strings(ipv6Ranges)
			diff := cmp.Diff(tc.expectedIPv6SourceRanges, ipv6Ranges)
			if diff != "" {
				t.Errorf("Expected IPv6 ranges: %v, got ranges %v, diff: %s", tc.expectedIPv6SourceRanges, ipv6Ranges, diff)
			}
		})
	}
}
