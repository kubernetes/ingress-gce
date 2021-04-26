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
	"reflect"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	fakeGaFeature    = "fakegafeature"
	fakeAlphaFeature = "fakealphafeature"
	fakeBetaFeature  = "fakebetafeature"
	// This fake feature is used to test per-resource versions
	fakeAlphaFeatureUrlMapOnly = "fakealphafeatureurlmaponly"

	fakeGlobalFeature   = "fakeglobalfeature"
	fakeRegionalFeature = "fakeRegionalFeature"
	fakeZonalFeature    = "fakeZonalFeature"
)

var (
	fakeAlphaFeatureVersions = ResourceVersions{
		meta.VersionAlpha,
		meta.VersionAlpha,
		meta.VersionAlpha,
		meta.VersionAlpha,
		meta.VersionAlpha,
		meta.VersionAlpha,
		meta.VersionAlpha,
	}

	fakeBetaFeatureVersions = ResourceVersions{
		meta.VersionBeta,
		meta.VersionBeta,
		meta.VersionBeta,
		meta.VersionBeta,
		meta.VersionBeta,
		meta.VersionBeta,
		meta.VersionBeta,
	}

	// Empty fields are considered meta.VersionGA
	fakeAlphaFeatureUrlMapOnlyVersions = ResourceVersions{
		UrlMap: meta.VersionAlpha,
	}

	emptyIng = networkingv1.Ingress{
		ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{},
		},
	}

	fakeScopeToFeatures = map[meta.KeyType][]string{
		meta.Global:   {fakeGlobalFeature},
		meta.Regional: {fakeRegionalFeature},
		meta.Zonal:    {fakeZonalFeature},
	}

	fakeFeatureToVersions = map[string]*ResourceVersions{
		fakeGaFeature:              GAResourceVersions,
		fakeAlphaFeature:           &fakeAlphaFeatureVersions,
		fakeBetaFeature:            &fakeBetaFeatureVersions,
		fakeAlphaFeatureUrlMapOnly: &fakeAlphaFeatureUrlMapOnlyVersions,
	}
)

// TODO: (shance) add real test cases once features are added
func TestScopeFromIngress(t *testing.T) {
	testCases := []struct {
		desc  string
		ing   networkingv1.Ingress
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

func TestMergeVersions(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		desc     string
		cur      meta.Version
		new      meta.Version
		expected meta.Version
	}{
		{
			desc:     "cur empty",
			new:      meta.VersionAlpha,
			expected: meta.VersionAlpha,
		},
		{
			desc:     "new empty",
			cur:      meta.VersionAlpha,
			expected: meta.VersionAlpha,
		},
		{
			desc:     "Newer version is lower",
			cur:      meta.VersionBeta,
			new:      meta.VersionAlpha,
			expected: meta.VersionAlpha,
		},
		{
			desc:     "Newer version is higher",
			cur:      meta.VersionAlpha,
			new:      meta.VersionBeta,
			expected: meta.VersionAlpha,
		},
		{
			desc:     "Same version",
			cur:      meta.VersionGA,
			new:      meta.VersionGA,
			expected: meta.VersionGA,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := mergeVersions(tc.cur, tc.new)

			if result != tc.expected {
				t.Fatalf("want %q, got %q", tc.expected, result)
			}
		})
	}
}

// This test makes sure none of the fields are empty
func TestL7ILBVersions(t *testing.T) {
	result := L7ILBVersions()
	if result.UrlMap == "" ||
		result.BackendService == "" ||
		result.HealthCheck == "" ||
		result.SslCertificate == "" ||
		result.TargetHttpsProxy == "" ||
		result.TargetHttpProxy == "" ||
		result.ForwardingRule == "" {
		t.Fatalf("L7ILBVersions returned an empty field")
	}
}

func TestVersionsFromFeatures(t *testing.T) {
	testCases := []struct {
		desc     string
		features []string
		expected *ResourceVersions
	}{
		{
			desc:     "No Features",
			expected: GAResourceVersions,
		},
		{
			desc:     "Alpha features",
			features: []string{fakeAlphaFeature},
			expected: &ResourceVersions{
				UrlMap:           meta.VersionAlpha,
				ForwardingRule:   meta.VersionAlpha,
				TargetHttpProxy:  meta.VersionAlpha,
				TargetHttpsProxy: meta.VersionAlpha,
				SslCertificate:   meta.VersionAlpha,
				BackendService:   meta.VersionAlpha,
				HealthCheck:      meta.VersionAlpha,
			},
		},
		{
			desc:     "Beta features",
			features: []string{fakeBetaFeature},
			expected: &ResourceVersions{
				UrlMap:           meta.VersionBeta,
				ForwardingRule:   meta.VersionBeta,
				TargetHttpProxy:  meta.VersionBeta,
				TargetHttpsProxy: meta.VersionBeta,
				SslCertificate:   meta.VersionBeta,
				BackendService:   meta.VersionBeta,
				HealthCheck:      meta.VersionBeta,
			},
		},
		{
			desc:     "Differing versions",
			features: []string{fakeGaFeature, fakeAlphaFeatureUrlMapOnly},
			expected: NewResourceVersions().merge(&ResourceVersions{UrlMap: meta.VersionAlpha}),
		},
		{
			desc: "Many features",
			features: []string{
				fakeGaFeature,
				fakeAlphaFeature,
				fakeBetaFeature,
				fakeAlphaFeatureUrlMapOnly,
			},
			expected: &ResourceVersions{
				UrlMap:           meta.VersionAlpha,
				ForwardingRule:   meta.VersionAlpha,
				TargetHttpProxy:  meta.VersionAlpha,
				TargetHttpsProxy: meta.VersionAlpha,
				SslCertificate:   meta.VersionAlpha,
				BackendService:   meta.VersionAlpha,
				HealthCheck:      meta.VersionAlpha,
			},
		},
	}

	// Override features with fakes
	featureToVersions = fakeFeatureToVersions
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := versionsFromFeatures(tc.features)

			if !reflect.DeepEqual(result, tc.expected) {
				t.Fatalf("want %v, got %v", tc.expected, result)
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
