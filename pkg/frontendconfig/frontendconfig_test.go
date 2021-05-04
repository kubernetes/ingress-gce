package frontendconfig

import (
	"testing"

	v1 "k8s.io/api/networking/v1"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/test"
)

func TestFrontendConfigForIngress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc     string
		ing      *v1.Ingress
		expected *frontendconfigv1beta1.FrontendConfig
		err      error
	}{
		{
			desc:     "ingress with no frontend config annotation",
			ing:      test.IngressWithoutFrontendConfig,
			expected: nil,
			err:      nil,
		},
		{
			desc:     "frontend config missing",
			ing:      test.IngressWithOtherFrontendConfig,
			expected: nil,
			err:      ErrFrontendConfigDoesNotExist,
		},
		{
			desc:     "ingress with frontend config that exists",
			ing:      test.IngressWithFrontendConfig,
			expected: test.FrontendConfig,
			err:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result, err := FrontendConfigForIngress([]*frontendconfigv1beta1.FrontendConfig{test.FrontendConfig}, tc.ing)
			if result != tc.expected {
				t.Fatalf("Expected result to be %v, got %v", tc.expected, result)
			}
			if err != tc.err {
				t.Fatalf("Expected err to be %v, got %v", tc.err, err)
			}
		})
	}
}
