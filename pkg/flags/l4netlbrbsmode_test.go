/*
Copyright 2021 The Kubernetes Authors.

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

package flags

import "testing"

func TestRbsModesSetup(t *testing.T) {
	t.Parallel()
	for _, p := range []struct {
		expectedRbs RbsMode
		expectedStr string
	}{
		{DISABLED, ""},
		{ENABLED, "enabled"},
		{OPTIN, "opt-in"},
		{ENFORCED, "enforced"},
	} {
		rbs := DISABLED
		if err := rbs.Set(p.expectedStr); err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if rbs != p.expectedRbs {
			t.Errorf("Error RbsMode mismatch: %v != %v", p.expectedRbs, rbs)
		}
		if rbs.String() != p.expectedStr {
			t.Errorf("Error Rbs string mismatch: %s != %s", p.expectedStr, rbs.String())
		}
	}
}

func TestRbsModesSetupFromUndefinedString(t *testing.T) {
	undefinedRbsMode := "not-rbs-mode"
	rbs := ENFORCED
	if err := rbs.Set(undefinedRbsMode); err == nil {
		t.Errorf("Set should return error")
	}
}
