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
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
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

	makeEntry = func(ga, alpha, beta string) map[meta.Version][]string {
		return map[meta.Version][]string{meta.VersionGA: {ga}, meta.VersionAlpha: {alpha}, meta.VersionBeta: {beta}}
	}

	fakeResourceToVersionMap = map[LBResource]map[meta.Version][]string{
		UrlMap: {
			meta.VersionGA:    {fakeGaFeature},
			meta.VersionAlpha: {fakeAlphaFeature, fakeAlphaFeatureUrlMapOnly},
			meta.VersionBeta:  {fakeBetaFeature},
		},
		ForwardingRule:   makeEntry(fakeGaFeature, fakeAlphaFeature, fakeBetaFeature),
		TargetHttpProxy:  makeEntry(fakeGaFeature, fakeAlphaFeature, fakeBetaFeature),
		TargetHttpsProxy: makeEntry(fakeGaFeature, fakeAlphaFeature, fakeBetaFeature),
		SslCertificate:   makeEntry(fakeGaFeature, fakeAlphaFeature, fakeBetaFeature),
		BackendService:   makeEntry(fakeGaFeature, fakeAlphaFeature, fakeBetaFeature),
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

func TestVersionFromFeatures(t *testing.T) {
	testCases := []struct {
		desc       string
		features   []string
		versionMap map[LBResource]meta.Version
	}{
		{
			desc: "No Features",
			versionMap: map[LBResource]meta.Version{
				UrlMap:           meta.VersionGA,
				ForwardingRule:   meta.VersionGA,
				TargetHttpProxy:  meta.VersionGA,
				TargetHttpsProxy: meta.VersionGA,
				SslCertificate:   meta.VersionGA,
				BackendService:   meta.VersionGA,
			},
		},
		{
			desc:     "Alpha features",
			features: []string{fakeAlphaFeature},
			versionMap: map[LBResource]meta.Version{
				UrlMap:           meta.VersionAlpha,
				ForwardingRule:   meta.VersionAlpha,
				TargetHttpProxy:  meta.VersionAlpha,
				TargetHttpsProxy: meta.VersionAlpha,
				SslCertificate:   meta.VersionAlpha,
				BackendService:   meta.VersionAlpha,
			},
		},
		{
			desc:     "Beta features",
			features: []string{fakeBetaFeature},
			versionMap: map[LBResource]meta.Version{
				UrlMap:           meta.VersionBeta,
				ForwardingRule:   meta.VersionBeta,
				TargetHttpProxy:  meta.VersionBeta,
				TargetHttpsProxy: meta.VersionBeta,
				SslCertificate:   meta.VersionBeta,
				BackendService:   meta.VersionBeta,
			},
		},
		{
			desc:     "Differing versions",
			features: []string{fakeGaFeature, fakeAlphaFeatureUrlMapOnly},
			versionMap: map[LBResource]meta.Version{
				UrlMap:           meta.VersionAlpha,
				ForwardingRule:   meta.VersionGA,
				TargetHttpProxy:  meta.VersionGA,
				TargetHttpsProxy: meta.VersionGA,
				SslCertificate:   meta.VersionGA,
				BackendService:   meta.VersionGA,
			},
		},
	}

	// Override features with fakes
	resourceToVersionMap = fakeResourceToVersionMap
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {

			for resource, expectedVersion := range tc.versionMap {
				result := versionFromFeatures(tc.features, resource)
				if result != expectedVersion {
					t.Fatalf("want %s, got %s", expectedVersion, result)
				}
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
