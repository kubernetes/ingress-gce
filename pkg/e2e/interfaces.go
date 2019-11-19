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

package e2e

import (
	"testing"
)

// UpgradeTest is an interface for writing generic upgrade test.
// TODO: add version compatibility into the interface
type UpgradeTest interface {
	// Name returns the name/description of the test.
	Name() string
	// Init initialized the Upgrade Test.
	Init(t *testing.T, s *Sandbox, framework *Framework) error
	// PreUpgrade runs before master upgrade.
	PreUpgrade() error
	// DuringUpgrade runs during master upgrade.
	DuringUpgrade() error
	// PostUpgrade runs after master upgrade.
	PostUpgrade() error
}
