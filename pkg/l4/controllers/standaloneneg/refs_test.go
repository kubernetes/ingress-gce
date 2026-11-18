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
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"k8s.io/klog/v2"
)

func TestParseFRRefs(t *testing.T) {
	region := "us-central1"
	regionalURL := "https://www.googleapis.com/compute/v1/projects/p/regions/us-central1/forwardingRules/fr1"
	globalURL := "https://www.googleapis.com/compute/v1/projects/p/global/forwardingRules/fr2"

	testCases := []struct {
		desc       string
		annotation string
		wantRefs   []frRef
		wantErrs   int
	}{
		{
			desc:       "empty annotation",
			annotation: "",
		},
		{
			desc:       "only separators",
			annotation: " , ,",
		},
		{
			desc:       "names and URLs",
			annotation: "fr1, " + globalURL,
			wantRefs: []frRef{
				{key: meta.RegionalKey("fr1", region), raw: "fr1"},
				{key: meta.GlobalKey("fr2"), raw: globalURL},
			},
		},
		{
			desc:       "duplicate references are skipped",
			annotation: "fr1," + regionalURL + ",fr1",
			wantRefs: []frRef{
				{key: meta.RegionalKey("fr1", region), raw: "fr1"},
			},
		},
		{
			desc:       "invalid URL",
			annotation: "invalid/url,fr1",
			wantRefs: []frRef{
				{key: meta.RegionalKey("fr1", region), raw: "fr1"},
			},
			wantErrs: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			refs, errs := parseFRRefs(tc.annotation, region, klog.TODO())
			if diff := cmp.Diff(tc.wantRefs, refs, cmp.AllowUnexported(frRef{})); diff != "" {
				t.Errorf("parseFRRefs() refs mismatch (-want +got):\n%s", diff)
			}
			if len(errs) != tc.wantErrs {
				t.Errorf("parseFRRefs() returned %d errors (%v), want %d", len(errs), errs, tc.wantErrs)
			}
		})
	}
}

func TestSortFRRefs(t *testing.T) {
	refs := []frRef{{raw: "fr-b"}, {raw: "fr-c"}, {raw: "fr-a"}}
	sortFRRefs(refs)
	if diff := cmp.Diff([]string{"fr-a", "fr-b", "fr-c"}, frRefNames(refs)); diff != "" {
		t.Errorf("sortFRRefs() mismatch (-want +got):\n%s", diff)
	}
}
