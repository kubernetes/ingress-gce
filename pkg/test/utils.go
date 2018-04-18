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

package test

import (
	"io/ioutil"

	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
)

// GetTestIngress returns an Ingress based on a spec defined in a YAML file.
func GetTestIngress(filename string) (v1beta1.Ingress, error) {
	ing := v1beta1.Ingress{}
	rawIng, err := ioutil.ReadFile(filename)
	if err != nil {
		return ing, err
	}
	json, err := utilyaml.ToJSON([]byte(rawIng))
	if err != nil {
		return v1beta1.Ingress{}, err
	}
	if err := runtime.DecodeInto(legacyscheme.Codecs.UniversalDecoder(), json, &ing); err != nil {
		return v1beta1.Ingress{}, err
	}
	return ing, nil
}
