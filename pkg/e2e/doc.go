/*
Copyright 2018 The Kubernetes Authors.

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

// Package e2e contains supporting infrastructure for end-to-end integration
// testing driven by the tests in cmd/e2e-test.
//
// Test should be written with a Sandbox:
//
//	func TestExample(t *testing.T) {
//	  for _, tc := range []struct{
//	    ...
//	  }{
//	    ...
//	  }{
//	    tc := tc // avoid variable capture
//	    Framework.RunWithSandbox(t, func(t *testing.T, s *e2e.Sandbox) {
//	      t.Parallel()
//	      // Test code...
//	    })
//	  }
//	}
//
// The Sandbox will handle resource isolation and reclamation.
package e2e
