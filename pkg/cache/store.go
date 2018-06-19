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

type value struct {
	meta *Metadata
	obj  interface{}
}

type entry struct {
	key Key
	value
}

// store is the type-generic backing store for a cache. This struct is not
// threadsafe.
type store struct {
	m map[Key]*value
}

type filterFunc func(*entry) (bool, error)

func newStore() *store {
	return &store{make(map[Key]*value)}
}

func (st *store) put(k Key, m *Metadata, o interface{}) error {
	st.m[k] = &value{m, o}
	return nil
}

func (st *store) get(k Key) *entry {
	if v, ok := st.m[k]; ok {
		return &entry{key: k, value: *v}
	}
	return nil
}

func (st *store) listByFilter(filter filterFunc) ([]Key, error) {
	var ret []Key
	for k, v := range st.m {
		ok, err := filter(&entry{k, *v})
		if err != nil {
			return nil, err
		}
		if ok {
			ret = append(ret, k)
		}
	}
	return ret, nil
}

func (st *store) flush(k Key) {
	if _, ok := st.m[k]; ok {
		delete(st.m, k)
	}
}
