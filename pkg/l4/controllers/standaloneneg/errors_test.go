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
	"testing"

	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

func TestJoinMaybeUserErrors(t *testing.T) {
	userErr1 := l4utils.NewUserError(errors.New("user err 1"))
	userErr2 := l4utils.NewUserError(errors.New("user err 2"))
	sysErr := errors.New("system err")

	testCases := []struct {
		desc       string
		errs       []error
		expectNil  bool
		expectUser bool
	}{
		{
			desc:      "all nil errors",
			errs:      []error{nil, nil},
			expectNil: true,
		},
		{
			desc:       "nil mixed with user errors",
			errs:       []error{nil, userErr1, nil, userErr2},
			expectUser: true,
		},
		{
			desc:       "only user errors",
			errs:       []error{userErr1, userErr2},
			expectUser: true,
		},
		{
			desc:       "nil mixed with system error",
			errs:       []error{nil, sysErr},
			expectUser: false,
		},
		{
			desc:       "user error mixed with system error and nil",
			errs:       []error{nil, userErr1, sysErr},
			expectUser: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := joinMaybeUserErrors(tc.errs...)
			if tc.expectNil {
				if got != nil {
					t.Fatalf("expected nil error, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil error, got nil")
			}
			var userErr *l4utils.UserError
			isUser := errors.As(got, &userErr) && got == userErr
			if isUser != tc.expectUser {
				t.Errorf("isUserErrorWrapper(%v) = %v, expected %v", got, isUser, tc.expectUser)
			}
		})
	}
}
