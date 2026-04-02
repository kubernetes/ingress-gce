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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/utils"
)

var (
	fakeSvcPortID = utils.ServicePortID{
		Service: types.NamespacedName{
			Namespace: "testNS",
			Name:      "testName",
		},
		Port: v1.ServiceBackendPort{Number: 80},
	}

	svcPortDefault = utils.ServicePort{
		ID: fakeSvcPortID,
	}

	svcPortWithHTTP2 = utils.ServicePort{
		ID:       fakeSvcPortID,
		Protocol: annotations.ProtocolHTTP2,
	}

	svcPortWithSecurityPolicy = utils.ServicePort{
		ID: fakeSvcPortID,
		BackendConfig: &backendconfigv1.BackendConfig{
			Spec: backendconfigv1.BackendConfigSpec{
				SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
					Name: "policy-test",
				},
			},
		},
	}

	svcPortWithHTTP2SecurityPolicy = utils.ServicePort{
		ID:       fakeSvcPortID,
		Protocol: annotations.ProtocolHTTP2,
		BackendConfig: &backendconfigv1.BackendConfig{
			Spec: backendconfigv1.BackendConfigSpec{
				SecurityPolicy: &backendconfigv1.SecurityPolicyConfig{
					Name: "policy-test",
				},
			},
		},
	}

	svcPortWithNEG = utils.ServicePort{
		ID:         fakeSvcPortID,
		NEGEnabled: true,
	}
)

func TestFeaturesFromServicePort(t *testing.T) {
	testCases := []struct {
		desc             string
		svcPort          utils.ServicePort
		expectedFeatures []string
	}{
		{
			desc:             "no feature",
			svcPort:          svcPortDefault,
			expectedFeatures: []string{},
		},
		{
			desc:             "HTTP2",
			svcPort:          svcPortWithHTTP2,
			expectedFeatures: []string{"HTTP2"},
		},
		{
			desc:             "SecurityPolicy",
			svcPort:          svcPortWithSecurityPolicy,
			expectedFeatures: []string{"SecurityPolicy"},
		},
		{
			desc:             "NEG",
			svcPort:          svcPortWithNEG,
			expectedFeatures: []string{"NEG"},
		},
		{
			desc:             "HTTP2 + SecurityPolicy",
			svcPort:          svcPortWithHTTP2SecurityPolicy,
			expectedFeatures: []string{"HTTP2", "SecurityPolicy"},
		},
	}

	for _, tc := range testCases {
		features := featuresFromServicePort(&tc.svcPort)
		if !sets.NewString(features...).Equal(sets.NewString(tc.expectedFeatures...)) {
			t.Errorf("%s: featuresFromServicePort()=%s, want %s", tc.desc, features, tc.expectedFeatures)
		}
	}
}

func TestVersionFromFeatures(t *testing.T) {
	testCases := []struct {
		desc            string
		features        []string
		expectedVersion meta.Version
	}{
		{
			desc:            "no feature",
			features:        []string{},
			expectedVersion: meta.VersionGA,
		},
		{
			desc:            "HTTP2",
			features:        []string{FeatureHTTP2},
			expectedVersion: meta.VersionBeta,
		},
		{
			desc:            "NEG + SecurityPolicy",
			features:        []string{FeatureNEG, FeatureSecurityPolicy},
			expectedVersion: meta.VersionGA,
		},
		{
			desc:            "HTTP2 + SecurityPolicy",
			features:        []string{FeatureHTTP2, FeatureSecurityPolicy},
			expectedVersion: meta.VersionBeta,
		},
		{
			desc:            "unknown feature",
			features:        []string{"whatisthis"},
			expectedVersion: meta.VersionGA,
		},
	}

	for _, tc := range testCases {
		if v := VersionFromFeatures(tc.features); v != tc.expectedVersion {
			t.Errorf("%s: VersionFromFeatures(%v)=%s, want %s", tc.desc, tc.features, v, tc.expectedVersion)
		}
	}
}

func TestVersionFromDescription(t *testing.T) {
	testCases := []struct {
		desc               string
		backendServiceDesc string
		expectedVersion    meta.Version
	}{
		{
			desc:               "invalid format",
			backendServiceDesc: "invalid",
			expectedVersion:    meta.VersionGA,
		},
		{
			desc:               "no feature",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port"}`,
			expectedVersion:    meta.VersionGA,
		},
		{
			desc:               "HTTP2",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["HTTP2"]}`,
			expectedVersion:    meta.VersionBeta,
		},
		{
			desc:               "SecurityPolicy",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["SecurityPolicy"]}`,
			expectedVersion:    meta.VersionGA,
		},
		{
			desc:               "HTTP2 + SecurityPolicy",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["HTTP2","SecurityPolicy"]}`,
			expectedVersion:    meta.VersionBeta,
		},
		{
			desc:               "HTTP2 + unknown",
			backendServiceDesc: `{"kubernetes.io/service-name":"my-service","kubernetes.io/service-port":"my-port","x-features":["HTTP2","whatisthis"]}`,
			expectedVersion:    meta.VersionBeta,
		},
	}

	for _, tc := range testCases {
		if v := VersionFromDescription(tc.backendServiceDesc); v != tc.expectedVersion {
			t.Errorf("%s: VersionFromDescription(%s)=%s, want %s", tc.desc, tc.backendServiceDesc, v, tc.expectedVersion)
		}
	}
}

func TestVersionFromServicePort(t *testing.T) {
	testCases := []struct {
		desc            string
		svcPort         utils.ServicePort
		expectedVersion meta.Version
	}{
		{
			desc:            "default",
			svcPort:         svcPortDefault,
			expectedVersion: meta.VersionGA,
		},
		{
			desc:            "enabled http2",
			svcPort:         svcPortWithHTTP2,
			expectedVersion: meta.VersionBeta,
		},
		{
			desc:            "enabled security policy",
			svcPort:         svcPortWithSecurityPolicy,
			expectedVersion: meta.VersionGA,
		},
		{
			desc:            "enabled http2 + security policy",
			svcPort:         svcPortWithHTTP2SecurityPolicy,
			expectedVersion: meta.VersionBeta,
		},
	}

	for _, tc := range testCases {
		if v := VersionFromServicePort(&tc.svcPort); v != tc.expectedVersion {
			t.Errorf("%s: VersionFromServicePort()=%s, want %s", tc.desc, v, tc.expectedVersion)
		}
	}
}

func TestIsLowerVersion(t *testing.T) {
	testCases := []struct {
		desc   string
		v1     meta.Version
		v2     meta.Version
		expect bool
	}{
		{
			desc:   "ga is not lower than ga",
			v1:     meta.VersionGA,
			v2:     meta.VersionGA,
			expect: false,
		},
		{
			desc:   "beta is lower than ga",
			v1:     meta.VersionBeta,
			v2:     meta.VersionGA,
			expect: true,
		},
		{
			desc:   "alpha is lower than ga",
			v1:     meta.VersionAlpha,
			v2:     meta.VersionGA,
			expect: true,
		},
		{
			desc:   "beta is not lower than beta",
			v1:     meta.VersionBeta,
			v2:     meta.VersionBeta,
			expect: false,
		},
		{
			desc:   "alpha is lower than beta",
			v1:     meta.VersionAlpha,
			v2:     meta.VersionBeta,
			expect: true,
		},
		{
			desc:   "alpha is not lower than alpha",
			v1:     meta.VersionAlpha,
			v2:     meta.VersionAlpha,
			expect: false,
		},
	}

	for _, tc := range testCases {
		if res := IsLowerVersion(tc.v1, tc.v2); res != tc.expect {
			t.Errorf("%s: IsLowerVersion(%s, %s)=%t, want %t", tc.desc, tc.v1, tc.v2, res, tc.expect)
		}
	}
}
