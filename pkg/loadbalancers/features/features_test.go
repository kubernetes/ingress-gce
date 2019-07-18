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

package features

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

const (
	fakeGaFeature    = "fakegafeature"
	fakeAlphaFeature = "fakealphafeature"
	fakeBetaFeature  = "fakebetafeature"

	fakeGlobalFeature   = "fakeglobalfeature"
	fakeRegionalFeature = "fakeRegionalFeature"
	fakeZonalFeature    = "fakeZonalFeature"
)

var (
	emptyIng = v1beta1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{},
		},
	}

	fakeScopeToFeatures = map[meta.KeyType][]string{
		meta.Global:   {fakeGlobalFeature},
		meta.Regional: {fakeRegionalFeature},
		meta.Zonal:    {fakeZonalFeature},
	}

	fakeVersionToFeatures = map[meta.Version][]string{
		meta.VersionGA:    {fakeGaFeature},
		meta.VersionAlpha: {fakeAlphaFeature},
		meta.VersionBeta:  {fakeBetaFeature},
	}
)

// TODO: (shance) add real test cases once features are added
func TestScopeFromIngress(t *testing.T) {
	testCases := []struct {
		desc  string
		ing   v1beta1.Ingress
		scope meta.KeyType
	}{
		{
			desc:  "Empty Ingress",
			ing:   emptyIng,
			scope: meta.Global,
		},
	}

	// Override features with fakes
	scopeToFeatures = fakeScopeToFeatures
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := ScopeFromIngress(&tc.ing)

			if result != tc.scope {
				t.Fatalf("want scope %s, got %s", tc.scope, result)
			}
		})
	}
}

// TODO: (shance) add real test cases once features are added
func TestVersionFromIngress(t *testing.T) {
	testCases := []struct {
		desc    string
		ing     v1beta1.Ingress
		version meta.Version
	}{
		{
			desc:    "Empty Ingress",
			ing:     emptyIng,
			version: meta.VersionGA,
		},
	}

	// Override features with fakes
	versionToFeatures = fakeVersionToFeatures
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := VersionFromIngress(&tc.ing)

			if result != tc.version {
				t.Fatalf("want scope %s, got %s", tc.version, result)
			}
		})
	}
}

func TestVersionFromFeatures(t *testing.T) {
	testCases := []struct {
		desc     string
		features []string
		version  meta.Version
	}{
		{
			desc:     "One Ga feature",
			features: []string{fakeGaFeature},
			version:  meta.VersionGA,
		},
		{
			desc:     "One Alpha feature",
			features: []string{fakeAlphaFeature},
			version:  meta.VersionAlpha,
		},
		{
			desc:     "One Beta feature",
			features: []string{fakeBetaFeature},
			version:  meta.VersionBeta,
		},
		{
			desc:     "Beta and GA feature",
			features: []string{fakeBetaFeature, fakeGaFeature},
			version:  meta.VersionBeta,
		},
		{
			desc:     "Alpha, Beta, and GA feature",
			features: []string{fakeAlphaFeature, fakeBetaFeature, fakeGaFeature},
			version:  meta.VersionAlpha,
		},
		{
			desc:     "Unknown features",
			features: []string{"unknownfeature"},
			version:  meta.VersionGA,
		},
	}

	// Override features with fakes
	scopeToFeatures = fakeScopeToFeatures
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := versionFromFeatures(tc.features)

			if result != tc.version {
				t.Fatalf("want scope %s, got %s", tc.version, result)
			}
		})
	}
}

func TestScopeFromFeatures(t *testing.T) {
	testCases := []struct {
		desc     string
		features []string
		scope    meta.KeyType
	}{
		{
			desc:     "One Global feature",
			features: []string{fakeGlobalFeature},
			scope:    meta.Global,
		},
		{
			desc:     "One Regional feature",
			features: []string{fakeRegionalFeature},
			scope:    meta.Regional,
		},
		{
			desc:     "One Zonal feature",
			features: []string{fakeZonalFeature},
			scope:    meta.Zonal,
		},
		{
			desc:     "Unknown features",
			features: []string{"unknownfeature"},
			scope:    meta.Global,
		},
	}

	// Override features with fakes
	scopeToFeatures = fakeScopeToFeatures

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := scopeFromFeatures(tc.features)

			if result != tc.scope {
				t.Fatalf("want scope %s, got %s", tc.scope, result)
			}
		})
	}
}
