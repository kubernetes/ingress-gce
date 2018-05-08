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

package utils

import (
	"testing"
)

func TestGetDefaultBackendName(t *testing.T) {
	urlMap := GCEURLMap{}
	urlMap.PutDefaultBackendName("foo")
	n := urlMap.GetDefaultBackendName(false)
	if n != "foo" {
		t.Fatalf("Expected default backend name foo. Got %v", n)
	}
}

func TestGetDefaultBackendNameDestructive(t *testing.T) {
	urlMap := GCEURLMap{}
	urlMap.PutDefaultBackendName("foo")
	n := urlMap.GetDefaultBackendName(true)
	if n != "foo" {
		t.Fatalf("Expected default backend name foo. Got %v", n)
	}
	n = urlMap.GetDefaultBackendName(true)
	if n != "" {
		t.Fatalf("Expected empty default backend name. Got %v", n)
	}
}

func TestPutDefaultBackendName(t *testing.T) {
	urlMap := GCEURLMap{}
	urlMap.PutDefaultBackendName("foo")
	urlMap.PutDefaultBackendName("bar")
	n := urlMap.GetDefaultBackendName(true)
	if n != "bar" {
		t.Fatalf("Expected default backend name bar. Got %v", n)
	}
}
