/*
Copyright 2019 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"reflect"
)

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

// typeEquality is a generic function that checks type equality.
func typeEquality(t1, t2 reflect.Type, all bool) error {
	t1Fields, t2Fields := make(map[string]bool), make(map[string]bool)
	for i := 0; i < t1.NumField(); i++ {
		t1Fields[t1.Field(i).Name] = true
	}
	for i := 0; i < t2.NumField(); i++ {
		t2Fields[t2.Field(i).Name] = true
	}

	// Only compare all fields if 'all' is set to true
	// If this is set to false, it effectively only checks that all fields
	// in t1 are present in t2
	if all {
		if !reflect.DeepEqual(t1Fields, t2Fields) {
			return fmt.Errorf("type = %+v, want %+v", t1Fields, t2Fields)
		}
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

func copyViaJSON(dest interface{}, src interface{}) error {
	var err error
	bytes, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dest)
}
