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

// This file was generated with "go run gen/main.go -unitTest > gen_test.go"

package cache

import (
	"errors"
	"testing"

	alpha "google.golang.org/api/compute/v0.alpha"
	beta "google.golang.org/api/compute/v0.beta"
	ga "google.golang.org/api/compute/v1"
)

func TestGCEAddress(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.Address{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.Address.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.Address.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.Address.Flush(key)

	// List should return no objects.
	keys, err := gce.Address.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.Address.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.Address.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.Address.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.Address.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.Address.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.Address.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.Address.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.Address.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.Address.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.Address.ListByFilter(func(Key, *Metadata, *ga.Address) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.Address.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.Address.ListByFilter(func(k Key, m *Metadata, o *ga.Address) (bool, error) {
		if o != nil {
			t.Errorf("gce.Address.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.Address.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.Address.FlushByFilter(func(Key, *Metadata, *ga.Address) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.Address.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.Address.FlushByFilter(func(Key, *Metadata, *ga.Address) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.Address.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEBackendService(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.BackendService{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.BackendService.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.BackendService.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.BackendService.Flush(key)

	// List should return no objects.
	keys, err := gce.BackendService.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.BackendService.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.BackendService.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.BackendService.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.BackendService.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.BackendService.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.BackendService.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.BackendService.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.BackendService.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BackendService.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.BackendService.ListByFilter(func(Key, *Metadata, *ga.BackendService) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BackendService.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.BackendService.ListByFilter(func(k Key, m *Metadata, o *ga.BackendService) (bool, error) {
		if o != nil {
			t.Errorf("gce.BackendService.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BackendService.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.BackendService.FlushByFilter(func(Key, *Metadata, *ga.BackendService) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.BackendService.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.BackendService.FlushByFilter(func(Key, *Metadata, *ga.BackendService) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.BackendService.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEFirewall(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.Firewall{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.Firewall.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.Firewall.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.Firewall.Flush(key)

	// List should return no objects.
	keys, err := gce.Firewall.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.Firewall.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.Firewall.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.Firewall.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.Firewall.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.Firewall.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.Firewall.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.Firewall.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.Firewall.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.Firewall.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.Firewall.ListByFilter(func(Key, *Metadata, *ga.Firewall) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.Firewall.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.Firewall.ListByFilter(func(k Key, m *Metadata, o *ga.Firewall) (bool, error) {
		if o != nil {
			t.Errorf("gce.Firewall.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.Firewall.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.Firewall.FlushByFilter(func(Key, *Metadata, *ga.Firewall) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.Firewall.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.Firewall.FlushByFilter(func(Key, *Metadata, *ga.Firewall) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.Firewall.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEBetaForwardingRule(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &beta.ForwardingRule{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.BetaForwardingRule.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.BetaForwardingRule.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.BetaForwardingRule.Flush(key)

	// List should return no objects.
	keys, err := gce.BetaForwardingRule.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.BetaForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.BetaForwardingRule.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.BetaForwardingRule.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.BetaForwardingRule.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.BetaForwardingRule.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.BetaForwardingRule.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.BetaForwardingRule.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.BetaForwardingRule.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BetaForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.BetaForwardingRule.ListByFilter(func(Key, *Metadata, *beta.ForwardingRule) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BetaForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.BetaForwardingRule.ListByFilter(func(k Key, m *Metadata, o *beta.ForwardingRule) (bool, error) {
		if o != nil {
			t.Errorf("gce.BetaForwardingRule.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BetaForwardingRule.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.BetaForwardingRule.FlushByFilter(func(Key, *Metadata, *beta.ForwardingRule) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.BetaForwardingRule.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.BetaForwardingRule.FlushByFilter(func(Key, *Metadata, *beta.ForwardingRule) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.BetaForwardingRule.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEForwardingRule(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.ForwardingRule{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.ForwardingRule.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.ForwardingRule.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.ForwardingRule.Flush(key)

	// List should return no objects.
	keys, err := gce.ForwardingRule.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.ForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.ForwardingRule.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.ForwardingRule.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.ForwardingRule.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.ForwardingRule.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.ForwardingRule.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.ForwardingRule.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.ForwardingRule.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.ForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.ForwardingRule.ListByFilter(func(Key, *Metadata, *ga.ForwardingRule) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.ForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.ForwardingRule.ListByFilter(func(k Key, m *Metadata, o *ga.ForwardingRule) (bool, error) {
		if o != nil {
			t.Errorf("gce.ForwardingRule.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.ForwardingRule.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.ForwardingRule.FlushByFilter(func(Key, *Metadata, *ga.ForwardingRule) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.ForwardingRule.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.ForwardingRule.FlushByFilter(func(Key, *Metadata, *ga.ForwardingRule) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.ForwardingRule.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEAlphaForwardingRule(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &alpha.ForwardingRule{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.AlphaForwardingRule.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.AlphaForwardingRule.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.AlphaForwardingRule.Flush(key)

	// List should return no objects.
	keys, err := gce.AlphaForwardingRule.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.AlphaForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.AlphaForwardingRule.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.AlphaForwardingRule.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.AlphaForwardingRule.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.AlphaForwardingRule.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.AlphaForwardingRule.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.AlphaForwardingRule.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.AlphaForwardingRule.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.AlphaForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.AlphaForwardingRule.ListByFilter(func(Key, *Metadata, *alpha.ForwardingRule) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.AlphaForwardingRule.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.AlphaForwardingRule.ListByFilter(func(k Key, m *Metadata, o *alpha.ForwardingRule) (bool, error) {
		if o != nil {
			t.Errorf("gce.AlphaForwardingRule.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.AlphaForwardingRule.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.AlphaForwardingRule.FlushByFilter(func(Key, *Metadata, *alpha.ForwardingRule) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.AlphaForwardingRule.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.AlphaForwardingRule.FlushByFilter(func(Key, *Metadata, *alpha.ForwardingRule) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.AlphaForwardingRule.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEBetaHealthCheck(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &beta.HealthCheck{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.BetaHealthCheck.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.BetaHealthCheck.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.BetaHealthCheck.Flush(key)

	// List should return no objects.
	keys, err := gce.BetaHealthCheck.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.BetaHealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.BetaHealthCheck.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.BetaHealthCheck.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.BetaHealthCheck.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.BetaHealthCheck.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.BetaHealthCheck.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.BetaHealthCheck.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.BetaHealthCheck.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BetaHealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.BetaHealthCheck.ListByFilter(func(Key, *Metadata, *beta.HealthCheck) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BetaHealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.BetaHealthCheck.ListByFilter(func(k Key, m *Metadata, o *beta.HealthCheck) (bool, error) {
		if o != nil {
			t.Errorf("gce.BetaHealthCheck.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.BetaHealthCheck.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.BetaHealthCheck.FlushByFilter(func(Key, *Metadata, *beta.HealthCheck) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.BetaHealthCheck.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.BetaHealthCheck.FlushByFilter(func(Key, *Metadata, *beta.HealthCheck) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.BetaHealthCheck.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEHealthCheck(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.HealthCheck{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.HealthCheck.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.HealthCheck.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.HealthCheck.Flush(key)

	// List should return no objects.
	keys, err := gce.HealthCheck.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.HealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.HealthCheck.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.HealthCheck.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.HealthCheck.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.HealthCheck.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.HealthCheck.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.HealthCheck.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.HealthCheck.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.HealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.HealthCheck.ListByFilter(func(Key, *Metadata, *ga.HealthCheck) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.HealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.HealthCheck.ListByFilter(func(k Key, m *Metadata, o *ga.HealthCheck) (bool, error) {
		if o != nil {
			t.Errorf("gce.HealthCheck.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.HealthCheck.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.HealthCheck.FlushByFilter(func(Key, *Metadata, *ga.HealthCheck) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.HealthCheck.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.HealthCheck.FlushByFilter(func(Key, *Metadata, *ga.HealthCheck) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.HealthCheck.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEAlphaHealthCheck(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &alpha.HealthCheck{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.AlphaHealthCheck.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.AlphaHealthCheck.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.AlphaHealthCheck.Flush(key)

	// List should return no objects.
	keys, err := gce.AlphaHealthCheck.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.AlphaHealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.AlphaHealthCheck.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.AlphaHealthCheck.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.AlphaHealthCheck.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.AlphaHealthCheck.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.AlphaHealthCheck.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.AlphaHealthCheck.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.AlphaHealthCheck.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.AlphaHealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.AlphaHealthCheck.ListByFilter(func(Key, *Metadata, *alpha.HealthCheck) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.AlphaHealthCheck.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.AlphaHealthCheck.ListByFilter(func(k Key, m *Metadata, o *alpha.HealthCheck) (bool, error) {
		if o != nil {
			t.Errorf("gce.AlphaHealthCheck.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.AlphaHealthCheck.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.AlphaHealthCheck.FlushByFilter(func(Key, *Metadata, *alpha.HealthCheck) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.AlphaHealthCheck.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.AlphaHealthCheck.FlushByFilter(func(Key, *Metadata, *alpha.HealthCheck) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.AlphaHealthCheck.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEInstanceGroup(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := RegionalKey("key", "us-central1")
	obj := &ga.InstanceGroup{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.InstanceGroup.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.InstanceGroup.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.InstanceGroup.Flush(key)

	// List should return no objects.
	keys, err := gce.InstanceGroup.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.InstanceGroup.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.InstanceGroup.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.InstanceGroup.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.InstanceGroup.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.InstanceGroup.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.InstanceGroup.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.InstanceGroup.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.InstanceGroup.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.InstanceGroup.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.InstanceGroup.ListByFilter(func(Key, *Metadata, *ga.InstanceGroup) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.InstanceGroup.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.InstanceGroup.ListByFilter(func(k Key, m *Metadata, o *ga.InstanceGroup) (bool, error) {
		if o != nil {
			t.Errorf("gce.InstanceGroup.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.InstanceGroup.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.InstanceGroup.FlushByFilter(func(Key, *Metadata, *ga.InstanceGroup) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.InstanceGroup.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.InstanceGroup.FlushByFilter(func(Key, *Metadata, *ga.InstanceGroup) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.InstanceGroup.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCESslCertificate(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.SslCertificate{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.SslCertificate.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.SslCertificate.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.SslCertificate.Flush(key)

	// List should return no objects.
	keys, err := gce.SslCertificate.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.SslCertificate.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.SslCertificate.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.SslCertificate.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.SslCertificate.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.SslCertificate.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.SslCertificate.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.SslCertificate.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.SslCertificate.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.SslCertificate.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.SslCertificate.ListByFilter(func(Key, *Metadata, *ga.SslCertificate) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.SslCertificate.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.SslCertificate.ListByFilter(func(k Key, m *Metadata, o *ga.SslCertificate) (bool, error) {
		if o != nil {
			t.Errorf("gce.SslCertificate.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.SslCertificate.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.SslCertificate.FlushByFilter(func(Key, *Metadata, *ga.SslCertificate) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.SslCertificate.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.SslCertificate.FlushByFilter(func(Key, *Metadata, *ga.SslCertificate) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.SslCertificate.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCETargetHttpProxy(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.TargetHttpProxy{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.TargetHttpProxy.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.TargetHttpProxy.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.TargetHttpProxy.Flush(key)

	// List should return no objects.
	keys, err := gce.TargetHttpProxy.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.TargetHttpProxy.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.TargetHttpProxy.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.TargetHttpProxy.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.TargetHttpProxy.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.TargetHttpProxy.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.TargetHttpProxy.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.TargetHttpProxy.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.TargetHttpProxy.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetHttpProxy.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.TargetHttpProxy.ListByFilter(func(Key, *Metadata, *ga.TargetHttpProxy) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetHttpProxy.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.TargetHttpProxy.ListByFilter(func(k Key, m *Metadata, o *ga.TargetHttpProxy) (bool, error) {
		if o != nil {
			t.Errorf("gce.TargetHttpProxy.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetHttpProxy.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.TargetHttpProxy.FlushByFilter(func(Key, *Metadata, *ga.TargetHttpProxy) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.TargetHttpProxy.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.TargetHttpProxy.FlushByFilter(func(Key, *Metadata, *ga.TargetHttpProxy) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.TargetHttpProxy.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCETargetHttpsProxy(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.TargetHttpsProxy{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.TargetHttpsProxy.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.TargetHttpsProxy.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.TargetHttpsProxy.Flush(key)

	// List should return no objects.
	keys, err := gce.TargetHttpsProxy.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.TargetHttpsProxy.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.TargetHttpsProxy.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.TargetHttpsProxy.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.TargetHttpsProxy.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.TargetHttpsProxy.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.TargetHttpsProxy.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.TargetHttpsProxy.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.TargetHttpsProxy.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetHttpsProxy.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.TargetHttpsProxy.ListByFilter(func(Key, *Metadata, *ga.TargetHttpsProxy) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetHttpsProxy.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.TargetHttpsProxy.ListByFilter(func(k Key, m *Metadata, o *ga.TargetHttpsProxy) (bool, error) {
		if o != nil {
			t.Errorf("gce.TargetHttpsProxy.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetHttpsProxy.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.TargetHttpsProxy.FlushByFilter(func(Key, *Metadata, *ga.TargetHttpsProxy) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.TargetHttpsProxy.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.TargetHttpsProxy.FlushByFilter(func(Key, *Metadata, *ga.TargetHttpsProxy) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.TargetHttpsProxy.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCETargetPool(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := RegionalKey("key", "us-central1")
	obj := &ga.TargetPool{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.TargetPool.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.TargetPool.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.TargetPool.Flush(key)

	// List should return no objects.
	keys, err := gce.TargetPool.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.TargetPool.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.TargetPool.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.TargetPool.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.TargetPool.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.TargetPool.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.TargetPool.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.TargetPool.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.TargetPool.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetPool.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.TargetPool.ListByFilter(func(Key, *Metadata, *ga.TargetPool) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetPool.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.TargetPool.ListByFilter(func(k Key, m *Metadata, o *ga.TargetPool) (bool, error) {
		if o != nil {
			t.Errorf("gce.TargetPool.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.TargetPool.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.TargetPool.FlushByFilter(func(Key, *Metadata, *ga.TargetPool) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.TargetPool.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.TargetPool.FlushByFilter(func(Key, *Metadata, *ga.TargetPool) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.TargetPool.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEUrlMap(t *testing.T) {
	t.Parallel()
	gce := NewGCE()
	key := GlobalKey("key")
	obj := &ga.UrlMap{Name: key.Name}

	// Get non-existant key.
	if meta, obj, err := gce.UrlMap.Get(key, 0); err != ErrNotFound {
		t.Errorf("gce.UrlMap.Get(%v, 0) = %v, %v, %v, want nil, nil, ErrNotFound", key, meta, obj, err)
	}

	// Flush non-existant key.
	gce.UrlMap.Flush(key)

	// List should return no objects.
	keys, err := gce.UrlMap.List()
	if len(keys) != 0 || err != nil {
		t.Errorf("gce.UrlMap.List() = %v, %v; want [], nil", keys, err)
	}

	// Put an object.
	if err := gce.UrlMap.Put(key, &Metadata{}, obj); err != nil {
		t.Errorf("gce.UrlMap.Put(%v, &Metadata{}, %v) = %v, want nil", key, obj, err)
	}

	// Cannot put nil.
	if err := gce.UrlMap.Put(key, &Metadata{}, nil); err == nil {
		t.Errorf("gce.UrlMap.Put(%v, &Metadata{}, nil) = nil, want error", key)
	}

	// Get the object back.
	m, o, err := gce.UrlMap.Get(key, 0)
	if err != nil || o != obj {
		t.Errorf("gce.UrlMap.Get(%v, 0) = %v, %v, %v, want _, _, nil", key, m, o, err)
	}

	// List.
	keys, err = gce.UrlMap.List()
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.UrlMap.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter.
	keys, err = gce.UrlMap.ListByFilter(func(Key, *Metadata, *ga.UrlMap) (bool, error) {
		return true, nil
	}, 0)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.UrlMap.List() = %v, %v; want [], nil", keys, err)
	}

	// ListByFilter, metadata only.
	keys, err = gce.UrlMap.ListByFilter(func(k Key, m *Metadata, o *ga.UrlMap) (bool, error) {
		if o != nil {
			t.Errorf("gce.UrlMap.ListByFilter(..., MetadataOnly), func called with non-nil obj")
		}
		return true, nil
	}, MetadataOnly)
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Errorf("gce.UrlMap.ListByFilter(0) = %v, %v; want [], nil", keys, err)
	}

	// FlushByFilter, returning error.
	err = gce.UrlMap.FlushByFilter(func(Key, *Metadata, *ga.UrlMap) (bool, error) {
		return false, errors.New("injected error")
	}, 0)
	if err == nil {
		t.Errorf("gce.UrlMap.FlushByFilter(...) = nil, want error")
	}

	// FlushByFilter.
	err = gce.UrlMap.FlushByFilter(func(Key, *Metadata, *ga.UrlMap) (bool, error) {
		return true, nil
	}, 0)
	if err != nil {
		t.Errorf("gce.UrlMap.FlushByFilter(...) = %v, want nil", err)
	}
}

func TestGCEForwardingRuleConversions(t *testing.T) {
	t.Parallel()

	gce := NewGCE()

	keyalpha := GlobalKey("keyalpha")
	objalpha := &alpha.ForwardingRule{Name: keyalpha.Name}
	keybeta := GlobalKey("keybeta")
	objbeta := &beta.ForwardingRule{Name: keybeta.Name}
	keyga := GlobalKey("keyga")
	objga := &ga.ForwardingRule{Name: keyga.Name}

	// Put for different versions.
	if err := gce.BetaForwardingRule.Put(keybeta, &Metadata{}, objbeta); err != nil {
		t.Errorf("gce.BetaForwardingRule.Put(keybeta, &Metadata{}, objbeta) = %v; want nil", err)
	}
	if err := gce.ForwardingRule.Put(keyga, &Metadata{}, objga); err != nil {
		t.Errorf("gce.ForwardingRule.Put(keyga, &Metadata{}, objga) = %v; want nil", err)
	}
	if err := gce.AlphaForwardingRule.Put(keyalpha, &Metadata{}, objalpha); err != nil {
		t.Errorf("gce.AlphaForwardingRule.Put(keyalpha, &Metadata{}, objalpha) = %v; want nil", err)
	}

	// Get for different versions.
	if m, o, err := gce.ForwardingRule.Get(keyga, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.ForwardingRule.Get(keyga, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.ForwardingRule.Get(keyga, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.ForwardingRule.Get(keyalpha, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.ForwardingRule.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.ForwardingRule.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.ForwardingRule.Get(keybeta, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.ForwardingRule.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.ForwardingRule.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}

	// List.
	keys, err := gce.ForwardingRule.ListAllVersions()
	if err != nil {
		t.Errorf("gce.ForwardingRule.ListAllVersions() = %v, %v; want _, nil", keys, err)
	}

	// ListByFilter
	fn := func(k Key, m *Metadata, o *ga.ForwardingRule) (bool, error) {
		return true, nil
	}
	keys, err = gce.ForwardingRule.ListByFilter(fn, ConvertVersion)
	if err != nil {
		t.Errorf("gce.ForwardingRule.ListByFilter(..., ConvertVersion) = %v, %v; want _, nil", keys, err)
	}
}

func TestGCEAlphaForwardingRuleConversions(t *testing.T) {
	t.Parallel()

	gce := NewGCE()

	keyga := GlobalKey("keyga")
	objga := &ga.ForwardingRule{Name: keyga.Name}
	keyalpha := GlobalKey("keyalpha")
	objalpha := &alpha.ForwardingRule{Name: keyalpha.Name}
	keybeta := GlobalKey("keybeta")
	objbeta := &beta.ForwardingRule{Name: keybeta.Name}

	// Put for different versions.
	if err := gce.ForwardingRule.Put(keyga, &Metadata{}, objga); err != nil {
		t.Errorf("gce.ForwardingRule.Put(keyga, &Metadata{}, objga) = %v; want nil", err)
	}
	if err := gce.AlphaForwardingRule.Put(keyalpha, &Metadata{}, objalpha); err != nil {
		t.Errorf("gce.AlphaForwardingRule.Put(keyalpha, &Metadata{}, objalpha) = %v; want nil", err)
	}
	if err := gce.BetaForwardingRule.Put(keybeta, &Metadata{}, objbeta); err != nil {
		t.Errorf("gce.BetaForwardingRule.Put(keybeta, &Metadata{}, objbeta) = %v; want nil", err)
	}

	// Get for different versions.
	if m, o, err := gce.AlphaForwardingRule.Get(keyga, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.AlphaForwardingRule.Get(keyga, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.AlphaForwardingRule.Get(keyga, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.AlphaForwardingRule.Get(keyalpha, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.AlphaForwardingRule.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.AlphaForwardingRule.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.AlphaForwardingRule.Get(keybeta, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.AlphaForwardingRule.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.AlphaForwardingRule.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}

	// List.
	keys, err := gce.AlphaForwardingRule.ListAllVersions()
	if err != nil {
		t.Errorf("gce.AlphaForwardingRule.ListAllVersions() = %v, %v; want _, nil", keys, err)
	}

	// ListByFilter
	fn := func(k Key, m *Metadata, o *alpha.ForwardingRule) (bool, error) {
		return true, nil
	}
	keys, err = gce.AlphaForwardingRule.ListByFilter(fn, ConvertVersion)
	if err != nil {
		t.Errorf("gce.AlphaForwardingRule.ListByFilter(..., ConvertVersion) = %v, %v; want _, nil", keys, err)
	}
}

func TestGCEBetaForwardingRuleConversions(t *testing.T) {
	t.Parallel()

	gce := NewGCE()

	keyga := GlobalKey("keyga")
	objga := &ga.ForwardingRule{Name: keyga.Name}
	keyalpha := GlobalKey("keyalpha")
	objalpha := &alpha.ForwardingRule{Name: keyalpha.Name}
	keybeta := GlobalKey("keybeta")
	objbeta := &beta.ForwardingRule{Name: keybeta.Name}

	// Put for different versions.
	if err := gce.ForwardingRule.Put(keyga, &Metadata{}, objga); err != nil {
		t.Errorf("gce.ForwardingRule.Put(keyga, &Metadata{}, objga) = %v; want nil", err)
	}
	if err := gce.AlphaForwardingRule.Put(keyalpha, &Metadata{}, objalpha); err != nil {
		t.Errorf("gce.AlphaForwardingRule.Put(keyalpha, &Metadata{}, objalpha) = %v; want nil", err)
	}
	if err := gce.BetaForwardingRule.Put(keybeta, &Metadata{}, objbeta); err != nil {
		t.Errorf("gce.BetaForwardingRule.Put(keybeta, &Metadata{}, objbeta) = %v; want nil", err)
	}

	// Get for different versions.
	if m, o, err := gce.BetaForwardingRule.Get(keyga, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.BetaForwardingRule.Get(keyga, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.BetaForwardingRule.Get(keyga, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.BetaForwardingRule.Get(keyalpha, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.BetaForwardingRule.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.BetaForwardingRule.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.BetaForwardingRule.Get(keybeta, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.BetaForwardingRule.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.BetaForwardingRule.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}

	// List.
	keys, err := gce.BetaForwardingRule.ListAllVersions()
	if err != nil {
		t.Errorf("gce.BetaForwardingRule.ListAllVersions() = %v, %v; want _, nil", keys, err)
	}

	// ListByFilter
	fn := func(k Key, m *Metadata, o *beta.ForwardingRule) (bool, error) {
		return true, nil
	}
	keys, err = gce.BetaForwardingRule.ListByFilter(fn, ConvertVersion)
	if err != nil {
		t.Errorf("gce.BetaForwardingRule.ListByFilter(..., ConvertVersion) = %v, %v; want _, nil", keys, err)
	}
}

func TestGCEAlphaHealthCheckConversions(t *testing.T) {
	t.Parallel()

	gce := NewGCE()

	keyga := GlobalKey("keyga")
	objga := &ga.HealthCheck{Name: keyga.Name}
	keyalpha := GlobalKey("keyalpha")
	objalpha := &alpha.HealthCheck{Name: keyalpha.Name}
	keybeta := GlobalKey("keybeta")
	objbeta := &beta.HealthCheck{Name: keybeta.Name}

	// Put for different versions.
	if err := gce.AlphaHealthCheck.Put(keyalpha, &Metadata{}, objalpha); err != nil {
		t.Errorf("gce.AlphaHealthCheck.Put(keyalpha, &Metadata{}, objalpha) = %v; want nil", err)
	}
	if err := gce.BetaHealthCheck.Put(keybeta, &Metadata{}, objbeta); err != nil {
		t.Errorf("gce.BetaHealthCheck.Put(keybeta, &Metadata{}, objbeta) = %v; want nil", err)
	}
	if err := gce.HealthCheck.Put(keyga, &Metadata{}, objga); err != nil {
		t.Errorf("gce.HealthCheck.Put(keyga, &Metadata{}, objga) = %v; want nil", err)
	}

	// Get for different versions.
	if m, o, err := gce.AlphaHealthCheck.Get(keyga, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.AlphaHealthCheck.Get(keyga, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.AlphaHealthCheck.Get(keyga, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.AlphaHealthCheck.Get(keyalpha, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.AlphaHealthCheck.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.AlphaHealthCheck.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.AlphaHealthCheck.Get(keybeta, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.AlphaHealthCheck.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.AlphaHealthCheck.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}

	// List.
	keys, err := gce.AlphaHealthCheck.ListAllVersions()
	if err != nil {
		t.Errorf("gce.AlphaHealthCheck.ListAllVersions() = %v, %v; want _, nil", keys, err)
	}

	// ListByFilter
	fn := func(k Key, m *Metadata, o *alpha.HealthCheck) (bool, error) {
		return true, nil
	}
	keys, err = gce.AlphaHealthCheck.ListByFilter(fn, ConvertVersion)
	if err != nil {
		t.Errorf("gce.AlphaHealthCheck.ListByFilter(..., ConvertVersion) = %v, %v; want _, nil", keys, err)
	}
}

func TestGCEBetaHealthCheckConversions(t *testing.T) {
	t.Parallel()

	gce := NewGCE()

	keyga := GlobalKey("keyga")
	objga := &ga.HealthCheck{Name: keyga.Name}
	keyalpha := GlobalKey("keyalpha")
	objalpha := &alpha.HealthCheck{Name: keyalpha.Name}
	keybeta := GlobalKey("keybeta")
	objbeta := &beta.HealthCheck{Name: keybeta.Name}

	// Put for different versions.
	if err := gce.HealthCheck.Put(keyga, &Metadata{}, objga); err != nil {
		t.Errorf("gce.HealthCheck.Put(keyga, &Metadata{}, objga) = %v; want nil", err)
	}
	if err := gce.AlphaHealthCheck.Put(keyalpha, &Metadata{}, objalpha); err != nil {
		t.Errorf("gce.AlphaHealthCheck.Put(keyalpha, &Metadata{}, objalpha) = %v; want nil", err)
	}
	if err := gce.BetaHealthCheck.Put(keybeta, &Metadata{}, objbeta); err != nil {
		t.Errorf("gce.BetaHealthCheck.Put(keybeta, &Metadata{}, objbeta) = %v; want nil", err)
	}

	// Get for different versions.
	if m, o, err := gce.BetaHealthCheck.Get(keyga, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.BetaHealthCheck.Get(keyga, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.BetaHealthCheck.Get(keyga, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.BetaHealthCheck.Get(keyalpha, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.BetaHealthCheck.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.BetaHealthCheck.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.BetaHealthCheck.Get(keybeta, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.BetaHealthCheck.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.BetaHealthCheck.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}

	// List.
	keys, err := gce.BetaHealthCheck.ListAllVersions()
	if err != nil {
		t.Errorf("gce.BetaHealthCheck.ListAllVersions() = %v, %v; want _, nil", keys, err)
	}

	// ListByFilter
	fn := func(k Key, m *Metadata, o *beta.HealthCheck) (bool, error) {
		return true, nil
	}
	keys, err = gce.BetaHealthCheck.ListByFilter(fn, ConvertVersion)
	if err != nil {
		t.Errorf("gce.BetaHealthCheck.ListByFilter(..., ConvertVersion) = %v, %v; want _, nil", keys, err)
	}
}

func TestGCEHealthCheckConversions(t *testing.T) {
	t.Parallel()

	gce := NewGCE()

	keyga := GlobalKey("keyga")
	objga := &ga.HealthCheck{Name: keyga.Name}
	keyalpha := GlobalKey("keyalpha")
	objalpha := &alpha.HealthCheck{Name: keyalpha.Name}
	keybeta := GlobalKey("keybeta")
	objbeta := &beta.HealthCheck{Name: keybeta.Name}

	// Put for different versions.
	if err := gce.BetaHealthCheck.Put(keybeta, &Metadata{}, objbeta); err != nil {
		t.Errorf("gce.BetaHealthCheck.Put(keybeta, &Metadata{}, objbeta) = %v; want nil", err)
	}
	if err := gce.HealthCheck.Put(keyga, &Metadata{}, objga); err != nil {
		t.Errorf("gce.HealthCheck.Put(keyga, &Metadata{}, objga) = %v; want nil", err)
	}
	if err := gce.AlphaHealthCheck.Put(keyalpha, &Metadata{}, objalpha); err != nil {
		t.Errorf("gce.AlphaHealthCheck.Put(keyalpha, &Metadata{}, objalpha) = %v; want nil", err)
	}

	// Get for different versions.
	if m, o, err := gce.HealthCheck.Get(keyga, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.HealthCheck.Get(keyga, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.HealthCheck.Get(keyga, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.HealthCheck.Get(keyalpha, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.HealthCheck.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.HealthCheck.Get(keyalpha, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}
	if m, o, err := gce.HealthCheck.Get(keybeta, ConvertVersion); err == nil {
		if o == nil {
			t.Errorf("gce.HealthCheck.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, non-nil, nil", m, o, err)
		}
	} else {
		t.Errorf("gce.HealthCheck.Get(keybeta, ConvertVersion) = %v, %v, %v, want _, _, nil", m, o, err)
	}

	// List.
	keys, err := gce.HealthCheck.ListAllVersions()
	if err != nil {
		t.Errorf("gce.HealthCheck.ListAllVersions() = %v, %v; want _, nil", keys, err)
	}

	// ListByFilter
	fn := func(k Key, m *Metadata, o *ga.HealthCheck) (bool, error) {
		return true, nil
	}
	keys, err = gce.HealthCheck.ListByFilter(fn, ConvertVersion)
	if err != nil {
		t.Errorf("gce.HealthCheck.ListByFilter(..., ConvertVersion) = %v, %v; want _, nil", keys, err)
	}
}
