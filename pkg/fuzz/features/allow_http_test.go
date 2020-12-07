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

package features

import (
	"testing"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/fuzz"
)

func TestAllowHTTPFeature(t *testing.T) {
	t.Parallel()

	v := &AllowHTTPFeature{}
	a := &fuzz.IngressValidatorAttributes{CheckHTTP: true}
	env := &fuzz.MockValidatorEnv{}

	ing := fuzz.NewIngressBuilder("ns1", "ing1", "").Build()
	ing.Annotations = map[string]string{}
	ing.Annotations[annotations.AllowHTTPKey] = "false"

	if err := v.ConfigureAttributes(env, ing, nil, nil, a); err != nil {
		t.Fatalf("v.ConfigureAttributes(%+v) = %v, want nil", ing, err)
	}
	if a.CheckHTTP {
		t.Error("a.CheckHTTP = true, want false")
	}
}
