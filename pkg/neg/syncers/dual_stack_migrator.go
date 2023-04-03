/*
Copyright 2023 The Kubernetes Authors.

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

package syncers

import "k8s.io/ingress-gce/pkg/neg/types"

// DualStackMigrator exposes functions to control the migration of single-stack
// NEG endpoints to dual-stack NEG endpoints (and vice versa)
//
// # Single-stack vs Dual-stack
//
//   - A NEG endpoint is said to be single-stack if it just has an IPv4 or IPv6
//     address (but not both.)
//
//   - A NEG endpoint is said to be dual-stack if it has both IPv4 and IPv6
//     address.
//
// # Migration endpoint
//
// An endpoint is said to be a migration-endpoint if its current state is
// single-stack but desired state is dual-stack (and vice versa.)
//
// TODO(gauravkghildiyal): Add details about the heuristics as we go on
// implementing.
type DualStackMigrator struct {
	// Setting this to false will make all exported functions a no-op.
	enableDualStackNEG bool
}

// Filter will modify the `addEndpoints` and `removeEndpoints` in TWO DISTINCT
// ways:
//  1. Remove all migration-endpoints, irrespective of whether the migrator is
//     paused or not.
//  2. If the migrator is not currently paused, it will also start the
//     detachment of a subset of migration-endpoints from a single zone.
//
// The returned string represents the zone for which detachment was started. An
// empty return value signifies that detachment was not started (which is the
// case when there were no migration-endpoints to begin with, or the migrator
// was paused.)
//
// Refer the comment on [DualStackMigrator] for further details and
// terminologies.
func (d *DualStackMigrator) Filter(addEndpoints, removeEndpoints, committedEndpoints map[string]types.NetworkEndpointSet) string {
	if !d.enableDualStackNEG {
		return ""
	}
	return ""
}

// Pause will prevent any subsequent Filter() invocations from starting
// detachment of migration-endpoints. Pause should be invoked before starting
// any NEG-endpoint detach operations that include migration-endpoints.
//
// Invoking Pause on a migrator which is already paused will be a no-op.
//
// Pause is usually paired with a Continue() invocation once the NEG-endpoint
// detach operation completes.
func (d *DualStackMigrator) Pause() {
	if !d.enableDualStackNEG {
		return
	}
}

// Continue will unpause the migration. It expects an error as input which
// specifies the result of the NEG-endpoint detach operation. Depending on
// whether the detach operation passed or failed, the effect of unpause could be
// delayed:
//   - If the NEG detach operation succeeded, a 1 minute timer will be started,
//     which upon completion will unpause the migration and also trigger another
//     sync. Continue will not keep the caller blocked for the completion of the
//     timer.
//   - If the NEG detach operation failed, the migration will be unpaused
//     immediately before Continue returns. This would allow any resyncs to
//     reattempt the migration. The migrator itself doesn't trigger any sync in
//     this case.
func (d *DualStackMigrator) Continue(err error) {
	if !d.enableDualStackNEG {
		return
	}
}
