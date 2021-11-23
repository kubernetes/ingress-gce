/*
Copyright 2021 The Kubernetes Authors.

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

package flags

import "fmt"

// L4 External LoadBalancer feature enable flag
// Possible values:
// * opt-in - Controller only manages Net LBs for services that contain opt-in annotation: cloud.google.com/l4-rbs: '{"enabled":true}' or
//            finalizer key gke.networking.io/l4-netlb-v2.
//             If managed service is already created as target pool based then it is migrated to regional-backend service based load balancer.
// * enabled - Controller manages all new Net LBs and all Net LBs for services with gke.networking.io/l4-netlb-v2 finalizer.
//             If managed service is already created as target pool based then it is migrated to regional-backend service based load balancer.
// * enforced - Controller manages all new Net LBs and all Net LBs for services with gke.networking.io/l4-netlb-v2 finalizer.
//              Services created as target pool based load balancer are migrated regardless of the annotation.
type RbsMode int32

const (
	DISABLED RbsMode = iota
	OPTIN
	ENABLED
	ENFORCED
)

var rbsModeNames = [...]string{
	DISABLED: "",
	OPTIN:    "opt-in",
	ENABLED:  "enabled",
	ENFORCED: "enforced",
}

// Set overrides flag Value interface.
// this function convert string to rbsMode enum value.
// If value is not valid rbs mode then it returns error.
func (rbs *RbsMode) Set(value string) error {
	for i, r := range rbsModeNames {
		if value == r {
			*rbs = RbsMode(i)
			return nil
		}
	}
	return fmt.Errorf("Unsupported value %s", value)
}

// String overrides flag Value interface
func (rbs *RbsMode) String() string {
	return rbsModeNames[*rbs]
}

// Type overrides flag Value interface
func (rbs *RbsMode) Type() string {
	return "rbsMode"
}
