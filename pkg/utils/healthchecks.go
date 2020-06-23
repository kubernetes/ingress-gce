/*
Copyright 2020 The Kubernetes Authors.

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
	"encoding/json"

	computealpha "google.golang.org/api/compute/v0.alpha"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/compute/v1"
)

// ToV1HealthCheck converts alpha health check to v1 health check.
// WARNING: alpha health check has a additional PORT_SPECIFICATION field.
// This field will be omitted after conversion.
func ToV1HealthCheck(hc *computealpha.HealthCheck) (*compute.HealthCheck, error) {
	ret := &compute.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

// ToBetaHealthCheck converts alpha health check to beta health check.
func ToBetaHealthCheck(hc *computealpha.HealthCheck) (*computebeta.HealthCheck, error) {
	ret := &computebeta.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

// V1ToAlphaHealthCheck converts v1 health check to alpha health check.
// There should be no information lost after conversion.
func V1ToAlphaHealthCheck(hc *compute.HealthCheck) (*computealpha.HealthCheck, error) {
	ret := &computealpha.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

// BetaToAlphaHealthCheck converts beta health check to alpha health check.
// There should be no information lost after conversion.
func BetaToAlphaHealthCheck(hc *computebeta.HealthCheck) (*computealpha.HealthCheck, error) {
	ret := &computealpha.HealthCheck{}
	err := copyViaJSON(ret, hc)
	return ret, err
}

type jsonConvertable interface {
	MarshalJSON() ([]byte, error)
}

func copyViaJSON(dest interface{}, src jsonConvertable) error {
	var err error
	bytes, err := src.MarshalJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dest)
}
