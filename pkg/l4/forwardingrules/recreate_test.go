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

package forwardingrules

import (
	"errors"
	"strings"
	"testing"

	"k8s.io/ingress-gce/pkg/composite"
)

type fakeNetwork struct {
	legacy bool
}

func (n fakeNetwork) IsLegacyNetwork() bool { return n.legacy }

func TestIsRecreateAllowed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc       string
		ipReserved bool
		legacy     bool
		want       bool
	}{
		{desc: "reserved IP", ipReserved: true, want: true},
		{desc: "reserved IP in legacy network", ipReserved: true, legacy: true, want: true},
		{desc: "unreserved IP in legacy network", ipReserved: false, legacy: true, want: true},
		{desc: "unreserved IP", ipReserved: false, want: false},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			if got := IsRecreateAllowed(fakeNetwork{legacy: tc.legacy}, tc.ipReserved); got != tc.want {
				t.Errorf("IsRecreateAllowed(legacy=%v, ipReserved=%v) = %v, want %v", tc.legacy, tc.ipReserved, got, tc.want)
			}
		})
	}
}

func TestNewIPNotReservedError(t *testing.T) {
	t.Parallel()
	rule := &composite.ForwardingRule{Name: "fr-name", IPAddress: "1.2.3.4"}

	err := NewIPNotReservedError(rule)
	if !errors.Is(err, ErrIPNotReserved) {
		t.Errorf("NewIPNotReservedError() = %v, want an error wrapping ErrIPNotReserved", err)
	}
	for _, want := range []string{rule.Name, rule.IPAddress} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("NewIPNotReservedError() = %q, want it to mention %q", err, want)
		}
	}
}
