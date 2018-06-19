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

// Package cache contains the implementation of a caching layer for GCE cloud
// objects. This data structure supports tracking a set of typed GCE objects,
// querying for objects matching a given predicate and automatic conversion
// between API versions (e.g. v1, alpha and beta).
//
// Usage
//
//   gce := NewGCE()
//   key := RegionalKey("my-forwarding-rule")
//   // Add a forwarding rule to the cache.
//   gce.AlphaForwardingRule.Put(key, &Metadata{...}, &compute.Forwarding{...})
//   // Get the forwarding rule back, coverting to v1 API object.
//   m, fwdRule, err := gce.ForwardingRule.Get(key, ConvertVersion)
//   // List only v1 forwarding rules.
//   keys, err := gce.ForwardingRule.List()
//   // List only all forwarding rules, converting as needed.
//   keys, err := gce.ForwardingRule.ListAllVersions()
//   // Remove the object from the cache.
//   gce.Flush(key)
package cache

import (
	"errors"
	"time"

	"k8s.io/ingress-gce/pkg/cache/meta"
)

// GCE is a cache of object state from the cloud.
type GCE struct {
	adapter
}

// Options are passed to Get(), ListByFilter, FlushByFilter.
type Options int

const (
	// NoOptions specifies the default behavior for Get() and ListByFilter().
	NoOptions = 0
	// ConvertVersion will make Get() and ListByFilter() convert objects between
	// API versions if possible.
	ConvertVersion Options = 1 << iota
	// MetadataOnly makes ListByFilter() operate only on metadata. Adding this
	// option will list all object versions.
	MetadataOnly Options = 1 << iota
)

func (o Options) convertVersion() bool {
	return (o & ConvertVersion) != 0
}

func (o Options) metadataOnly() bool {
	return (o & MetadataOnly) != 0
}

// Metadata is the extra data kept about the cached object.
type Metadata struct {
	// Sync is the last time the object was synchronized with the cloud.
	Sync time.Time
	// Expiry is the deadline for object validity.
	Expiry time.Time
	// API is the API version of the object.
	Version meta.Version
}

// NewGCE returns a new cache for GCE objects.
func NewGCE() *GCE {
	ret := &GCE{}
	initAdapter(&ret.adapter, newStore)
	return ret
}

var (
	// ErrNotFound is returned from Get() when there is no matching
	// object for the given Key.
	ErrNotFound = errors.New("not found")
	// ErrNoConversion is returned when a version conversion is requested
	// but none is available for the given type.
	ErrNoConversion = errors.New("no conversion possible")
	// ErrInvalidKeyForType is returned when a Key for a resource does not match
	// what is valid for the given type.
	ErrInvalidKeyForType = errors.New("invalid key for resource type")
)
