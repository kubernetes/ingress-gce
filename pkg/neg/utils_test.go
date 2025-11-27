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

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/negannotation"
)

func TestNEGServicePorts(t *testing.T) {
	portName0 := ""
	portName1 := "name1"
	portName2 := "name2"

	testcases := []struct {
		desc                  string
		annotation            string
		knownPortMap          []types.SvcPortTuple
		expectedPortMap       []types.SvcPortTuple
		expectedCustomNameMap map[types.SvcPortTuple]string
		expectedErr           error
	}{
		{
			desc:       "NEG annotation references port that Service does not have",
			annotation: `{"exposed_ports":{"3000":{}}}`,
			expectedErr: utilerrors.NewAggregate([]error{
				fmt.Errorf("port %v specified in %q doesn't exist in the service", 3000, negannotation.NEGAnnotationKey),
			}),
			knownPortMap: []types.SvcPortTuple{
				{
					Name:       portName0,
					Port:       80,
					TargetPort: "some_port",
				},
				{
					Name:       portName0,
					Port:       443,
					TargetPort: "another_port",
				},
			},
		},
		{
			desc:       "NEG annotation references existing service ports",
			annotation: `{"exposed_ports":{"80":{},"443":{}}}`,
			knownPortMap: []types.SvcPortTuple{
				{
					Name:       portName0,
					Port:       80,
					TargetPort: "namedport",
				},
				{
					Name:       portName0,
					Port:       443,
					TargetPort: "3000",
				},
			},
			expectedPortMap: []types.SvcPortTuple{
				{
					Name:       portName0,
					Port:       80,
					TargetPort: "namedport",
				},
				{
					Name:       portName0,
					Port:       443,
					TargetPort: "3000",
				},
			},
		},
		{
			desc:       "NEGServicePort takes the union of known ports and ports referenced in the annotation",
			annotation: `{"exposed_ports":{"80":{}}}`,
			knownPortMap: []types.SvcPortTuple{
				{
					Name:       portName0,
					Port:       80,
					TargetPort: "8080",
				},
				{
					Name:       portName0,
					Port:       3000,
					TargetPort: "3030",
				},
				{
					Name:       portName0,
					Port:       4000,
					TargetPort: "4040",
				},
			},
			expectedPortMap: []types.SvcPortTuple{
				{
					Name:       portName0,
					Port:       80,
					TargetPort: "8080",
				},
			},
		},
		{
			desc:       "NEGServicePort takes the union of known ports with port names",
			annotation: `{"exposed_ports":{"80":{}, "3000":{}}}`,
			knownPortMap: []types.SvcPortTuple{
				{
					Name:       portName1,
					Port:       80,
					TargetPort: "8080",
				},
				{
					Name:       portName2,
					Port:       3000,
					TargetPort: "3030",
				},
				{
					Name:       portName0,
					Port:       4000,
					TargetPort: "4040",
				},
			},
			expectedPortMap: []types.SvcPortTuple{
				{
					Name:       portName1,
					Port:       80,
					TargetPort: "8080",
				},
				{
					Name:       portName2,
					Port:       3000,
					TargetPort: "3030",
				},
			},
		},
		{
			desc:       "NEG annotation has custom names for negs",
			annotation: `{"exposed_ports":{"80":{"name":"neg-name"},"443":{}}}`,
			knownPortMap: []types.SvcPortTuple{
				{
					Name:       portName0,
					Port:       80,
					TargetPort: "namedport",
				},
				{
					Name:       portName0,
					Port:       443,
					TargetPort: "3000",
				},
			},
			expectedPortMap: []types.SvcPortTuple{
				{
					Name:       portName0,
					Port:       80,
					TargetPort: "namedport",
				},
				{
					Name:       portName0,
					Port:       443,
					TargetPort: "3000",
				},
			},
			expectedCustomNameMap: map[types.SvcPortTuple]string{
				types.SvcPortTuple{Name: portName0, Port: 80, TargetPort: "namedport"}: "neg-name",
			},
		},
	}

	for _, tc := range testcases {
		service := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}

		if len(tc.annotation) > 0 {
			service.Annotations[negannotation.NEGAnnotationKey] = tc.annotation
		}

		svc := negannotation.FromService(service)
		exposeNegStruct, _, _ := svc.NEGAnnotation()

		t.Run(tc.desc, func(t *testing.T) {
			inputSet := types.NewSvcPortTupleSet(tc.knownPortMap...)
			expectSet := types.NewSvcPortTupleSet(tc.expectedPortMap...)

			outputSet, customNameMap, err := negServicePorts(exposeNegStruct, inputSet)
			if tc.expectedErr == nil && err != nil {
				t.Errorf("ExpectedNEGServicePorts to not return an error, got: %v", err)
			}

			if !reflect.DeepEqual(outputSet, expectSet) {
				t.Errorf("Expected negServicePorts to equal: %v == %v; err: %v", expectSet, outputSet, err)
			}

			if tc.expectedErr != nil {
				if !reflect.DeepEqual(err, tc.expectedErr) {
					t.Errorf("Expected negServicePorts to return a %v error, got: %v", tc.expectedErr, err)
				}
			}

			if len(tc.expectedCustomNameMap) == 0 && len(customNameMap) != 0 {
				t.Errorf("Expected no custom names, but found %+v", customNameMap)
			}

			for expectedTuple, expectedNegName := range tc.expectedCustomNameMap {
				if negName, ok := customNameMap[expectedTuple]; ok {
					if negName != expectedNegName {
						t.Errorf("Expected neg name for tuple %+v to be %s, but was %s", expectedTuple, expectedNegName, negName)
					}
				} else {
					t.Errorf("Expected tuple %+v to be a key in customNameMap", expectedTuple)
				}
			}
		})
	}
}

func TestIsZoneChanged(t *testing.T) {
	testCases := []struct {
		desc          string
		oldZones      []string
		newZones      []string
		expectChanged bool
	}{
		{
			desc:          "a zone is added",
			oldZones:      []string{"zone1"},
			newZones:      []string{"zone1", "zone2"},
			expectChanged: true,
		},
		{
			desc:          "a zone is deleted",
			oldZones:      []string{"zone1", "zone2"},
			newZones:      []string{"zone1"},
			expectChanged: true,
		},
		{
			desc:          "zones stay unchaged",
			oldZones:      []string{"zone1", "zone2"},
			newZones:      []string{"zone1", "zone2"},
			expectChanged: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotChanged := isZoneChanged(tc.oldZones, tc.newZones)
			if gotChanged != tc.expectChanged {
				t.Errorf("Got zone changed = %v, expected %v", gotChanged, tc.expectChanged)
			}
		})
	}
}

func TestIsSubnetChange(t *testing.T) {
	defaultSubnet := "default"
	defaultSubnetPath := "projects/my-project/regions/us-central1/subnetworks/default"

	additionalSubnet := "add-subnet"
	additionalSubnetPath := "add-subnet"
	testCases := []struct {
		desc          string
		oldSubnets    []nodetopologyv1.SubnetConfig
		newSubnets    []nodetopologyv1.SubnetConfig
		expectChanged bool
	}{
		{
			desc: "a subnet is added",
			oldSubnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultSubnet, SubnetPath: defaultSubnetPath},
			},
			newSubnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultSubnet, SubnetPath: defaultSubnetPath},
				{Name: additionalSubnet, SubnetPath: additionalSubnetPath},
			},
			expectChanged: true,
		},
		{
			desc: "a subnet is deleted",
			oldSubnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultSubnet, SubnetPath: defaultSubnetPath},
				{Name: additionalSubnet, SubnetPath: additionalSubnetPath},
			},
			newSubnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultSubnet, SubnetPath: defaultSubnetPath},
			},
			expectChanged: true,
		},
		{
			desc: "subnets stay unchaged",
			oldSubnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultSubnet, SubnetPath: defaultSubnetPath},
				{Name: additionalSubnet, SubnetPath: additionalSubnetPath},
			},
			newSubnets: []nodetopologyv1.SubnetConfig{
				{Name: defaultSubnet, SubnetPath: defaultSubnetPath},
				{Name: additionalSubnet, SubnetPath: additionalSubnetPath},
			},
			expectChanged: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotChanged := isSubnetChanged(tc.oldSubnets, tc.newSubnets)
			if gotChanged != tc.expectChanged {
				t.Errorf("Got subnet changed = %v, expected %v", gotChanged, tc.expectChanged)
			}
		})
	}
}
