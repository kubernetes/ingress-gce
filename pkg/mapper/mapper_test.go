// Copyright 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mapper

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/ingress-gce/pkg/test"
)

func TestServices(t *testing.T) {
	ing, err := test.GetTestIngress("../test/manifests/ing1.yaml")
	if err != nil {
		t.Fatalf("Error occured when getting test Ingress: %v", err)
	}

	testCases := []struct {
		clusterServiceMapperImpl ClusterServiceMapper
		expected                 map[v1beta1.IngressBackend]v1.Service
	}{
		{
			NewClusterServiceMapper(stubbedSvcGetter, nil),
			map[v1beta1.IngressBackend]v1.Service{
				v1beta1.IngressBackend{ServiceName: "default", ServicePort: intstr.FromInt(80)}: v1.Service{ObjectMeta: meta_v1.ObjectMeta{Name: "default"}},
				v1beta1.IngressBackend{ServiceName: "testy", ServicePort: intstr.FromInt(80)}:   v1.Service{ObjectMeta: meta_v1.ObjectMeta{Name: "testy"}},
				v1beta1.IngressBackend{ServiceName: "testx", ServicePort: intstr.FromInt(80)}:   v1.Service{ObjectMeta: meta_v1.ObjectMeta{Name: "testx"}},
				v1beta1.IngressBackend{ServiceName: "testz", ServicePort: intstr.FromInt(80)}:   v1.Service{ObjectMeta: meta_v1.ObjectMeta{Name: "testz"}},
			},
		},
		{
			NewClusterServiceMapper(stubbedErrorSvcGetter, nil),
			make(map[v1beta1.IngressBackend]v1.Service),
		},
		{
			NewClusterServiceMapper(stubbedSvcGetter, []string{"default", "testz"}),
			map[v1beta1.IngressBackend]v1.Service{
				v1beta1.IngressBackend{ServiceName: "default", ServicePort: intstr.FromInt(80)}: v1.Service{ObjectMeta: meta_v1.ObjectMeta{Name: "default"}},
				v1beta1.IngressBackend{ServiceName: "testz", ServicePort: intstr.FromInt(80)}:   v1.Service{ObjectMeta: meta_v1.ObjectMeta{Name: "testz"}},
			},
		},
	}
	for _, testCase := range testCases {
		res, _ := testCase.clusterServiceMapperImpl.Services(&ing)
		if !reflect.DeepEqual(res, testCase.expected) {
			t.Errorf("Result %v does not match expected %v", res, testCase.expected)
		}
	}
}

// stubbedErrorSvcGetter always returns an error.
func stubbedErrorSvcGetter(svcName, namespace string) (*v1.Service, error) {
	return nil, fmt.Errorf("service %v/%v not found in store", namespace, svcName)
}

// stubbedSvcGetter returns a skeleton Service.
func stubbedSvcGetter(svcName, namespace string) (*v1.Service, error) {
	return &v1.Service{ObjectMeta: meta_v1.ObjectMeta{Name: svcName}}, nil
}
