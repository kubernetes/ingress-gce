/*
Copyright 2023 The Kubernetes Authors.

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

package labels

import (
	"errors"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

func TestGetPodLabelMap(t *testing.T) {
	t.Parallel()
	testContext := negtypes.NewTestContext()
	podLister := testContext.PodInformer.GetIndexer()

	namespace := "ns1"
	name := "n1"
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"app.kubernetes.io/name":    "pod-with-long-name",
				"app.kubernetes.io/version": "v1",
				"foo-key":                   "foo",
			},
		},
	}
	podLister.Add(pod)

	for _, tc := range []struct {
		desc      string
		lpConfig  PodLabelPropagationConfig
		expect    PodLabelMap
		expectErr bool
	}{
		{
			desc: "No short key for both labels, no truncation needed for both",
			lpConfig: PodLabelPropagationConfig{
				Labels: []Label{
					{
						Key:               "app.kubernetes.io/name",
						MaxLabelSizeBytes: 40,
					},
					{
						Key:               "app.kubernetes.io/version",
						MaxLabelSizeBytes: 30,
					},
				},
			},
			expect: PodLabelMap{
				"app.kubernetes.io/name":    "pod-with-long-name",
				"app.kubernetes.io/version": "v1",
			},
			expectErr: false,
		},
		{
			desc: "Short key for both labels, truncation needed for the first one.",
			lpConfig: PodLabelPropagationConfig{
				Labels: []Label{
					{
						Key:               "app.kubernetes.io/name",
						ShortKey:          "name",
						MaxLabelSizeBytes: 20,
					},
					{
						Key:               "app.kubernetes.io/version",
						ShortKey:          "version",
						MaxLabelSizeBytes: 30,
					},
				},
			},
			expect: PodLabelMap{
				"name":    "pod-with-long-na",
				"version": "v1",
			},
			expectErr: true,
		},
		{
			desc: "No short key for both labels, truncation needed for the first one, truncation failed for the second one",
			lpConfig: PodLabelPropagationConfig{
				Labels: []Label{
					{
						Key:               "app.kubernetes.io/name",
						MaxLabelSizeBytes: 30,
					},
					{
						Key:               "app.kubernetes.io/version",
						MaxLabelSizeBytes: 20,
					},
				},
			},
			expect: PodLabelMap{
				"app.kubernetes.io/name": "pod-with",
			},
			expectErr: true,
		},
	} {
		ret, err := GetPodLabelMap(pod, tc.lpConfig)
		if !reflect.DeepEqual(ret, tc.expect) {
			t.Errorf("For test case %q, got label map %+v, want %+v", tc.desc, ret, tc.expect)
		}
		if (err != nil) != tc.expectErr {
			t.Errorf("For test case %q, got error %v, expectErr: %t ", tc.desc, err, tc.expectErr)
		}
	}
}

func TestTruncateLabel(t *testing.T) {
	for _, tc := range []struct {
		desc              string
		key               string
		label             string
		maxLabelSizeBytes int
		expect            string
		expectErr         error
	}{
		{
			desc:              "No truncation needed",
			key:               "app.kubernetes.io/name",
			label:             "pod-with-long-name",
			maxLabelSizeBytes: 40,
			expect:            "pod-with-long-name",
			expectErr:         nil,
		},
		{
			desc:              "No truncation needed with label shorter than minLabelLength",
			key:               "app.kubernetes.io/name",
			label:             "pod",
			maxLabelSizeBytes: 25,
			expect:            "pod",
			expectErr:         nil,
		},
		{
			desc:              "Truncation needed",
			key:               "app.kubernetes.io/name",
			label:             "pod-with-long-name",
			maxLabelSizeBytes: 30,
			expect:            "pod-with",
			expectErr:         ErrLabelTruncated,
		},
		{
			desc:              "Truncation failed with key length exceeding limit",
			key:               "app.kubernetes.io/name",
			label:             "pod-with-long-name",
			maxLabelSizeBytes: 20,
			expect:            "",
			expectErr:         ErrLabelTruncationFailed,
		},
		{
			desc:              "Truncation failed with truncated label shorter than minLabelLength",
			key:               "app.kubernetes.io/name",
			label:             "pod-with-long-name",
			maxLabelSizeBytes: 25,
			expect:            "",
			expectErr:         ErrLabelTruncationFailed,
		},
	} {
		label, err := truncatePodLabel(tc.key, tc.label, tc.maxLabelSizeBytes)
		if tc.expect != label {
			t.Errorf("For test case %q, got label value: %s, want: %s,", tc.desc, label, tc.expect)
		}
		if !errors.Is(err, tc.expectErr) {
			t.Errorf("For test case %q, got error: %v, want: %v", tc.desc, err, tc.expectErr)
		}
	}
}
