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

package negannotation

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNEGAnnotation(t *testing.T) {
	for _, tc := range []struct {
		desc                string
		svc                 *v1.Service
		expectNegAnnotation *NegAnnotation
		expectError         error
		expectFound         bool
		negEnabled          bool
		ingress             bool
		exposed             bool
	}{
		{
			desc:        "NEG annotation not specified",
			svc:         &v1.Service{},
			expectFound: false,
			expectError: nil,
		},
		{
			desc: "NEG annotation is malformed",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `foo`,
					},
				},
			},
			expectFound: true,
			expectError: ErrNEGAnnotationInvalid,
		},
		{
			desc: "NEG annotation is malformed 2",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{80:{}}}`,
					},
				},
			},
			expectFound: true,
			expectError: ErrNEGAnnotationInvalid,
		},
		{
			desc: "NEG enabled for ingress",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"ingress":true}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				Ingress: true,
			},
			negEnabled: true,
			ingress:    true,
			exposed:    false,
		},
		{
			desc: "NEG exposed",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}}}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
		{
			desc: "Multiple ports exposed",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}, "443":{}}}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}, int32(443): {}},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
		{
			desc: "Service is enabled for both ingress and exposedNEG",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"ingress":true,"exposed_ports":{"80":{}, "443":{}}}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				Ingress:      true,
				ExposedPorts: map[int32]NegAttributes{int32(80): {}, int32(443): {}},
			},
			negEnabled: true,
			ingress:    true,
			exposed:    true,
		},
		{
			desc: "NEG exposed with pre-provisioned zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["us-central1-a","us-central1-b"]}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}},
				Zones:        []string{"us-central1-a", "us-central1-b"},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
		{
			desc: "NEG exposed with pre-provisioned wildcard zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["*"]}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}},
				Zones:        []string{"*"},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
		{
			desc: "NEG exposed with empty zones list",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":[]}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}},
				Zones:        []string{},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
		{
			desc: "NEG exposed with incorrect zone values list",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["incorrect values"]}`,
					},
				},
			},
			expectFound: true,
			expectNegAnnotation: &NegAnnotation{
				ExposedPorts: map[int32]NegAttributes{int32(80): {}},
				Zones:        []string{"incorrect values"},
			},
			negEnabled: true,
			ingress:    false,
			exposed:    true,
		},
	} {
		negAnnotation, found, err := FromService(tc.svc).NEGAnnotation()
		if fmt.Sprintf("%q", err) != fmt.Sprintf("%q", tc.expectError) {
			t.Errorf("Test case %q: Expect error to be %q, but got: %q", tc.desc, tc.expectError, err)
		}

		if found != tc.expectFound {
			t.Errorf("Test case %q: Expect found to be %v, be got %v", tc.desc, tc.expectFound, found)
		}

		if tc.expectError != nil || !tc.expectFound {
			continue
		}

		if !reflect.DeepEqual(*tc.expectNegAnnotation, *negAnnotation) {
			t.Errorf("Test case %q: Expect NegAnnotation to be %v, be got %v", tc.desc, *tc.expectNegAnnotation, *negAnnotation)
		}

		if neg := negAnnotation.NEGEnabled(); neg != tc.negEnabled {
			t.Errorf("Test case %q: Expect EnabledNEG() to be %v, be got %v", tc.desc, tc.negEnabled, neg)
		}

		if ing := negAnnotation.NEGEnabledForIngress(); ing != tc.ingress {
			t.Errorf("Test case %q: Expect NEGEnabledForIngress() = %v; want %v", tc.desc, tc.ingress, ing)
		}

		if exposed := negAnnotation.NEGExposed(); exposed != tc.exposed {
			t.Errorf("Test case %q: Expect NEGExposed() = %v; want %v", tc.desc, tc.exposed, exposed)
		}
	}
}

func TestNEGStatus(t *testing.T) {
	for _, tc := range []struct {
		desc            string
		svc             *v1.Service
		expectNegStatus *NegStatus
		expectError     error
		expectFound     bool
	}{
		{
			desc:        "No NEG Status",
			svc:         &v1.Service{},
			expectFound: false,
			expectError: nil,
		},
		{
			desc: "Basic NEG Status",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGStatusKey: `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
					},
				},
			},
			expectNegStatus: &NegStatus{NetworkEndpointGroups: PortNegMap{"80": "neg-name"}, Zones: []string{"us-central1-a"}},
			expectFound:     true,
			expectError:     nil,
		},
		{
			desc: "Invalid NEG Status",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGStatusKey: `foobar`,
					},
				},
			},
			expectNegStatus: nil,
			expectFound:     true,
			expectError:     fmt.Errorf("Error parsing neg status: invalid character 'o' in literal false (expecting 'a')"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			negStatus, found, err := FromService(tc.svc).NEGStatus()

			if fmt.Sprintf("%q", err) != fmt.Sprintf("%q", tc.expectError) {
				t.Errorf("Test case %q: Expect error to be %q, but got: %q", tc.desc, tc.expectError, err)
			}

			if found != tc.expectFound {
				t.Errorf("Test case %q: Expect found to be %v, be got %v", tc.desc, tc.expectFound, found)
			}

			if tc.expectError != nil || !tc.expectFound {
				return
			}

			if !reflect.DeepEqual(*tc.expectNegStatus, *negStatus) {
				t.Errorf("Test case %q: Expect NegStatus to be %v, be got %v", tc.desc, *tc.expectNegStatus, *negStatus)
			}
		})
	}
}

func TestParseNegStatus(t *testing.T) {
	for _, tc := range []struct {
		desc            string
		status          string
		expectNegStatus *NegStatus
		expectError     error
	}{
		{
			desc:            "Test empty string",
			status:          "",
			expectNegStatus: &NegStatus{},
			expectError:     fmt.Errorf("unexpected end of JSON input"),
		},
		{
			desc:            "Test basic status",
			status:          `{"network_endpoint_groups":{"80":"neg-name"},"zones":["us-central1-a"]}`,
			expectNegStatus: &NegStatus{NetworkEndpointGroups: PortNegMap{"80": "neg-name"}, Zones: []string{"us-central1-a"}},
			expectError:     nil,
		},
		{
			desc:            "Test NEG status with 2 ports",
			status:          `{"network_endpoint_groups":{"80":"neg-name", "443":"another-neg-name"},"zones":["us-central1-a"]}`,
			expectNegStatus: &NegStatus{NetworkEndpointGroups: PortNegMap{"80": "neg-name", "443": "another-neg-name"}, Zones: []string{"us-central1-a"}},
			expectError:     nil,
		},
		{
			desc:            "Incorrect fields",
			status:          `{"network_endpoint_group":{"80":"neg-name"},"zone":["us-central1-a"]}`,
			expectNegStatus: &NegStatus{},
			expectError:     nil,
		},
		{
			desc:            "Invalid annotation",
			status:          `{"network_endpoint_groups":{"80":"neg-name"},"zones":"us-central1-a"]}`,
			expectNegStatus: &NegStatus{},
			expectError:     fmt.Errorf("invalid character ']' after object key:value pair"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			negStatus, err := ParseNegStatus(tc.status)

			if fmt.Sprintf("%q", err) != fmt.Sprintf("%q", tc.expectError) {
				t.Errorf("Test case %q: Expect error to be %q, but got: %q", tc.desc, tc.expectError, err)
			}

			if !reflect.DeepEqual(*tc.expectNegStatus, negStatus) {
				t.Errorf("Expected NegStatus to be %v, got %v instead", tc.expectNegStatus, negStatus)
			}
		})
	}
}

func TestGetPreprovisioningZones(t *testing.T) {
	for _, tc := range []struct {
		desc          string
		svc           *v1.Service
		expectedZones []string
		expectError   bool
	}{
		{
			desc: "Service is nil",
			svc:  nil,
		},
		{
			desc: "NEG annotation not found",
			svc:  &v1.Service{},
		},
		{
			desc: "NEG annotation exists but no zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}}}`,
					},
				},
			},
		},
		{
			desc: "NEG annotation exists with empty zones list",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":[]}`,
					},
				},
			},
			expectedZones: []string{},
		},
		{
			desc: "NEG annotation exists with zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["us-central1-a","us-central1-b"]}`,
					},
				},
			},
			expectedZones: []string{"us-central1-a", "us-central1-b"},
		},
		{
			desc: "NEG annotation has wildcard zone",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["*"]}`,
					},
				},
			},
			expectedZones: []string{"*"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			zones, err := GetPreprovisioningZonesAnnotation(tc.svc)
			if (err != nil) != tc.expectError {
				t.Errorf("GetPreprovisioningZones() error = %v, expectError = %v", err, tc.expectError)
				return
			}
			if !reflect.DeepEqual(zones, tc.expectedZones) {
				t.Errorf("GetPreprovisioningZones() = %v, expected %v", zones, tc.expectedZones)
			}
		})
	}
}

func TestResolvePreprovisioningZones(t *testing.T) {
	for _, tc := range []struct {
		desc                 string
		preprovisioningZones []string
		regionZones          []string
		expectedZones        []string
		expectError          bool
	}{
		{
			desc:                 "Empty preprovisioning zones",
			preprovisioningZones: []string{},
			regionZones:          []string{"us-central1-a"},
		},
		{
			desc:                 "Empty region zones",
			preprovisioningZones: []string{"us-central1-a"},
			regionZones:          []string{},
			expectError:          true,
		},
		{
			desc:                 "Valid zones",
			preprovisioningZones: []string{"us-central1-a", "us-central1-b"},
			regionZones:          []string{"us-central1-a", "us-central1-b", "us-central1-c"},
			expectedZones:        []string{"us-central1-a", "us-central1-b"},
		},
		{
			desc:                 "Invalid zones",
			preprovisioningZones: []string{"us-central1-a", "invalid-zone"},
			regionZones:          []string{"us-central1-a", "us-central1-b"},
			expectError:          true,
		},
		{
			desc:                 "Wildcard zone",
			preprovisioningZones: []string{"*"},
			regionZones:          []string{"us-central1-a", "us-central1-b", "us-central1-c"},
			expectedZones:        []string{"us-central1-a", "us-central1-b", "us-central1-c"},
		},
		{
			desc:                 "Wildcard and other zone",
			preprovisioningZones: []string{"us-central1-a", "*"},
			regionZones:          []string{"us-central1-a", "us-central1-b", "us-central1-c"},
			expectedZones:        []string{"us-central1-a", "us-central1-b", "us-central1-c"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			zones, err := ResolvePreprovisioningZones(tc.preprovisioningZones, tc.regionZones)
			if (err != nil) != tc.expectError {
				t.Errorf("ResolvePreprovisioningZones() error = %v, expectError = %v", err, tc.expectError)
				return
			}
			if !reflect.DeepEqual(zones, tc.expectedZones) {
				t.Errorf("ResolvePreprovisioningZones() = %v, expected %v", zones, tc.expectedZones)
			}
		})
	}
}

type fakeCloudZoneGetter struct {
	zones []string
	err   error
}

func (f *fakeCloudZoneGetter) Zones() ([]string, error) {
	return f.zones, f.err
}

func TestGetAndResolvePreprovisioningZones(t *testing.T) {
	for _, tc := range []struct {
		desc          string
		svc           *v1.Service
		zoneGetter    CloudZoneGetter
		expectedZones []string
		expectError   bool
	}{
		{
			desc: "Nil service",
			svc:  nil,
			zoneGetter: &fakeCloudZoneGetter{
				zones: []string{"us-central1-a"},
			},
			expectedZones: nil,
		},
		{
			desc: "No NEG annotation",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			zoneGetter: &fakeCloudZoneGetter{
				zones: []string{"us-central1-a"},
			},
			expectedZones: nil,
		},
		{
			desc: "NEG annotation with empty zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":[]}`,
					},
				},
			},
			zoneGetter: &fakeCloudZoneGetter{
				zones: []string{"us-central1-a"},
			},
			expectedZones: []string{},
		},
		{
			desc: "NEG annotation with valid zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["us-central1-a","us-central1-b"]}`,
					},
				},
			},
			zoneGetter: &fakeCloudZoneGetter{
				zones: []string{"us-central1-a", "us-central1-b", "us-central1-c"},
			},
			expectedZones: []string{"us-central1-a", "us-central1-b"},
		},
		{
			desc: "NEG annotation with invalid zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["invalid-zone"]}`,
					},
				},
			},
			zoneGetter: &fakeCloudZoneGetter{
				zones: []string{"us-central1-a", "us-central1-b"},
			},
			expectError: true,
		},
		{
			desc: "NEG annotation with wildcard zone",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["*"]}`,
					},
				},
			},
			zoneGetter: &fakeCloudZoneGetter{
				zones: []string{"us-central1-a", "us-central1-b", "us-central1-c"},
			},
			expectedZones: []string{"us-central1-a", "us-central1-b", "us-central1-c"},
		},
		{
			desc: "ZoneGetter returns error",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["us-central1-a"]}`,
					},
				},
			},
			zoneGetter: &fakeCloudZoneGetter{
				err: fmt.Errorf("cloud error"),
			},
			expectError: true,
		},
		{
			desc: "Nil ZoneGetter with non-empty preprovisioning zones",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						NEGAnnotationKey: `{"exposed_ports":{"80":{}},"zones":["us-central1-a"]}`,
					},
				},
			},
			zoneGetter:  nil,
			expectError: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			zones, err := GetPreprovisioningZones(tc.svc, tc.zoneGetter)
			if (err != nil) != tc.expectError {
				t.Errorf("GetAndResolvePreprovisioningZones() error = %v, expectError = %v", err, tc.expectError)
				return
			}
			if !reflect.DeepEqual(zones, tc.expectedZones) {
				t.Errorf("GetAndResolvePreprovisioningZones() = %v, expected %v", zones, tc.expectedZones)
			}
		})
	}
}
