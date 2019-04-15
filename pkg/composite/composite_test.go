/*
Copyright 2018 The Kubernetes Authors.

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

package composite

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/kr/pretty"
	computealpha "google.golang.org/api/compute/v0.alpha"
	compute "google.golang.org/api/compute/v1"
)

// TODO(rramkumar): All code in this file should ideally be generated.

func TestBackendService(t *testing.T) {
	// Use reflection to verify that our composite type contains all the
	// same fields as the alpha type.
	compositeType := reflect.TypeOf(BackendService{})
	alphaType := reflect.TypeOf(computealpha.BackendService{})

	// For the composite type, remove the Version field from consideration
	compositeTypeNumFields := compositeType.NumField() - 1
	if compositeTypeNumFields != alphaType.NumField() {
		t.Fatalf("%v should contain %v fields. Got %v", alphaType.Name(), alphaType.NumField(), compositeTypeNumFields)
	}
	// Start loop at 1 to ignore the composite type's Version field.
	for i := 1; i < compositeType.NumField(); i++ {
		if err := compareFields(compositeType.Field(i), alphaType.Field(i-1)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBackend(t *testing.T) {
	compositeType := reflect.TypeOf(Backend{})
	alphaType := reflect.TypeOf(computealpha.Backend{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestBackendServiceIAP(t *testing.T) {
	compositeType := reflect.TypeOf(BackendServiceIAP{})
	alphaType := reflect.TypeOf(computealpha.BackendServiceIAP{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestBackendServiceIAPOAuth2ClientInfo(t *testing.T) {
	compositeType := reflect.TypeOf(BackendServiceIAPOAuth2ClientInfo{})
	alphaType := reflect.TypeOf(computealpha.BackendServiceIAPOAuth2ClientInfo{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestBackendServiceCdnPolicy(t *testing.T) {
	compositeType := reflect.TypeOf(BackendServiceCdnPolicy{})
	alphaType := reflect.TypeOf(computealpha.BackendServiceCdnPolicy{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestCacheKeyPolicy(t *testing.T) {
	compositeType := reflect.TypeOf(CacheKeyPolicy{})
	alphaType := reflect.TypeOf(computealpha.CacheKeyPolicy{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestBackendServiceFailoverPolicy(t *testing.T) {
	compositeType := reflect.TypeOf(BackendServiceFailoverPolicy{})
	alphaType := reflect.TypeOf(computealpha.BackendServiceFailoverPolicy{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestBackendServiceCloudFunctionBackend(t *testing.T) {
	compositeType := reflect.TypeOf(BackendServiceCloudFunctionBackend{})
	alphaType := reflect.TypeOf(computealpha.BackendServiceCloudFunctionBackend{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionDraining(t *testing.T) {
	compositeType := reflect.TypeOf(ConnectionDraining{})
	alphaType := reflect.TypeOf(computealpha.ConnectionDraining{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestBackendServiceAppEngineBackend(t *testing.T) {
	compositeType := reflect.TypeOf(BackendServiceAppEngineBackend{})
	alphaType := reflect.TypeOf(computealpha.BackendServiceAppEngineBackend{})
	if err := typeEquality(compositeType, alphaType); err != nil {
		t.Fatal(err)
	}
}

func TestToBackendService(t *testing.T) {
	testCases := []struct {
		input    interface{}
		expected *BackendService
	}{
		{
			computealpha.BackendService{Protocol: "HTTP2"},
			&BackendService{Protocol: "HTTP2"},
		},
		{
			compute.BackendService{Protocol: "HTTP"},
			&BackendService{Protocol: "HTTP"},
		},
	}
	for _, testCase := range testCases {
		result, _ := toBackendService(testCase.input)
		if !reflect.DeepEqual(result, testCase.expected) {
			t.Fatalf("toBackendService(input) = \ninput = %s\n%s\nwant = \n%s", pretty.Sprint(testCase.input), pretty.Sprint(result), pretty.Sprint(testCase.expected))
		}
	}
}

func TestToAlpha(t *testing.T) {
	composite := BackendService{
		Version:  meta.VersionAlpha,
		Protocol: "HTTP2",
	}
	alpha, err := composite.toAlpha()
	if err != nil {
		t.Fatalf("composite.toAlpha() =_, %v; want _, nil", err)
	}
	// Verify that a known alpha field value is preserved.
	if alpha.Protocol != "HTTP2" {
		t.Errorf("alpha.Protocol = %q, want HTTP2", alpha.Protocol)
	}
}

func TestToBeta(t *testing.T) {
	composite := BackendService{
		Version:        meta.VersionGA,
		SecurityPolicy: "foo",
	}
	beta, err := composite.toBeta()
	if err != nil {
		t.Fatalf("composite.toBeta() =_, %v; want _, nil", err)
	}
	// Verify that a known beta field value is preserved.
	if beta.SecurityPolicy != "foo" {
		t.Errorf("beta.SecurityPolicy = %q, want foo", beta.SecurityPolicy)
	}
}

func TestToGA(t *testing.T) {
	composite := BackendService{
		Version:  meta.VersionGA,
		Protocol: "HTTP",
	}
	ga, err := composite.toGA()
	if err != nil {
		t.Fatalf("composite.toGA() =_, %v; want _, nil", err)
	}
	if ga.Protocol != "HTTP" {
		t.Errorf("ga.Protocol = %q, want HTTP", ga.Protocol)
	}
}

// typeEquality is a generic function that checks type equality.
func typeEquality(t1, t2 reflect.Type) error {
	t1Fields, t2Fields := make(map[string]bool), make(map[string]bool)
	for i := 0; i < t1.NumField(); i++ {
		t1Fields[t1.Field(i).Name] = true
	}
	for i := 0; i < t2.NumField(); i++ {
		t2Fields[t2.Field(i).Name] = true
	}
	if !reflect.DeepEqual(t1Fields, t2Fields) {
		return fmt.Errorf("type = %+v, want %+v", t1Fields, t2Fields)
	}
	for n := range t1Fields {
		f1, _ := t1.FieldByName(n)
		f2, _ := t2.FieldByName(n)
		if err := compareFields(f1, f2); err != nil {
			return err
		}
	}
	return nil
}

// compareFields verifies that two fields in a struct have the same relevant metadata.
// Note: This comparison ignores field offset, index, and pkg path, all of which don't matter.
func compareFields(s1, s2 reflect.StructField) error {
	if s1.Name != s2.Name {
		return fmt.Errorf("field %s name = %q, want %q", s1.Name, s1.Name, s2.Name)
	}
	if s1.Tag != s2.Tag {
		return fmt.Errorf("field %s tag = %q, want %q", s1.Name, s1.Tag, s2.Tag)
	}
	if s1.Type.Name() != s2.Type.Name() {
		return fmt.Errorf("field %s type = %q, want %q", s1.Name, s1.Type.Name(), s2.Type.Name())
	}
	return nil
}
