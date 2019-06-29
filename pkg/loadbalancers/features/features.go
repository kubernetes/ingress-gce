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
	"k8s.io/ingress-gce/pkg/utils"
)

type LBResource string

const (
	FeatureL7ILB string = "L7ILB"

	// L7 Resources
	UrlMap           LBResource = "UrlMap"
	ForwardingRule   LBResource = "ForwardingRule"
	TargetHttpProxy  LBResource = "TargetHttpProxy"
	TargetHttpsProxy LBResource = "TargetHttpsProxy"
	SslCertificate   LBResource = "SslCertificate"

	// This is *only* used for backend annotations
	// TODO(shance): find a way to remove these
	BackendService LBResource = "BackendService"
	HealthCheck    LBResource = "HealthCheck"
)

var (
	// versionToFeatures stores the mapping from the resource to the required API
	// version to feature names.  This is necessary since a feature could
	// require using different versions for each resource.
	resourceToVersionMap = map[LBResource]map[meta.Version][]string{
		UrlMap: {
			meta.VersionAlpha: {FeatureL7ILB},
		},
		ForwardingRule: {
			meta.VersionAlpha: {FeatureL7ILB},
		},
		TargetHttpProxy: {
			meta.VersionAlpha: {FeatureL7ILB},
		},
		TargetHttpsProxy: {
			meta.VersionAlpha: {FeatureL7ILB},
		},
		SslCertificate: {
			meta.VersionAlpha: {FeatureL7ILB},
		},
		// *Only used for backend annotations*
		BackendService: {
			meta.VersionAlpha: {FeatureL7ILB},
		},
		// TODO(shance): find a way to remove this
		HealthCheck: {
			meta.VersionAlpha: {FeatureL7ILB},
		},
	}

	// scopeToFeatures stores the mapping from the required resource type
	// to feature names
	// Only add features that have a hard scope requirement
	// TODO: (shance) refactor scope to be per-resource
	scopeToFeatures = map[meta.KeyType][]string{
		meta.Regional: []string{FeatureL7ILB},
	}
)

// featuresFromIngress returns the features enabled by an ingress
func featuresFromIngress(ing *v1beta1.Ingress) []string {
	var result []string
	if utils.IsGCEL7ILBIngress(ing) {
		result = append(result, FeatureL7ILB)
	}
	return result
}

// versionFromFeatures returns the meta.Version required for a list of features
func versionFromFeatures(strings []string, resource LBResource) meta.Version {
	fs := sets.NewString(strings...)
	if fs.HasAny(resourceToVersionMap[resource][meta.VersionAlpha]...) {
		return meta.VersionAlpha
	}
	if fs.HasAny(resourceToVersionMap[resource][meta.VersionBeta]...) {
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
func VersionFromIngressForResource(ing *v1beta1.Ingress, resource LBResource) meta.Version {
	return versionFromFeatures(featuresFromIngress(ing), resource)
}
