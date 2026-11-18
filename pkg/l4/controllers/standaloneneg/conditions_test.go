/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package standaloneneg

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"google.golang.org/api/googleapi"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

func TestClassifyError(t *testing.T) {
	testCases := []struct {
		desc     string
		err      error
		expected lbConditionReason
	}{
		{
			desc:     "nil error",
			err:      nil,
			expected: reasonProviderError,
		},
		{
			desc:     "not found error",
			err:      &googleapi.Error{Code: http.StatusNotFound},
			expected: reasonInvalidForwardingRule,
		},
		{
			desc:     "unsupported load balancing scheme",
			err:      l4utils.NewUnsupportedLoadBalancingSchemeError("fr-name", "EXTERNAL", []string{"INTERNAL"}),
			expected: reasonUnsupportedLBType,
		},
		{
			desc:     "unsupported load balancing scheme wrapped in UserError",
			err:      l4utils.NewUserError(l4utils.NewUnsupportedLoadBalancingSchemeError("fr-name", "EXTERNAL", []string{"INTERNAL"})),
			expected: reasonUnsupportedLBType,
		},
		{
			desc:     "unsupported load balancing scheme wrapped in UserError and fmt.Errorf",
			err:      fmt.Errorf("wrapped: %w", l4utils.NewUserError(l4utils.NewUnsupportedLoadBalancingSchemeError("fr-name", "EXTERNAL", []string{"INTERNAL"}))),
			expected: reasonUnsupportedLBType,
		},
		{
			desc:     "unsupported protocol",
			err:      l4utils.NewUnsupportedProtocolError("fr-name", "UAUDP", []string{"TCP"}),
			expected: reasonInvalidForwardingRule,
		},
		{
			desc:     "unsupported protocol wrapped in UserError",
			err:      l4utils.NewUserError(l4utils.NewUnsupportedProtocolError("fr-name", "UAUDP", []string{"TCP"})),
			expected: reasonInvalidForwardingRule,
		},
		{
			desc:     "unsupported protocol wrapped in UserError and fmt.Errorf",
			err:      fmt.Errorf("wrapped: %w", l4utils.NewUserError(l4utils.NewUnsupportedProtocolError("fr-name", "UAUDP", []string{"TCP"}))),
			expected: reasonInvalidForwardingRule,
		},
		{
			desc:     "backend not attached",
			err:      l4utils.NewBackendNotAttachedError("bs1"),
			expected: reasonBackendNotAttached,
		},
		{
			desc:     "backend not attached wrapped in UserError",
			err:      l4utils.NewUserError(l4utils.NewBackendNotAttachedError("bs1")),
			expected: reasonBackendNotAttached,
		},
		{
			desc:     "backend not attached wrapped in UserError and fmt.Errorf",
			err:      fmt.Errorf("wrapped: %w", l4utils.NewUserError(l4utils.NewBackendNotAttachedError("bs1"))),
			expected: reasonBackendNotAttached,
		},
		{
			desc:     "generic error",
			err:      errors.New("generic error"),
			expected: reasonProviderError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := classifyError(tc.err)
			if got != tc.expected {
				t.Errorf("classifyError(%v) = %q, expected %q", tc.err, got, tc.expected)
			}
		})
	}
}
