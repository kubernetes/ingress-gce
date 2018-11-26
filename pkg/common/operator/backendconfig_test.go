package operator

import (
	"testing"

	"k8s.io/ingress-gce/pkg/backendconfig"

	api_v1 "k8s.io/api/core/v1"
)

func TestDoesServiceReferenceBackendConfig(t *testing.T) {
	testCases := []struct {
		desc     string
		service  *api_v1.Service
		expected bool
	}{
		{
			desc:     "service with no backend config",
			service:  backendconfig.SvcWithoutConfig,
			expected: false,
		},
		{
			desc:     "service with test backend config",
			service:  backendconfig.SvcWithTestConfig,
			expected: true,
		},
		{
			desc:     "service with default test backend config",
			service:  backendconfig.SvcWithDefaultTestConfig,
			expected: true,
		},
		{
			desc:     "service with test backend config in a different namespace",
			service:  backendconfig.SvcWithTestConfigOtherNamespace,
			expected: false,
		},
		{
			desc:     "service with a different backend config",
			service:  backendconfig.SvcWithOtherConfig,
			expected: false,
		},
		{
			desc:     "service with a different default backend config",
			service:  backendconfig.SvcWithDefaultOtherConfig,
			expected: false,
		},
		{
			desc:     "service with invalid backend config",
			service:  backendconfig.SvcWithInvalidConfig,
			expected: false,
		},
	}

	for _, tc := range testCases {
		result := doesServiceReferenceBackendConfig(tc.service, backendconfig.TestBackendConfig)
		if result != tc.expected {
			t.Fatalf("SDSD)")
		}
	}
}
