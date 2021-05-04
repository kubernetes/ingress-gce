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
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	FeatureL7ILB = "L7ILB"
)

var GAResourceVersions = NewResourceVersions()

// ResourceVersions allows you to define all the versions required for each resource
// for a feature.
type ResourceVersions struct {
	UrlMap           meta.Version
	ForwardingRule   meta.Version
	TargetHttpProxy  meta.Version
	TargetHttpsProxy meta.Version
	SslCertificate   meta.Version

	// This is *only* used for backend annotations
	// TODO(shance): find a way to remove these
	BackendService meta.Version
	HealthCheck    meta.Version
}

var (
	// versionToFeatures stores the mapping from the resource to the required API
	// version to feature names.  This is necessary since a feature could
	// require using different versions for each resource.
	// must not be nil
	featureToVersions = map[string]*ResourceVersions{
		FeatureL7ILB: &l7IlbVersions,
	}

	// scopeToFeatures stores the mapping from the required resource type
	// to feature names
	// Only add features that have a hard scope requirement
	// TODO: (shance) refactor scope to be per-resource
	scopeToFeatures = map[meta.KeyType][]string{
		meta.Regional: {FeatureL7ILB},
	}

	// All of these fields must be filled in to allow L7ILBVersions() to work
	// TODO(shance) Remove this entirely
	l7IlbVersions = *NewResourceVersions()
)

func NewResourceVersions() *ResourceVersions {
	return &ResourceVersions{
		meta.VersionGA,
		meta.VersionGA,
		meta.VersionGA,
		meta.VersionGA,
		meta.VersionGA,
		meta.VersionGA,
		meta.VersionGA,
	}
}

func (r *ResourceVersions) merge(other *ResourceVersions) *ResourceVersions {
	return &ResourceVersions{
		UrlMap:           mergeVersions(r.UrlMap, other.UrlMap),
		ForwardingRule:   mergeVersions(r.ForwardingRule, other.ForwardingRule),
		TargetHttpProxy:  mergeVersions(r.TargetHttpProxy, other.TargetHttpProxy),
		TargetHttpsProxy: mergeVersions(r.TargetHttpsProxy, other.TargetHttpsProxy),
		SslCertificate:   mergeVersions(r.SslCertificate, other.SslCertificate),
		BackendService:   mergeVersions(r.BackendService, other.BackendService),
		HealthCheck:      mergeVersions(r.HealthCheck, other.HealthCheck),
	}
}

// featuresFromIngress returns the features enabled by an ingress
func featuresFromIngress(ing *v1.Ingress) []string {
	var result []string
	if utils.IsGCEL7ILBIngress(ing) {
		result = append(result, FeatureL7ILB)
	}
	return result
}

// versionFromFeatures returns the meta.Version required for a list of features
func versionsFromFeatures(features []string) *ResourceVersions {
	result := NewResourceVersions()

	for _, feature := range features {
		versions := featureToVersions[feature]
		result = result.merge(versions)
	}

	return result
}

func versionOrdinal(v meta.Version) int {
	switch v {
	case meta.VersionAlpha:
		return 2
	case meta.VersionBeta:
		return 1
	case "":
		return -1
	}

	return 0
}

// mergeVersions returns the newer API (e.g. mergeVersions(alpha, GA) = alpha)
func mergeVersions(a, b meta.Version) meta.Version {
	if versionOrdinal(a) < versionOrdinal(b) {
		return b
	}

	return a
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
func ScopeFromIngress(ing *v1.Ingress) meta.KeyType {
	return scopeFromFeatures(featuresFromIngress(ing))
}

// VersionsFromIngress returns a ResourceVersions struct containing all of the resources per version
func VersionsFromIngress(ing *v1.Ingress) *ResourceVersions {
	return versionsFromFeatures(featuresFromIngress(ing))
}
