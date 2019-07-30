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

package utils

import (
	"testing"

	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStrategicMergePatchBytes(t *testing.T) {
	// Patch an Ingress w/ a finalizer
	ing := &v1beta1.Ingress{}
	updated := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"foo"},
		},
	}
	b, err := StrategicMergePatchBytes(ing, updated, v1beta1.Ingress{})
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"metadata":{"finalizers":["foo"]}}`
	if string(b) != expected {
		t.Errorf("StrategicMergePatchBytes(%+v, %+v) = %s ; want %s", ing, updated, string(b), expected)
	}

	// Patch an Ingress with the finalizer removed
	ing = updated
	updated = &v1beta1.Ingress{}
	b, err = StrategicMergePatchBytes(ing, updated, v1beta1.Ingress{})
	if err != nil {
		t.Fatal(err)
	}
	expected = `{"metadata":{"finalizers":null}}`
	if string(b) != expected {
		t.Errorf("StrategicMergePatchBytes(%+v, %+v) = %s ; want %s", ing, updated, string(b), expected)
	}
}
