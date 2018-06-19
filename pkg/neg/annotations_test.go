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

package neg

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/ingress-gce/pkg/annotations"
)

func TestPortNameMapUnion(t *testing.T) {
	testcases := []struct {
		desc        string
		p1          PortNameMap
		p2          PortNameMap
		expectedMap PortNameMap
	}{
		{
			"empty map union empty map",
			PortNameMap{},
			PortNameMap{},
			PortNameMap{},
		},
		{
			"empty map union a non-empty map is the non-empty map",
			PortNameMap{},
			PortNameMap{80: "namedport", 443: "3000"},
			PortNameMap{80: "namedport", 443: "3000"},
		},
		{
			"union of two non-empty maps",
			PortNameMap{443: "3000", 5000: "6000"},
			PortNameMap{80: "namedport", 8080: "9000"},
			PortNameMap{80: "namedport", 443: "3000", 5000: "6000", 8080: "9000"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			result := tc.p1.Union(tc.p2)
			if !reflect.DeepEqual(result, tc.expectedMap) {
				t.Errorf("Expected p1.Union(p2) to equal: %v; got: %v", tc.expectedMap, result)
			}
		})
	}
}

func TestPortNameMapDifference(t *testing.T) {
	testcases := []struct {
		desc        string
		p1          PortNameMap
		p2          PortNameMap
		expectedMap PortNameMap
	}{
		{
			"empty map difference empty map",
			PortNameMap{},
			PortNameMap{},
			PortNameMap{},
		},
		{
			"empty map difference a non-empty map is empty map",
			PortNameMap{},
			PortNameMap{80: "namedport", 443: "3000"},
			PortNameMap{},
		},
		{
			"non-empty map difference a non-empty map is the non-empty map",
			PortNameMap{80: "namedport", 443: "3000"},
			PortNameMap{},
			PortNameMap{80: "namedport", 443: "3000"},
		},
		{
			"difference of two non-empty maps with the same elements",
			PortNameMap{80: "namedport", 443: "3000"},
			PortNameMap{80: "namedport", 443: "3000"},
			PortNameMap{},
		},
		{
			"difference of two non-empty maps with no elements in common returns p1",
			PortNameMap{443: "3000", 5000: "6000"},
			PortNameMap{80: "namedport", 8080: "9000"},
			PortNameMap{443: "3000", 5000: "6000"},
		},
		{
			"difference of two non-empty maps with elements in common",
			PortNameMap{80: "namedport", 443: "3000", 5000: "6000", 8080: "9000"},
			PortNameMap{80: "namedport", 8080: "9000"},
			PortNameMap{443: "3000", 5000: "6000"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			result := tc.p1.Difference(tc.p2)
			if !reflect.DeepEqual(result, tc.expectedMap) {
				t.Errorf("Expected p1.Difference(p2) to equal: %v; got: %v", tc.expectedMap, result)
			}
		})
	}
}

func TestNEGServicePorts(t *testing.T) {
	testcases := []struct {
		desc            string
		annotation      string
		knownPortMap    PortNameMap
		expectedPortMap PortNameMap
		expectedErr     error
	}{
		{
			desc:       "NEG annotation references port that Service does not have",
			annotation: `{"3000":{}}`,
			expectedErr: utilerrors.NewAggregate([]error{
				fmt.Errorf("ServicePort %v doesn't exist on Service", 3000),
			}),
			knownPortMap:    PortNameMap{80: "some_port", 443: "another_port"},
			expectedPortMap: PortNameMap{3000: ""},
		},
		{
			desc:            "NEG annotation references existing service ports",
			annotation:      `{"80":{},"443":{}}`,
			knownPortMap:    PortNameMap{80: "namedport", 443: "3000"},
			expectedPortMap: PortNameMap{80: "namedport", 443: "3000"},
		},

		{
			desc:            "NEGServicePort takes the union of known ports and ports referenced in the annotation",
			annotation:      `{"80":{}}`,
			knownPortMap:    PortNameMap{80: "8080", 3000: "3030", 4000: "4040"},
			expectedPortMap: PortNameMap{80: "8080"},
		},
	}

	for _, tc := range testcases {
		service := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}

		if len(tc.annotation) > 0 {
			service.Annotations[annotations.ExposeNEGAnnotationKey] = tc.annotation
		}

		svc := annotations.FromService(service)
		exposeNegStruct, _ := svc.ExposeNegAnnotation()

		t.Run(tc.desc, func(t *testing.T) {
			svcPorts, err := NEGServicePorts(exposeNegStruct, tc.knownPortMap)
			if tc.expectedErr == nil && err != nil {
				t.Errorf("ExpectedNEGServicePorts to not return an error, got: %v", err)
			}

			if !reflect.DeepEqual(svcPorts, tc.expectedPortMap) {
				t.Errorf("Expected NEGServicePorts to equal: %v; got: %v", tc.expectedPortMap, svcPorts)
			}

			if tc.expectedErr != nil {
				if !reflect.DeepEqual(err, tc.expectedErr) {
					t.Errorf("Expected NEGServicePorts to return a %v error, got: %v", tc.expectedErr, err)
				}
			}
		})
	}
}
