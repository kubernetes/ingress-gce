/*
Copyright 2017 The Kubernetes Authors.

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

// Package meta contains a concise description of the types used by the cache
// library. This is a separate package as it is imported by the code generation
// routines to determines which types should be handled.
//
// AllTypes is the master list of types and their attributes.
package meta

// Version is the compute API version.
type Version string

const (
	// VersionGA is the API version in compute.v1.
	VersionGA Version = "ga"
	// VersionAlpha is the API version in computer.v0.alpha.
	VersionAlpha Version = "alpha"
	// VersionBeta is the API version in computer.v0.beta.
	VersionBeta Version = "beta"
)

// AllVersions is a list of all versions of the GCE API.
var AllVersions = []Version{
	VersionGA,
	VersionAlpha,
	VersionBeta,
}

// CamelCase returns the go-legal CamelCase representation of the version.
func (v Version) CamelCase() string {
	switch v {
	case VersionGA:
		return "GA"
	case VersionAlpha:
		return "Alpha"
	case VersionBeta:
		return "Beta"
	}
	return "Invalid"
}

func toVersionMap(versions ...Version) map[Version]bool {
	ret := map[Version]bool{}
	for _, v := range versions {
		ret[v] = true
	}
	return ret
}

var gaOnly = toVersionMap(VersionGA)

// keyConstraint encodes what kind of keys are possible for a given
// resource.
type keyConstraint struct {
	Global, Regional, Zonal bool
}

// TypeInfo is the metadata concerning a GCE resource type.
type TypeInfo struct {
	Name          string
	Versions      map[Version]bool
	KeyConstraint keyConstraint
}

// Adapter is the name of the golang type for the adapter structure.
func (ti *TypeInfo) Adapter(version Version) string {
	switch version {
	case VersionGA:
		return ti.Name
	case VersionAlpha:
		return "Alpha" + ti.Name
	case VersionBeta:
		return "Beta" + ti.Name
	}
	return "Invalid"
}

// AllTypes defines properties of the GCE objects that should be
// supported.
var AllTypes = []*TypeInfo{
	&TypeInfo{
		Name:          "Address",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Global: true, Regional: true},
	},
	&TypeInfo{
		Name:          "BackendService",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Global: true},
	},
	&TypeInfo{
		Name:          "Firewall",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Global: true},
	},
	&TypeInfo{
		Name:          "ForwardingRule",
		Versions:      toVersionMap(VersionGA, VersionAlpha, VersionBeta),
		KeyConstraint: keyConstraint{Global: true, Regional: true},
	},
	&TypeInfo{
		Name:          "HealthCheck",
		Versions:      toVersionMap(VersionGA, VersionAlpha, VersionBeta),
		KeyConstraint: keyConstraint{Global: true},
	},
	&TypeInfo{
		Name:          "InstanceGroup",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Regional: true, Zonal: true},
	},
	&TypeInfo{
		Name:          "SslCertificate",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Global: true},
	},
	&TypeInfo{
		Name:          "TargetHttpProxy",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Global: true},
	},
	&TypeInfo{
		Name:          "TargetHttpsProxy",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Global: true},
	},
	&TypeInfo{
		Name:          "TargetPool",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Regional: true},
	},
	&TypeInfo{
		Name:          "UrlMap",
		Versions:      gaOnly,
		KeyConstraint: keyConstraint{Global: true},
	},
}

// AllTypesMap is a map of AllTypes indexed by Name.
var AllTypesMap = map[string]*TypeInfo{}

func init() {
	for _, t := range AllTypes {
		AllTypesMap[t.Name] = t
	}
}
