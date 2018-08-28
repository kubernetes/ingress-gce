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
	"k8s.io/ingress-gce/pkg/neg/types"
)

func TestNEGServicePorts(t *testing.T) {
	testcases := []struct {
		desc            string
		annotation      string
		knownPortMap    types.PortNameMap
		expectedPortMap types.PortNameMap
		expectedErr     error
	}{
		{
			desc:       "NEG annotation references port that Service does not have",
			annotation: `{"exposed_ports":{"3000":{}}}`,
			expectedErr: utilerrors.NewAggregate([]error{
				fmt.Errorf("port %v specified in %q doesn't exist in the service", 3000, annotations.NEGAnnotationKey),
			}),
			knownPortMap:    types.PortNameMap{80: "some_port", 443: "another_port"},
			expectedPortMap: types.PortNameMap{3000: ""},
		},
		{
			desc:            "NEG annotation references existing service ports",
			annotation:      `{"exposed_ports":{"80":{},"443":{}}}`,
			knownPortMap:    types.PortNameMap{80: "namedport", 443: "3000"},
			expectedPortMap: types.PortNameMap{80: "namedport", 443: "3000"},
		},

		{
			desc:            "NEGServicePort takes the union of known ports and ports referenced in the annotation",
			annotation:      `{"exposed_ports":{"80":{}}}`,
			knownPortMap:    types.PortNameMap{80: "8080", 3000: "3030", 4000: "4040"},
			expectedPortMap: types.PortNameMap{80: "8080"},
		},
	}

	for _, tc := range testcases {
		service := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}

		if len(tc.annotation) > 0 {
			service.Annotations[annotations.NEGAnnotationKey] = tc.annotation
		}

		svc := annotations.FromService(service)
		_, exposeNegStruct, _ := svc.NEGAnnotation()

		t.Run(tc.desc, func(t *testing.T) {
			svcPorts, err := NEGServicePorts(exposeNegStruct, tc.knownPortMap)
			if tc.expectedErr == nil && err != nil {
				t.Errorf("ExpectedNEGServicePorts to not return an error, got: %v", err)
			}

			if !reflect.DeepEqual(svcPorts, tc.expectedPortMap) {
				t.Errorf("Expected NEGServicePorts to equal: %v; got: %v; err: %v", tc.expectedPortMap, svcPorts, err)
			}

			if tc.expectedErr != nil {
				if !reflect.DeepEqual(err, tc.expectedErr) {
					t.Errorf("Expected NEGServicePorts to return a %v error, got: %v", tc.expectedErr, err)
				}
			}
		})
	}
}
