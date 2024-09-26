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
	"math"
	"sync"
	"time"

	"k8s.io/ingress-gce/pkg/neg/types"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

const (
	// Default time to wait between two successive migration-detachments.
	defaultMigrationWaitDuration = 1 * time.Minute
	// Default threshold used to determine if it has been too long since the last
	// successful detachment.
	defaultPreviousDetachThreshold = 5 * time.Minute
	// Default multiplication factor used to decide the maximum number of
	// endpoints which the migrator can attempt to migrate in each Filter
	// invocation.
	defaultFractionOfMigratingEndpoints float64 = 0.1
	// Default multiplication factor used to decide if there are too many pending
	// NEG attach operations.
	defaultFractionForPendingAttachmentThreshold float64 = 0.5
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
type Migrator struct {
	// Setting this to false will make all exported functions a no-op.
	enableDualStack bool
	// The NEG syncer which will be synced when Continue gets called.
	syncer syncable
	// The unique identifier of the syncer which is using this Migrator.
	syncerKey types.NegSyncerKey
	// metricsCollector is used for exporting metrics.
	metricsCollector MetricsCollector
	// errorStateChecker is used to check if the the transactionSyncer is in error
	// state.
	errorStateChecker errorStateChecker

	// mu protects paused, continueInProgress and previousDetach.
	mu sync.Mutex
	// Identifies whether the migrator is paused.
	paused bool
	// Identifies if some async operation triggered by Continue is still in
	// progress.
	continueInProgress bool
	// The most recent time when Continue was invoked for a successful detachment.
	previousDetach time.Time

	// Time to wait between two successive migration-detachments.
	migrationWaitDuration time.Duration
	// Threshold used to determine if it has been too long since the
	// last successful detachment.
	previousDetachThreshold time.Duration
	// Multiplication factor used to decide the maximum number of endpoints which
	// the migrator can attempt to migrate in each Filter invocation.
	fractionOfMigratingEndpoints float64
	// Multiplication factor used to decide if there are too many pending NEG
	// attach operations.
	fractionForPendingAttachmentThreshold float64

	clock  clock.Clock
	logger klog.Logger
}

type syncable interface {
	Sync() bool
}

type errorStateChecker interface {
	InErrorState() bool
}

type MetricsCollector interface {
	CollectDualStackMigrationMetrics(key types.NegSyncerKey, committedEndpoints map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet, migrationCount int)
}

func NewMigrator(enableDualStackNEG bool, syncer syncable, syncerKey types.NegSyncerKey, metricsCollector MetricsCollector, errorStateChecker errorStateChecker, logger klog.Logger) *Migrator {
	return &Migrator{
		enableDualStack:                       enableDualStackNEG,
		syncer:                                syncer,
		syncerKey:                             syncerKey,
		metricsCollector:                      metricsCollector,
		errorStateChecker:                     errorStateChecker,
		migrationWaitDuration:                 defaultMigrationWaitDuration,
		previousDetachThreshold:               defaultPreviousDetachThreshold,
		fractionOfMigratingEndpoints:          defaultFractionOfMigratingEndpoints,
		fractionForPendingAttachmentThreshold: defaultFractionForPendingAttachmentThreshold,
		clock:                                 clock.RealClock{},
		logger:                                logger.WithName("DualStackMigrator"),
	}
}

// Filter will modify the `addEndpoints` and `removeEndpoints` in TWO DISTINCT
// ways:
//  1. Remove all migration-endpoints, irrespective of whether the migrator is
//     paused or not.
//  2. If the migrator is not currently paused, it will also start the
//     detachment of a subset of migration-endpoints from a single zone.
//
// The returned EndpointGroup represents the zone and subnet of NEG for which
// detachment was started on. An empty subnet and zone value signifies that
// detachment was not started (which is the case when there were no
// migration-endpoints to begin with, or the migrator was paused.)
func (d *Migrator) Filter(addEndpoints, removeEndpoints, committedEndpoints map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet) negtypes.EndpointGroupInfo {
	if !d.enableDualStack {
		return negtypes.EndpointGroupInfo{}
	}

	_, migrationEndpointsInRemoveSet := findAndFilterMigrationEndpoints(addEndpoints, removeEndpoints)
	migrationCount := endpointsCount(migrationEndpointsInRemoveSet)

	d.metricsCollector.CollectDualStackMigrationMetrics(d.syncerKey, committedEndpoints, migrationCount)

	paused := d.isPaused()
	if migrationCount == 0 || paused {
		d.logger.V(2).Info("Not starting migration detachments", "migrationCount", migrationCount, "paused", paused)
		return negtypes.EndpointGroupInfo{}
	}

	return d.calculateMigrationEndpointsToDetach(addEndpoints, removeEndpoints, committedEndpoints, migrationEndpointsInRemoveSet)
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
	d.previousDetach = d.clock.Now()
	go func() {
		d.clock.Sleep(d.migrationWaitDuration)

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

// calculateMigrationEndpointsToDetach will move a subset of migrationEndpoints
// to removeEndpoints so that they can be detached. The number of endpoints to
// move will be determined using the following heuritic:
//
//  1. The desired number of endpoints which can be moved is:
//
//     fractionOfMigratingEndpoints * count(committedEndpoints + migrationEndpoints)
//
//  2. All endpoints being moved will be from the same zone.
//
//  3. If all zones have less than the desired number of endpoints, then all
//     endpoints from the largest zone will be moved.
//
//  4. No endpoints will be moved if (1) there are many endpoints waiting to be
//     attached (as determined by the manyEndpointsWaitingToBeAttached()
//     function) AND (2) we are in degraded mode OR the previous successful
//     detach was quite recent (as determined by the
//     tooLongSincePreviousDetach() function)
func (d *Migrator) calculateMigrationEndpointsToDetach(addEndpoints, removeEndpoints, committedEndpoints, migrationEndpoints map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet) negtypes.EndpointGroupInfo {
	addCount := endpointsCount(addEndpoints)
	committedCount := endpointsCount(committedEndpoints)
	migrationCount := endpointsCount(migrationEndpoints)

	if d.manyEndpointsWaitingToBeAttached(addCount, committedCount, migrationCount) && (d.errorStateChecker.InErrorState() || !d.tooLongSincePreviousDetach()) {
		d.logger.V(1).Info("Not starting migration detachments; Too many attachments are pending and the threshold for forceful detach hasn't been reached.",
			"addCount", addCount,
			"committedCount", committedCount,
			"migrationCount", migrationCount,
			"fractionForPendingAttachmentThreshold", d.fractionForPendingAttachmentThreshold,
			"inErrorState", d.errorStateChecker.InErrorState(),
			"previousDetach", d.previousDetach,
			"previousDetachThreshold", d.previousDetachThreshold,
		)
		return negtypes.EndpointGroupInfo{}
	}

	// Find the zone and subnet which has the maximum number of migration-endpoints.
	endpointGroupInfo, maxZoneEndpointCount := negtypes.EndpointGroupInfo{}, 0
	for curEndpointInfo, endpointSet := range migrationEndpoints {
		if endpointSet.Len() > maxZoneEndpointCount {
			maxZoneEndpointCount = endpointSet.Len()
			endpointGroupInfo = curEndpointInfo
		}
	}
	if endpointGroupInfo.Zone == "" && endpointGroupInfo.Subnet == "" {
		return negtypes.EndpointGroupInfo{}
	}

	currentlyMigratingCount := int(math.Ceil(float64(committedCount+migrationCount) * d.fractionOfMigratingEndpoints))
	if currentlyMigratingCount > maxZoneEndpointCount {
		currentlyMigratingCount = maxZoneEndpointCount
	}
	d.logger.V(2).Info("Result of migration heuristic calculations", "currentlyMigratingCount", currentlyMigratingCount, "totalMigrationCount", migrationCount)

	if removeEndpoints[endpointGroupInfo] == nil {
		removeEndpoints[endpointGroupInfo] = types.NewNetworkEndpointSet()
	}
	for i := 0; i < currentlyMigratingCount; i++ {
		endpoint, ok := migrationEndpoints[endpointGroupInfo].PopAny()
		if !ok {
			break
		}
		removeEndpoints[endpointGroupInfo].Insert(endpoint)
	}

	return endpointGroupInfo
}

// Returns true if there are many endpoints waiting to be attached.
//
// Refer the implementation below to get the exact heuristic being used to
// determine this.
func (d *Migrator) manyEndpointsWaitingToBeAttached(addCount, committedCount, migrationCount int) bool {
	return addCount >= int(math.Ceil(float64(committedCount+migrationCount)*d.fractionForPendingAttachmentThreshold))
}

// Returns true if the time since the last successful detach exceeds the
// previousDetachThreshold
func (d *Migrator) tooLongSincePreviousDetach() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.clock.Since(d.previousDetach) >= d.previousDetachThreshold
}

// findAndFilterMigrationEndpoints will filter out the migration endpoints from
// the `addEndpoints` and `removeEndpoints` sets. The passed sets will get
// modified. The returned value will be two endpoints sets which will contain
// the values that were filtered out from the `addEndpoints` and
// `removeEndpoints` sets respectively.
func findAndFilterMigrationEndpoints(addEndpoints, removeEndpoints map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet) (map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet, map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet) {
	allEndpoints := make(map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet)
	for endpointGroupInfo, endpointSet := range addEndpoints {
		allEndpoints[endpointGroupInfo] = allEndpoints[endpointGroupInfo].Union(endpointSet)
	}
	for endpointGroupInfo, endpointSet := range removeEndpoints {
		allEndpoints[endpointGroupInfo] = allEndpoints[endpointGroupInfo].Union(endpointSet)
	}

	migrationEndpointsInAddSet := make(map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet)
	migrationEndpointsInRemoveSet := make(map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet)
	for endpointGroupInfo, endpointSet := range allEndpoints {
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
			isMigrating = isMigrating || moveEndpoint(ipv4Only, addEndpoints, migrationEndpointsInAddSet, endpointGroupInfo)
			isMigrating = isMigrating || moveEndpoint(ipv6Only, addEndpoints, migrationEndpointsInAddSet, endpointGroupInfo)
			// Check if endpoint is migrating from single-stack to dual-stack.
			isMigrating = isMigrating || moveEndpoint(ipv4Only, removeEndpoints, migrationEndpointsInRemoveSet, endpointGroupInfo)
			isMigrating = isMigrating || moveEndpoint(ipv6Only, removeEndpoints, migrationEndpointsInRemoveSet, endpointGroupInfo)

			if isMigrating {
				moveEndpoint(endpoint, addEndpoints, migrationEndpointsInAddSet, endpointGroupInfo)
				moveEndpoint(endpoint, removeEndpoints, migrationEndpointsInRemoveSet, endpointGroupInfo)
			}
		}
	}

	return migrationEndpointsInAddSet, migrationEndpointsInRemoveSet
}

// moveEndpoint deletes endpoint `e` from `source[endpointGroupInfo]` and adds it to
// `dest[endpointGroupInfo]`. If the move was successful, `true` is returned. A return value
// of `false` denotes that nothing was moved and no input variable were
// modified.
func moveEndpoint(e types.NetworkEndpoint, source, dest map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet, endpointGroupInfo negtypes.EndpointGroupInfo) bool {
	if source == nil || dest == nil {
		return false
	}
	if source[endpointGroupInfo].Has(e) {
		source[endpointGroupInfo].Delete(e)
		if dest[endpointGroupInfo] == nil {
			dest[endpointGroupInfo] = types.NewNetworkEndpointSet()
		}
		dest[endpointGroupInfo].Insert(e)
		return true
	}
	return false
}

func endpointsCount(endpointSets map[negtypes.EndpointGroupInfo]types.NetworkEndpointSet) int {
	var count int
	for _, endpointSet := range endpointSets {
		count += endpointSet.Len()
	}
	return count
}
