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

type A struct {
	A, B, C string
}

type B struct {
	A, B, D string
}

type E struct{}

func (*E) MarshalJSON() ([]byte, error) {
	return nil, errors.New("injected error")
}

func TestCopyVisJSON(t *testing.T) {
	t.Parallel()

	var b B
	srcA := &A{"aa", "bb", "cc"}
	err := copyViaJSON(&b, srcA)
	if err != nil {
		t.Errorf(`copyViaJSON(&b, %+v) = %v, want nil`, srcA, err)
	} else {
		expectedB := B{"aa", "bb", ""}
		if b != expectedB {
			t.Errorf("b == %+v, want %+v", b, expectedB)
		}
	}

	var a A
	srcB := &B{"aaa", "bbb", "ccc"}
	err = copyViaJSON(&a, srcB)
	if err != nil {
		t.Errorf(`copyViaJSON(&a, %+v) = %v, want nil`, srcB, err)
	} else {
		expectedA := A{"aaa", "bbb", ""}
		if a != expectedA {
			t.Errorf("a == %+v, want %+v", a, expectedA)
		}
	}

	if err := copyViaJSON(&a, &E{}); err == nil {
		t.Errorf("copyViaJSON(&a, &E{}) = nil, want error")
	}
}
