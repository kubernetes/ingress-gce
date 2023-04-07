/*
Copyright 2020 The Kubernetes Authors.

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

package types

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassifyError(t *testing.T) {
	nonNegSyncError := errors.New("not a NegSyncError")
	nonNegSyncErrorWrapped := fmt.Errorf("%w: %v", errors.New("additional context"), nonNegSyncError)

	testCases := []struct {
		desc          string
		err           error
		expectSyncErr NegSyncError
	}{
		{
			desc: "not an NegSyncError",
			err:  nonNegSyncError,
			expectSyncErr: NegSyncError{
				Err:          nonNegSyncError,
				Reason:       ReasonOtherError,
				IsErrorState: false,
			},
		},
		{
			desc:          "is an NegSyncError",
			err:           ErrEPNodeMissing,
			expectSyncErr: ErrEPNodeMissing,
		},
		{
			desc:          "an error that wraps a NegSyncError",
			err:           fmt.Errorf("%w: %v", ErrEPNodeMissing, errors.New("additional context")),
			expectSyncErr: ErrEPNodeMissing,
		},
		{
			desc: "an error that wraps a non NegSyncError",
			err:  nonNegSyncErrorWrapped,
			expectSyncErr: NegSyncError{
				Err:          nonNegSyncErrorWrapped,
				Reason:       ReasonOtherError,
				IsErrorState: false,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			syncErr := ClassifyError(tc.err)
			if !errors.Is(syncErr, tc.expectSyncErr) {
				t.Errorf("ClassifyError(%v)=%v, want %v", tc.err, syncErr, tc.expectSyncErr)
			}
		})
	}
}
