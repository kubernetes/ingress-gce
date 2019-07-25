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

// TODO: (shance) this file should ideally be combined with backends/features
package features

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	// versionToFeatures stores the mapping from the required API
	// version to feature names.
	// TODO: (shance) Add L7-ILB here
	versionToFeatures = map[meta.Version][]string{}
	// scopeToFeatures stores the mapping from the required resource type
	// to feature names
	// TODO: (shance) add L7-ILB here
	// Only add features that have a hard scope requirement
	// TODO: (shance) refactor scope to be per-resource
	scopeToFeatures = map[meta.KeyType][]string{}
)

// featuresFromIngress returns the features enabled by an ingress
func featuresFromIngress(ing *v1beta1.Ingress) []string {
	result := []string{}
	return result
}

// versionFromFeatures returns the meta.Version required for a list of features
func versionFromFeatures(features []string) meta.Version {
	fs := sets.NewString(features...)
	if fs.HasAny(versionToFeatures[meta.VersionAlpha]...) {
		return meta.VersionAlpha
	}
	if fs.HasAny(versionToFeatures[meta.VersionBeta]...) {
		return meta.VersionBeta
	}
	return meta.VersionGA
}

// TODO: (shance) refactor scope to be per-resource
// scopeFromFeatures returns the required scope for a list of features
func scopeFromFeatures(features []string) meta.KeyType {
	fs := sets.NewString(features...)
	if fs.HasAny(scopeToFeatures[meta.Zonal]...) {
		return meta.Zonal
	}
	if fs.HasAny(scopeToFeatures[meta.Regional]...) {
		return meta.Regional
	}
	return meta.Global
}

// TODO: (shance) refactor scope to be per-resource
// ScopeFromIngress returns the required scope of features for an Ingress
func ScopeFromIngress(ing *v1beta1.Ingress) meta.KeyType {
	return scopeFromFeatures(featuresFromIngress(ing))
}

// VersionFromIngress returns the required meta.Version of features for an Ingress
func VersionFromIngress(ing *v1beta1.Ingress) meta.Version {
	return versionFromFeatures(featuresFromIngress(ing))
}
