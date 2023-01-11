/*
Copyright 2016 The Kubernetes Authors.

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

package storage

import (
	"testing"

	api "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreateOnlyConfigMap(t *testing.T) {
	vault := NewFakeConfigMapVault(api.NamespaceSystem, "ingress-uid")
	// Get value from an empty vault.
	val, exists, err := vault.Get(UIDDataKey)
	if exists {
		t.Errorf("Got value from an empty vault")
	}

	// Store empty value for UIDDataKey.
	uid := "foo"
	err = vault.Put(UIDDataKey, uid, true)
	if err != nil {
		t.Errorf("expect err == nil, got %v", err)
	}
	val, exists, err = vault.Get(UIDDataKey)
	if !exists || err != nil {
		t.Errorf("Failed to retrieve value from vault: %v", err)
	}
	if val != "foo" {
		t.Errorf("expect val == foo, but got %v", val)
	}

	// Store empty value for UIDDataKey.
	uid = "bar"
	err = vault.Put(UIDDataKey, uid, true)
	if err == nil {
		t.Errorf("expect err != nil, got %v", err)
	}

	uid = "bar"
	err = vault.Put(UIDDataKey, uid, false)
	if err != nil {
		t.Errorf("expect err == nil, got %v", err)
	}
	val, exists, err = vault.Get(UIDDataKey)
	if !exists || err != nil {
		t.Errorf("Failed to retrieve value from vault: %v", err)
	}
	if val != "bar" {
		t.Errorf("expect val == bar, but got %v", val)
	}
}

func TestFakeConfigMapValue(t *testing.T) {
	vault := NewFakeConfigMapVault(api.NamespaceSystem, "ingress-uid")
	// Get value from an empty vault.
	val, exists, err := vault.Get(UIDDataKey)
	if exists {
		t.Errorf("Got value from an empty vault")
	}

	// Store empty value for UIDDataKey.
	uid := ""
	vault.Put(UIDDataKey, uid, false)
	val, exists, err = vault.Get(UIDDataKey)
	if !exists || err != nil {
		t.Errorf("Failed to retrieve value from vault: %v", err)
	}
	if val != "" {
		t.Errorf("Failed to store empty string as a key in the vault")
	}

	// Store actual value in key.
	storedVal := "newuid"
	vault.Put(UIDDataKey, storedVal, false)
	val, exists, err = vault.Get(UIDDataKey)
	if !exists || err != nil {
		t.Errorf("Failed to retrieve value from vault")
	} else if val != storedVal {
		t.Errorf("Failed to store empty string as a key in the vault")
	}

	// Store second value which will have the affect of updating to Store
	// rather than adding.
	secondVal := "bar"
	vault.Put("foo", secondVal, false)
	val, exists, err = vault.Get("foo")
	if !exists || err != nil || val != secondVal {
		t.Errorf("Failed to retrieve second value from vault")
	}
	val, exists, err = vault.Get(UIDDataKey)
	if !exists || err != nil || val != storedVal {
		t.Errorf("Failed to retrieve first value from vault")
	}

	// Delete value.
	if err := vault.Delete(); err != nil {
		t.Errorf("Failed to delete uid %v", err)
	}
	if _, exists, _ := vault.Get(UIDDataKey); exists {
		t.Errorf("Found uid but expected none after deletion")
	}
}
