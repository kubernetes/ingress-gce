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

package composite

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/legacy-cloud-providers/gce"
)

// CreateKey() is a helper function for creating a meta.Key when interacting with the
// composite functions.  For regional scopes, this function looks up
// the region of the gceCloud object.  You should use this wherever possible to avoid
// creating a resource in the wrong region or creating a global resource accidentally.
// TODO: (shance) implement zonal
func CreateKey(gceCloud *gce.Cloud, name string, scope meta.KeyType) (*meta.Key, error) {
	switch scope {
	case meta.Regional:
		region := gceCloud.Region()
		if region == "" {
			return nil, fmt.Errorf("error getting region")
		}
		return meta.RegionalKey(name, region), nil
	case meta.Global:
		return meta.GlobalKey(name), nil
	}

	return nil, fmt.Errorf("invalid resource type: %s", scope)
}

// TODO: (shance) generate this
// TODO: (shance) populate scope
// TODO: (shance) figure out a more accurate way to obtain version
// ListAllUrlMaps() merges all configured List() calls into one list of composite UrlMaps
// This function combines both global and regional resources into one slice
// so users must use ScopeFromSelfLink() to determine the correct scope
// before issuing an API call.
func ListAllBackendServices(gceCloud *gce.Cloud) ([]*BackendService, error) {
	resultMap := map[string]*BackendService{}
	key1, err := CreateKey(gceCloud, "", meta.Global)
	if err != nil {
		return nil, err
	}
	key2, err := CreateKey(gceCloud, "", meta.Regional)
	if err != nil {
		return nil, err
	}

	// List ga-global and regional-alpha
	versions := []meta.Version{meta.VersionGA, meta.VersionAlpha}
	keys := []*meta.Key{key1, key2}

	for i := range versions {
		list, err := ListBackendServices(gceCloud, keys[i], versions[i])
		if err != nil {
			return nil, fmt.Errorf("error listing all urlmaps: %v", err)
		}
		for _, bs := range list {
			resultMap[bs.SelfLink] = bs
		}
	}

	// Convert map to slice
	result := []*BackendService{}
	for _, bs := range resultMap {
		result = append(result, bs)
	}
	return result, nil
}

// IsRegionalUrlMap() returns if the url map is regional
func IsRegionalUrlMap(um *UrlMap) (bool, error) {
	if um != nil {
		return IsRegionalResource(um.SelfLink)
	}
	return false, nil
}

// IsRegionalResource() returns true if the resource URL is regional
func IsRegionalResource(selfLink string) (bool, error) {
	scope, err := ScopeFromSelfLink(selfLink)
	if err != nil {
		return false, err
	}
	return scope == meta.Regional, nil
}

func ScopeFromSelfLink(selfLink string) (meta.KeyType, error) {
	resourceID, err := cloud.ParseResourceURL(selfLink)
	if err != nil {
		return meta.KeyType(""), fmt.Errorf("error parsing self-link %s: %v", selfLink, err)
	}
	return resourceID.Key.Type(), nil
}

// compareFields verifies that two fields in a struct have the same relevant metadata.
// Note: This comparison ignores field offset, index, and pkg path, all of which don't matter.
func compareFields(s1, s2 reflect.StructField) error {
	if s1.Name != s2.Name {
		return fmt.Errorf("field %s name = %q, want %q", s1.Name, s1.Name, s2.Name)
	}
	if s1.Tag != s2.Tag {
		return fmt.Errorf("field %s tag = %q, want %q", s1.Name, s1.Tag, s2.Tag)
	}
	if s1.Type.Name() != s2.Type.Name() {
		return fmt.Errorf("field %s type = %q, want %q", s1.Name, s1.Type.Name(), s2.Type.Name())
	}
	return nil
}

// typeEquality is a generic function that checks type equality.
func typeEquality(t1, t2 reflect.Type, all bool) error {
	t1Fields, t2Fields := make(map[string]bool), make(map[string]bool)
	for i := 0; i < t1.NumField(); i++ {
		t1Fields[t1.Field(i).Name] = true
	}
	for i := 0; i < t2.NumField(); i++ {
		t2Fields[t2.Field(i).Name] = true
	}

	// Only compare all fields if 'all' is set to true
	// If this is set to false, it effectively only checks that all fields
	// in t1 are present in t2
	if all {
		if !reflect.DeepEqual(t1Fields, t2Fields) {
			return fmt.Errorf("type = %+v, want %+v", t1Fields, t2Fields)
		}
	}

	for n := range t1Fields {
		f1, _ := t1.FieldByName(n)
		f2, _ := t2.FieldByName(n)
		if err := compareFields(f1, f2); err != nil {
			return err
		}
	}
	return nil
}

func copyViaJSON(dest interface{}, src interface{}) error {
	var err error
	bytes, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes, dest)
}
