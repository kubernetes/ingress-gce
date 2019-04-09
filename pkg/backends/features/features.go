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
	"sort"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// FeatureHTTP2 defines the feature name of HTTP2.
	FeatureHTTP2 = "HTTP2"
	// FeatureSecurityPolicy defines the feature name of SecurityPolicy.
	FeatureSecurityPolicy = "SecurityPolicy"
	// FeatureNEG defines the feature name of NEG.
	FeatureNEG = "NEG"
)

var (
	// versionToFeatures stores the mapping from the required API
	// version to feature names.
	versionToFeatures = map[meta.Version][]string{
		meta.VersionAlpha: []string{},
		meta.VersionBeta:  []string{FeatureSecurityPolicy, FeatureNEG, FeatureHTTP2},
	}
)

// SetDescription sets the XFeatures field for the given Description.
func SetDescription(desc *utils.Description, sp *utils.ServicePort) {
	desc.XFeatures = featuresFromServicePort(sp)
}

// featuresFromServicePort returns a list of features used by the
// given ServicePort that required non-GA API.
func featuresFromServicePort(sp *utils.ServicePort) []string {
	features := []string{}
	if sp.Protocol == annotations.ProtocolHTTP2 {
		features = append(features, FeatureHTTP2)
	}
	if sp.BackendConfig != nil && sp.BackendConfig.Spec.SecurityPolicy != nil {
		features = append(features, FeatureSecurityPolicy)
	}
	if sp.NEGEnabled {
		features = append(features, FeatureNEG)
	}
	// Keep feature names sorted to be consistent.
	sort.Strings(features)
	return features
}

// VersionFromServicePort returns the meta.Version for the backend that this ServicePort is
// associated with.
func VersionFromServicePort(sp *utils.ServicePort) meta.Version {
	return VersionFromFeatures(featuresFromServicePort(sp))
}

// VersionFromDescription returns the meta.Version required for the given
// description.
func VersionFromDescription(desc string) meta.Version {
	return VersionFromFeatures(utils.DescriptionFromString(desc).XFeatures)
}

// VersionFromFeatures returns the meta.Version required for the given
// list of features.
func VersionFromFeatures(features []string) meta.Version {
	fs := sets.NewString(features...)
	if fs.HasAny(versionToFeatures[meta.VersionAlpha]...) {
		return meta.VersionAlpha
	}
	if fs.HasAny(versionToFeatures[meta.VersionBeta]...) {
		return meta.VersionBeta
	}
	return meta.VersionGA
}

var (
	versionMap = map[meta.Version]int{
		meta.VersionAlpha: 0,
		meta.VersionBeta:  1,
		meta.VersionGA:    2,
	}
)

// IsLowerVersion returns if v1 is a lower version than v2.
func IsLowerVersion(v1, v2 meta.Version) bool {
	return versionMap[v1] < versionMap[v2]
}
