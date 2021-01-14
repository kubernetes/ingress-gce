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

package namer

import (
	"strings"
	"testing"
)

func TestNamerServiceAttachment(t *testing.T) {
	longstring := "01234567890123456789012345678901234567890123456789"
	svcAttachmentUID := "service-attachment-uid"
	prefix := "prefix"
	testCases := []struct {
		desc                string
		namespace           string
		name                string
		expectDefaultPrefix string
		expectCustomPrefix  string
	}{
		{
			"simple case",
			"namespace",
			"name",
			"k8s1-sa-7kpbhpki-namespace-name-0md8wvdl",
			"prefix1-sa-7kpbhpki-namespace-name-0md8wvdl",
		},
		{
			"63 characters with default prefix k8s",
			longstring[:18],
			longstring[:18],
			"k8s1-sa-7kpbhpki-012345678901234567-012345678901234567-ylkgnp40",
			"prefix1-sa-7kpbhpki-01234567890123456-0123456789012345-ylkgnp40",
		},
		{
			"long namespace",
			longstring,
			"name",
			"k8s1-sa-7kpbhpki-0123456789012345678901234567890123-na-t9r9x2i3",
			"prefix1-sa-7kpbhpki-0123456789012345678901234567890-na-t9r9x2i3",
		},
		{
			"long name and namespace",
			longstring,
			longstring,
			"k8s1-sa-7kpbhpki-012345678901234567-012345678901234567-m7pgimzt",
			"prefix1-sa-7kpbhpki-01234567890123456-0123456789012345-m7pgimzt",
		},
		{
			"long name",
			"namespace",
			longstring,
			"k8s1-sa-7kpbhpki-namesp-012345678901234567890123456789-68oiwx7n",
			"prefix1-sa-7kpbhpki-namesp-012345678901234567890123456-68oiwx7n",
		},
	}

	for _, tc := range testCases {
		for _, withPrefix := range []bool{true, false} {
			var oldNamer *Namer
			var expectedName string

			if withPrefix {
				oldNamer = NewNamer(clusterId, "")
				expectedName = tc.expectDefaultPrefix
			} else {
				oldNamer = NewNamerWithPrefix(prefix, clusterId, "")
				expectedName = tc.expectCustomPrefix
			}

			newNamer := NewServiceAttachmentNamer(oldNamer, kubeSystemUID)
			res := newNamer.ServiceAttachment(tc.namespace, tc.name, svcAttachmentUID)
			if len(res) > 63 {
				t.Errorf("%s: got len(res) == %v, want <= 63", tc.desc, len(res))
			}
			if numHyphens := strings.Count(res, "-"); numHyphens != 5 {
				t.Errorf("Expected to have 5 components to name delimited by `-`. Found only %d `-`", numHyphens)
			}
			if res != expectedName {
				t.Errorf("%s: got %q, want %q", tc.desc, res, expectedName)
			}
		}
	}
}
