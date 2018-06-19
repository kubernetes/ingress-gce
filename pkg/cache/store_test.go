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
	"errors"
	"testing"
)

func TestStore(t *testing.T) {
	t.Parallel()

	st := newStore()
	gkey := GlobalKey("abc")

	obj := "obj1"
	if obj := st.get(gkey); obj != nil {
		t.Errorf("st.Get(%v) = %v, want nil", gkey, obj)
	}
	if err := st.put(gkey, &Metadata{}, obj); err != nil {
		t.Fatalf(`st.put(%v, &Metadata{}, %q) = %v, want nil`, gkey, err, obj)
	}
	entry := st.get(gkey)
	if entry == nil {
		t.Fatalf("st.Get(%v) = nil, want non-nil", gkey)
	}
	if entry.obj != obj {
		t.Fatalf("entry.obj == %v, want %v", entry.obj, obj)
	}
	// Overwrite entry.
	obj2 := "obj2"
	if err := st.put(gkey, &Metadata{}, obj2); err != nil {
		t.Fatalf(`st.put(%v, &Metadata{}, %q) = %v, want nil`, gkey, err, obj2)
	}
	entry = st.get(gkey)
	if entry == nil {
		t.Fatalf("st.Get(%v) = nil, want non-nil", gkey)
	}
	if entry.obj != obj2 {
		t.Fatalf("entry.obj == %v, want %v", entry.obj, obj2)
	}
	// Flush entry.
	st.flush(gkey)
	if obj := st.get(gkey); obj != nil {
		t.Errorf("st.Get(%v) = %v, want nil", gkey, obj)
	}
}

var testEntries = map[Key]string{
	// These are all distinct entries.
	GlobalKey("a"):        "a",
	RegionalKey("c", "r"): "c",
	ZonalKey("b", "z"):    "b",
	GlobalKey("d"):        "dg",
	RegionalKey("d", "r"): "dr",
	ZonalKey("d", "z"):    "dz",
}

func addTestEntries(t *testing.T, st *store) {
	t.Helper()

	for k, v := range testEntries {
		if err := st.put(k, &Metadata{}, v); err != nil {
			t.Fatalf("st.put(%v, &Metadata{}, %v) = %v, want nil", k, v, err)
		}
	}
}

func TestStoreGetPutMultiple(t *testing.T) {
	t.Parallel()

	st := newStore()
	addTestEntries(t, st)
	for k, v := range testEntries {
		entry := st.get(k)
		if entry == nil {
			t.Errorf("st.get(%v) = nil, want non-nil", k)
			continue
		}
		if entry.obj != v {
			t.Errorf("entry.obj = %v, want %v", entry.obj, v)
		}
	}
}

func TestStoreList(t *testing.T) {
	t.Parallel()

	st := newStore()
	addTestEntries(t, st)

	// List all of the keys.
	keys, err := st.listByFilter(func(*entry) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("st.listByFilter(all) = %v, %v; want _, nil", keys, err)
	}
	if len(keys) != len(testEntries) {
		t.Errorf("keys is not equal to keys in testEntries: keys = %v, testEntries = %v", keys, testEntries)
	}
	for _, k := range keys {
		if _, ok := testEntries[k]; !ok {
			t.Errorf("st.listByFilter() returned key %v that was not in testEntries", k)
		}
	}
	// List only "d" keys.
	keys, err = st.listByFilter(func(e *entry) (bool, error) {
		return e.key.Name == "d", nil
	})
	if err != nil {
		t.Fatalf(`st.listByFilter(filter("d")) = %v, %v; want _, nil`, keys, err)
	}
	for _, k := range keys {
		if k.Name != "d" {
			t.Errorf(`st.listByFilter(filter("d")) returned key %v`, k)
		}
		if _, ok := testEntries[k]; !ok {
			t.Errorf("st.listByFilter() returned key %v that was not in testEntries", k)
		}
	}
	keysMap := map[Key]bool{}
	for _, k := range keys {
		keysMap[k] = true
	}
	for k := range testEntries {
		if k.Name == "d" && !keysMap[k] {
			t.Errorf("key %v was in testEntries but not in keysMap", k)
		}
	}
}

func TestStoreListError(t *testing.T) {
	t.Parallel()

	st := newStore()
	addTestEntries(t, st)

	_, err := st.listByFilter(func(*entry) (bool, error) { return true, errors.New("injected error") })
	if err == nil {
		t.Errorf("st.listByFilter(error) = _, nil; want error")
	}
}

func TestStoreFlush(t *testing.T) {
	t.Parallel()

	st := newStore()
	for k, v := range testEntries {
		if err := st.put(k, &Metadata{}, v); err != nil {
			t.Fatalf("st.put(%v, &Metadata{}, %v) = %v, want nil", k, v, err)
		}
	}
	for k := range testEntries {
		st.flush(k)
	}
	keys, err := st.listByFilter(func(*entry) (bool, error) { return true, nil })
	if err != nil {
		t.Fatalf("st.listByFilter(all) = %v, %v; want _, nil", keys, err)
	}
	if len(keys) != 0 {
		t.Errorf("st.listByFilter(all) = %v, nil; want [], nil", keys)
	}
}
