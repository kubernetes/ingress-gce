/*
Copyright 2017 The Kubernetes Authors.

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

package utils

import (
	"testing"
)

const (
	clusterId = "0123456789abcdef"
)

func TestNamerNEG(t *testing.T) {
	longstring := "01234567890123456789012345678901234567890123456789"
	testCases := []struct {
		desc      string
		namespace string
		name      string
		port      string
		expect    string
	}{
		{
			"simple case",
			"namespace",
			"name",
			"80",
			"k8s1-0123456789abcdef-namespace-name-80-1e047e33",
		},
		{
			"63 characters",
			longstring[:10],
			longstring[:10],
			longstring[:10],
			"k8s1-0123456789abcdef-0123456789-0123456789-0123456789-4f7223eb",
		},
		{
			"long namespace",
			longstring,
			"0",
			"0",
			"k8s1-0123456789abcdef-01234567890123456789012345678-0--44255b67",
		},

		{
			"long name and namespace",
			longstring,
			longstring,
			"0",
			"k8s1-0123456789abcdef-012345678901234-012345678901234--525cce3d",
		},
		{
			" long name, namespace and port",
			longstring,
			longstring[:40],
			longstring[:30],
			"k8s1-0123456789abcdef-0123456789012-0123456789-0123456-71877a60",
		},
	}

	namer := NewNamer(clusterId, "")
	for _, tc := range testCases {
		res := namer.NEG(tc.namespace, tc.name, tc.port)
		if len(res) > 63 {
			t.Errorf("%s: got len(res) == %v, want <= 63", tc.desc, len(res))
		}
		if res != tc.expect {
			t.Errorf("%s: got %q, want %q", tc.desc, res, tc.expect)
		}
	}
}
