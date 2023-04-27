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

package dualstack

import (
	"sync"
	"time"

	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

const (
	// Default time to wait between two successive migration-detachments.
	defaultMigrationWaitDuration = 1 * time.Minute
)

// Migrator exposes functions to control the migration of single-stack
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
type Migrator struct {
	// Setting this to false will make all exported functions a no-op.
	enableDualStack bool
	// The NEG syncer which will be synced when Continue gets called.
	syncer syncable

	// mu protects paused and continueInProgress.
	mu sync.Mutex
	// Identifies whether the migrator is paused.
	paused bool
	// Identifies if some async operation triggered by Continue is still in
	// progress.
	continueInProgress bool

	// Time to wait between two successive migration-detachments.
	migrationWaitDuration time.Duration

	logger klog.Logger
}

type syncable interface {
	Sync() bool
}

func NewMigrator(enableDualStackNEG bool, syncer syncable, logger klog.Logger) *Migrator {
	return &Migrator{
		enableDualStack:       enableDualStackNEG,
		syncer:                syncer,
		migrationWaitDuration: defaultMigrationWaitDuration,
		logger:                logger.WithName("DualStackMigrator"),
	}
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
// Refer the comment on [Migrator] for further details and
// terminologies.
func (d *Migrator) Filter(addEndpoints, removeEndpoints, committedEndpoints map[string]types.NetworkEndpointSet) string {
	if !d.enableDualStack {
		return ""
	}

	_, migrationEndpointsInRemoveSet := findAndFilterMigrationEndpoints(addEndpoints, removeEndpoints)

	if d.isPaused() {
		return ""
	}

	// TODO(gauravkghildiyal): Implement rate limited migration-detachment.
	for zone, endpointSet := range migrationEndpointsInRemoveSet {
		if endpointSet.Len() != 0 {
			removeEndpoints[zone] = removeEndpoints[zone].Union(endpointSet)
			return zone
		}
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
func (d *Migrator) Pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.enableDualStack {
		return
	}
	d.paused = true
}

// Continue will unpause the migration. It expects an error as input which
// specifies the result of the NEG-endpoint detach operation. Depending on
// whether the detach operation passed or failed, the effect of unpause could be
// delayed:
//   - If the NEG detach operation failed, the migration will be unpaused
//     immediately before Continue returns. This would allow any resyncs to
//     reattempt the migration. The migrator itself doesn't trigger any sync in
//     this case.
//   - If the NEG detach operation succeeded, a migrationWaitDuration timer will
//     be started, which upon completion will unpause the migration and also
//     trigger another sync. Continue will not keep the caller blocked for the
//     completion of the timer. If Continue is invoked multiple times, only the
//     first continue will trigger a resync.
func (d *Migrator) Continue(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Do nothing if any of the following is true:
	//  1. dualStackNEG is not enabled.
	//  2. We are not paused.
	//  3. Another continue is already running for this instance.
	if !d.enableDualStack || !d.paused || d.continueInProgress {
		d.logger.V(1).Info("Continue() returning early", "migrator.enableDualStack", d.enableDualStack, "migrator.paused", d.paused, "migrator.continueInProgress", d.continueInProgress)
		return
	}

	if err != nil {
		// NEG Detach failed; unpause immediately.
		d.paused = false
		return
	}

	// NEG Detach succeeded; unpause after migrationWaitDuration and trigger
	// resync.
	d.continueInProgress = true
	go func() {
		time.Sleep(d.migrationWaitDuration)

		d.mu.Lock()
		d.paused = false
		d.continueInProgress = false
		d.mu.Unlock()

		d.logger.V(2).Info("Triggering sync")
		d.syncer.Sync()
	}()
}

func (d *Migrator) isPaused() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.paused
}

// findAndFilterMigrationEndpoints will filter out the migration endpoints from
// the `addEndpoints` and `removeEndpoints` sets. The passed sets will get
// modified. The returned value will be two endpoints sets which will contain
// the values that were filtered out from the `addEndpoints` and
// `removeEndpoints` sets respectively.
func findAndFilterMigrationEndpoints(addEndpoints, removeEndpoints map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, map[string]types.NetworkEndpointSet) {
	allEndpoints := make(map[string]types.NetworkEndpointSet)
	for zone, endpointSet := range addEndpoints {
		allEndpoints[zone] = allEndpoints[zone].Union(endpointSet)
	}
	for zone, endpointSet := range removeEndpoints {
		allEndpoints[zone] = allEndpoints[zone].Union(endpointSet)
	}

	migrationEndpointsInAddSet := make(map[string]types.NetworkEndpointSet)
	migrationEndpointsInRemoveSet := make(map[string]types.NetworkEndpointSet)
	for zone, endpointSet := range allEndpoints {
		for endpoint := range endpointSet {
			if endpoint.IP == "" || endpoint.IPv6 == "" {
				// Endpoint is not dual-stack so continue.
				continue
			}

			// At this point, we know that `endpoint` is a dual-stack endpoint. An
			// endpoint can be identified as migrating if the IPs from the dual-stack
			// endpoint exist as individual single-stack endpoint inside
			// `addEndpoints` or `removeEndpoints`.

			// Construct single-stack endpoint corresponding to the dual-stack
			// endpoint. Their existence will determine if an endpoint is migrating.
			ipv4Only := endpoint
			ipv4Only.IPv6 = ""
			ipv6Only := endpoint
			ipv6Only.IP = ""

			isMigrating := false
			// Check if endpoint is migrating from dual-stack to single-stack.
			isMigrating = isMigrating || moveEndpoint(ipv4Only, addEndpoints, migrationEndpointsInAddSet, zone)
			isMigrating = isMigrating || moveEndpoint(ipv6Only, addEndpoints, migrationEndpointsInAddSet, zone)
			// Check if endpoint is migrating from single-stack to dual-stack.
			isMigrating = isMigrating || moveEndpoint(ipv4Only, removeEndpoints, migrationEndpointsInRemoveSet, zone)
			isMigrating = isMigrating || moveEndpoint(ipv6Only, removeEndpoints, migrationEndpointsInRemoveSet, zone)

			if isMigrating {
				moveEndpoint(endpoint, addEndpoints, migrationEndpointsInAddSet, zone)
				moveEndpoint(endpoint, removeEndpoints, migrationEndpointsInRemoveSet, zone)
			}
		}
	}

	return migrationEndpointsInAddSet, migrationEndpointsInRemoveSet
}

// moveEndpoint deletes endpoint `e` from `source[zone]` and adds it to
// `dest[zone]`. If the move was successful, `true` is returned. A return value
// of `false` denotes that nothing was moved and no input variable were
// modified.
func moveEndpoint(e types.NetworkEndpoint, source, dest map[string]types.NetworkEndpointSet, zone string) bool {
	if source == nil || dest == nil {
		return false
	}
	if source[zone].Has(e) {
		source[zone].Delete(e)
		if dest[zone] == nil {
			dest[zone] = types.NewNetworkEndpointSet()
		}
		dest[zone].Insert(e)
		return true
	}
	return false
}
