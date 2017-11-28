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

package cache

import (
	"reflect"
	"testing"

	alpha "google.golang.org/api/compute/v0.alpha"
	beta "google.golang.org/api/compute/v0.beta"
	ga "google.golang.org/api/compute/v1"
)

func TestGCE(t *testing.T) {
	t.Parallel()

	gce := NewGCE()

	keyGA := GlobalKey("key-ga")
	objGA := &ga.ForwardingRule{Name: keyGA.Name}

	keyAlpha := GlobalKey("key-alpha")
	objAlpha := &alpha.ForwardingRule{Name: keyAlpha.Name}

	keyBeta := GlobalKey("key-beta")
	objBeta := &beta.ForwardingRule{Name: keyBeta.Name}

	allKeys := []Key{
		keyGA,
		keyAlpha,
		keyBeta,
	}

	// Empty cache
	for _, key := range allKeys {
		if meta, obj, err := gce.ForwardingRule.Get(key, 0); err != ErrNotFound {
			t.Errorf("gce.ForwardingRule.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
		}
		// Flush non-existant key.
		gce.ForwardingRule.Flush(key)
	}
	keys, err := gce.ForwardingRule.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.ForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object of each valid version into the cache.
	if err := gce.ForwardingRule.Put(keyGA, &Metadata{}, objGA); err != nil {
		t.Errorf("gce.ForwardingRule.Put(%v, &Metadata{}, %v) = %v, want nil", keyGA, objGA, err)
	}
	if err := gce.AlphaForwardingRule.Put(keyAlpha, &Metadata{}, objAlpha); err != nil {
		t.Errorf("gce.AlphaForwardingRule.Put(%v, &Metadata{}, %v) = %v, want nil", keyAlpha, objAlpha, err)
	}
	if err := gce.BetaForwardingRule.Put(keyBeta, &Metadata{}, objBeta); err != nil {
		t.Errorf("gce.BetaForwardingRule.Put(%v, &Metadata{}, %v) = %v, want nil", keyBeta, objBeta, err)
	}

	// Get keys. Types that do not match will return only the metadata unless ConvertVersion is specified.
	for _, key := range allKeys {
		meta, obj, err := gce.ForwardingRule.Get(key, 0)
		if err != nil {
			t.Errorf("gce.ForwardingRule.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, meta, obj, err)
		}
		if key == keyGA {
			if objGA != obj {
				t.Errorf("gce.ForwardingRule.Get(%v, 0) = %v, %v, %v, want non-nil, %v, nil", key, meta, objGA, err, objGA)
			}
		} else {
			if meta == nil || obj != nil {
				t.Errorf("gce.ForwardingRule.Get(%v, 0) = %v, %v, %v, want non-nil, nil, nil", key, meta, obj, err)
			}
		}
		meta, obj, err = gce.ForwardingRule.Get(key, ConvertVersion)
		if err != nil || meta == nil || obj == nil {
			t.Errorf("gce.ForwardingRule.Get(%v, ConvertVersion) = %v, %v, %v, want non-nil, non-nil, nil", key, meta, obj, err)
		}
	}

	// List will only return the given version.
	keys, err = gce.ForwardingRule.List()
	if err != nil {
		t.Errorf("gce.ForwardingRule.List() = %v, %v; want _, nil", keys, err)
	}
	want := []Key{keyGA}
	if !reflect.DeepEqual(KeysToMap(keys...), KeysToMap(want...)) {
		t.Errorf("gce.ForwardingRule.List() = %v, nil; want %v, nil", keys, want)
	}

	keys, err = gce.ForwardingRule.ListAllVersions()
	if err != nil {
		t.Errorf("gce.ForwardingRule.List() = %v, %v; want _, nil", keys, err)
	}
	want = allKeys
	if !reflect.DeepEqual(KeysToMap(keys...), KeysToMap(want...)) {
		t.Errorf("gce.ForwardingRule.List() = %v, nil; want %v, nil", keys, want)
	}

	keys, err = gce.ForwardingRule.ListByFilter(
		func(k Key, m *Metadata, obj *ga.ForwardingRule) (bool, error) {
			return k == keyGA, nil
		}, 0)
	want = []Key{keyGA}
	if !reflect.DeepEqual(KeysToMap(keys...), KeysToMap(want...)) {
		t.Errorf("gce.ForwardingRule.ListByFilter(<k == keyGA>) = %v, nil; want %v, nil", keys, want)
	}

	gce.ForwardingRule.Flush(keyGA)

	err = gce.ForwardingRule.FlushByFilter(
		func(k Key, m *Metadata, obj *ga.ForwardingRule) (bool, error) {
			return true, nil
		}, 0)
	if err != nil {
		t.Errorf("gce.ForwardingRule.FlushByFilter(<all>) = %v, want nil", err)
	}

	meta, obj, err := gce.ForwardingRule.Get(keyGA, 0)
	if err != ErrNotFound {
		t.Errorf("gce.ForwardingRule.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", keyGA, meta, obj, err)
		return
	}
}
