package dualstack

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/ktesting"
)

func TestFilter(t *testing.T) {
	testCases := []struct {
		desc                string
		migrator            *Migrator
		addEndpoints        map[string]types.NetworkEndpointSet
		removeEndpoints     map[string]types.NetworkEndpointSet
		committedEndpoints  map[string]types.NetworkEndpointSet
		wantAddEndpoints    map[string]types.NetworkEndpointSet
		wantRemoveEndpoints map[string]types.NetworkEndpointSet
		wantMigrationZone   bool
	}{
		{
			desc: "paused migrator should only filter migration endpoints and not start detachment",
			migrator: func() *Migrator {
				m := newMigratorForTest(t, true)
				m.Pause()
				return m
			}(),
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "c", IPv6: "C"},
				}...),
			},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "D"},
				}...),
			},
			wantAddEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b"},
				}...),
			},
			wantRemoveEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					// Migration-endpoints were filtered out but no new migration
					// detachment was started.
					{IP: "c", IPv6: "C"},
				}...),
			},
		},
		{
			desc:     "unpaused migrator should filter migration endpoints AND also start detachment",
			migrator: newMigratorForTest(t, true),
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "c", IPv6: "C"},
				}...),
			},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "D"},
				}...),
			},
			wantAddEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b"},
				}...),
			},
			wantRemoveEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // Migration detachment started.
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantMigrationZone: true,
		},
		{
			desc:     "migrator should do nothing if enableDualStack is false",
			migrator: newMigratorForTest(t, false),
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "c", IPv6: "C"},
				}...),
			},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "D"},
				}...),
			},
			wantAddEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			wantRemoveEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "c", IPv6: "C"},
				}...),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotAddEndpoints := cloneZoneNetworkEndpointsMap(tc.addEndpoints)
			gotRemoveEndpoints := cloneZoneNetworkEndpointsMap(tc.removeEndpoints)
			gotCommittedEndpoints := cloneZoneNetworkEndpointsMap(tc.committedEndpoints)
			gotMigrationZone := tc.migrator.Filter(gotAddEndpoints, gotRemoveEndpoints, gotCommittedEndpoints)

			if tc.wantMigrationZone && gotMigrationZone == "" {
				t.Errorf("Filter() returned empty migrationZone; want non empty migrationZone")
			}

			if diff := cmp.Diff(tc.wantAddEndpoints, gotAddEndpoints); diff != "" {
				t.Errorf("Filter() returned unexpected diff in addEndpoints (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantRemoveEndpoints, gotRemoveEndpoints); diff != "" {
				t.Errorf("Filter() returned unexpected diff in removeEndpoints (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.committedEndpoints, gotCommittedEndpoints); diff != "" {
				t.Errorf("Filter() returned unexpected diff in committedEndpoints; want no diff; (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPause(t *testing.T) {
	addEndpoints := map[string]types.NetworkEndpointSet{
		"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
			{IP: "a", IPv6: "A"}, // migrating
			{IP: "b"},
			{IP: "c", IPv6: "C"},
			{IP: "d"}, // migrating
		}...),
	}
	removeEndpoints := map[string]types.NetworkEndpointSet{
		"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
			{IP: "a"}, // migrating
			{IP: "e", IPv6: "E"},
			{IP: "d", IPv6: "D"}, // migrating
		}...),
	}

	migrator := newMigratorForTest(t, true)

	// Ensure that before calling pause, Filter() is working as expected and is
	// starting migration-detachments.
	clonedAddEndpoints := cloneZoneNetworkEndpointsMap(addEndpoints)
	clonedRemoveEndpoints := cloneZoneNetworkEndpointsMap(removeEndpoints)
	migrator.Filter(clonedAddEndpoints, clonedRemoveEndpoints, map[string]types.NetworkEndpointSet{})
	possibleMigrationDetachments := []types.NetworkEndpoint{
		{IP: "a"},            // migrating
		{IP: "d", IPv6: "D"}, // migrating
	}
	if !clonedRemoveEndpoints["zone1"].HasAny(possibleMigrationDetachments...) {
		t.Fatalf("Precondition to verify the behaviour of Pause() not satisfied; Filter() should start migration-detachments; got removeEndpoints=%+v; want non-empty union with %+v", clonedRemoveEndpoints, possibleMigrationDetachments)
	}

	// We should be able to Pause multiple times without interleaving with
	// Continue(). An incorrect implementation of Pause could result in subsequent
	// Pause calls blocking the caller, in which case this unit test will never
	// complete.
	migrator.Pause()
	migrator.Pause()
	migrator.Pause()

	// The effect of Pause() is observed by verifying that Filter() does not start
	// any migration-detachments when paused.
	migrator.Filter(addEndpoints, removeEndpoints, map[string]types.NetworkEndpointSet{})
	wantRemoveEndpoints := map[string]types.NetworkEndpointSet{
		"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
			// Since we don't expect any migration-detachments, this set should be
			// missing all migration endpoints.
			{IP: "e", IPv6: "E"},
		}...),
	}

	if diff := cmp.Diff(wantRemoveEndpoints, removeEndpoints); diff != "" {
		t.Errorf("Unexpected diff in removeEndpoints; Pause() not stopping Filter from starting detachment; (-want +got):\n%s", diff)
	}
}

func TestContinue_InputError(t *testing.T) {
	syncable := &fakeSyncable{}
	migrator := &Migrator{
		enableDualStack:       true,
		paused:                true,
		migrationWaitDuration: 5 * time.Second,
		syncer:                syncable,
	}

	migrator.Continue(errors.New("random error"))

	if migrator.isPaused() {
		t.Errorf("Continue(err) didn't unpause the migrator; want migrator to be unpaused before Continue(err) returns")
	}
	if syncable.syncCount != 0 {
		t.Errorf("Continue(err) triggered sync %v times; want no syncs triggered", syncable.syncCount)
	}
}

func TestContinue_NoInputError(t *testing.T) {
	t.Parallel()

	syncable := &fakeSyncable{}
	migrator := &Migrator{
		enableDualStack:       true,
		paused:                true,
		migrationWaitDuration: 10 * time.Millisecond,
		syncer:                syncable,
		logger:                klog.Background(),
	}

	migrator.Continue(nil)

	if !migrator.isPaused() {
		t.Errorf("Continue(nil) most likely unpaused the migrator before returning; want migrator to be unpaused after %v", migrator.migrationWaitDuration)
	}

	// Sleep until we can expect a sync.
	time.Sleep(migrator.migrationWaitDuration)

	if err := wait.PollImmediate(1*time.Second, migrator.migrationWaitDuration, func() (done bool, err error) {
		if syncable.syncCount == 0 {
			return false, nil
		}
		if migrator.isPaused() {
			return false, errors.New("Continue(nil) triggered sync before unpausing; want unpause to happen before triggering sync")
		}
		return true, nil
	}); err != nil {
		if err == wait.ErrWaitTimeout {
			t.Errorf("Continue(nil) didn't trigger a sync even after waiting for %v; want sync to be triggered shortly after %v", 2*migrator.migrationWaitDuration, migrator.migrationWaitDuration)
		} else {
			t.Error(err)
		}
	}
}

func TestContinue_NoInputError_MultipleInvocationsShouldSyncOnce(t *testing.T) {
	t.Parallel()

	syncable := &fakeSyncable{}
	migrator := &Migrator{
		enableDualStack:       true,
		paused:                true,
		migrationWaitDuration: 10 * time.Millisecond,
		syncer:                syncable,
		logger:                klog.Background(),
	}

	for i := 0; i < 10; i++ {
		go migrator.Continue(nil)
	}

	// We wait for 3x time to ensure that if multiple Continues attempted to
	// resync, atleast one of those go routines finished.
	time.Sleep(3 * migrator.migrationWaitDuration)
	if syncable.syncCount != 1 {
		t.Errorf("Continue(nil) triggered %v syncs; want exactly 1 sync", syncable.syncCount)
	}
}

type fakeSyncable struct {
	syncCount int
}

func (f *fakeSyncable) Sync() bool {
	f.syncCount++
	return true
}

type fakeErrorStateChecker struct {
	errorState bool
}

func (f *fakeErrorStateChecker) InErrorState() bool {
	return f.errorState
}

func TestContinue_NoInputError_ShouldChangeTimeSincePreviousDetach(t *testing.T) {
	t.Parallel()

	syncable := &fakeSyncable{}

	migrator := &Migrator{
		enableDualStack:         true,
		paused:                  true,
		previousDetachThreshold: 20 * time.Millisecond,
		syncer:                  syncable,
		logger:                  klog.Background(),
	}

	// Ensure that before Continue, tooLongSincePreviousDetach() returns true.
	if !migrator.tooLongSincePreviousDetach() {
		t.Fatalf("Precondition failed; tooLongSincePreviousDetach() = 'false'; want 'true' before calling Continue.")
	}

	migrator.Continue(nil)

	// Ensure that immediately after calling Continue,
	// tooLongSincePreviousDetach() returns false.
	if migrator.tooLongSincePreviousDetach() {
		t.Errorf("tooLongSincePreviousDetach() = 'true'; want 'false' immediately after calling Continue()")
	}

	// Ensure that previousDetachThreshold time after calling Continue,
	// tooLongSincePreviousDetach() returns true.
	time.Sleep(migrator.previousDetachThreshold)
	if !migrator.tooLongSincePreviousDetach() {
		t.Errorf("Precondition not met; tooLongSincePreviousDetach() = 'false'; want 'true' after previousDetachThreshold time has elapsed")
	}
}

func TestCalculateMigrationEndpointsToDetach(t *testing.T) {
	testCases := []struct {
		desc                        string
		addEndpoints                map[string]types.NetworkEndpointSet
		removeEndpoints             map[string]types.NetworkEndpointSet
		committedEndpoints          map[string]types.NetworkEndpointSet
		migrationEndpoints          map[string]types.NetworkEndpointSet
		migrator                    *Migrator
		wantCurrentlyMigratingCount int
	}{
		{
			desc:            "less than or equal to 10 (committed + migration) endpoints should only detach 1 at a time",
			addEndpoints:    map[string]types.NetworkEndpointSet{},
			removeEndpoints: map[string]types.NetworkEndpointSet{},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			migrationEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"}, {IP: "7"}, {IP: "8"}, {IP: "9"}, {IP: "10"},
				}...),
			},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 1,
		},
		{
			desc:            "more than 10 (committed + migration) endpoints can detach more than 1 at a time",
			addEndpoints:    map[string]types.NetworkEndpointSet{},
			removeEndpoints: map[string]types.NetworkEndpointSet{},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			migrationEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"}, {IP: "7"}, {IP: "8"}, {IP: "9"}, {IP: "10"},
					{IP: "11"},
				}...),
			},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 2,
		},
		{
			// If there are many endpoints waiting to be attached AND the most recent
			// migration was NOT too long ago, then we will not start any new
			// detachments since we wait for the pending attaches to complete
			desc: "many endpoints are waiting to be attached AND previous migration was quite recent",
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"},
				}...),
			},
			migrationEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "7"},
				}...),
			},
			migrator: func() *Migrator {
				m := newMigratorForTest(t, true)
				m.previousDetach = time.Now().Add(30 * time.Minute) // Future time.
				return m
			}(),
			wantCurrentlyMigratingCount: 0,
		},
		{
			// If there are many endpoints waiting to be attached AND the most recent
			// migration was too long ago BUT we are in error state, then we will not
			// start any new detachments since we wait to get out of error state.
			desc: "many endpoints are waiting to be attached AND previous migration was too long ago BUT in error state",
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"},
				}...),
			},
			migrationEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "7"},
				}...),
			},
			migrator: func() *Migrator {
				m := newMigratorForTest(t, true)
				m.errorStateChecker.(*fakeErrorStateChecker).errorState = true
				return m
			}(),
			wantCurrentlyMigratingCount: 0,
		},
		{
			// If there are many endpoints waiting to be attached BUT the most recent
			// migration was too long ago AND we are not in error state, then we don't
			// want to keep waiting indefinitely for the next detach and we proceed
			// with the detachments.
			desc: "many endpoints are waiting to be attached BUT previous migration was too long ago AND not in error state",
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"},
				}...),
			},
			migrationEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "7"},
				}...),
			},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 1,
		},
		{
			desc: "no detachments started since nothing to migrate",
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "2"},
				}...),
			},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "3"}, {IP: "4"}, {IP: "5"}, {IP: "6"},
				}...),
			},
			migrationEndpoints:          map[string]types.NetworkEndpointSet{},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 0,
		},
		{
			// If our calculations suggest that the number of endpoints to migrate is
			// more than the number of endpoints in any single zone, we should not
			// include endpoints from multiple zones.
			desc:            "endpoints from multiple zones should not be detached at once",
			addEndpoints:    map[string]types.NetworkEndpointSet{},
			removeEndpoints: map[string]types.NetworkEndpointSet{},
			committedEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"},
				}...),
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "5"}, {IP: "6"}, {IP: "7"}, {IP: "8"},
				}...),
			},
			migrationEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "9"}, {IP: "10"},
				}...),
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "11"}, {IP: "12"},
				}...),
			},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			clonedAddEndpoints := cloneZoneNetworkEndpointsMap(tc.addEndpoints)
			clonedRemoveEndpoints := cloneZoneNetworkEndpointsMap(tc.removeEndpoints)
			clonedCommittedEndpoints := cloneZoneNetworkEndpointsMap(tc.committedEndpoints)
			clonedMigrationEndpoints := cloneZoneNetworkEndpointsMap(tc.migrationEndpoints)

			migrationZone := tc.migrator.calculateMigrationEndpointsToDetach(tc.addEndpoints, tc.removeEndpoints, tc.committedEndpoints, tc.migrationEndpoints)

			if tc.wantCurrentlyMigratingCount > 0 && migrationZone == "" {
				t.Fatalf("calculateMigrationEndpointsToDetach(...) returned empty zone which means no migration detachment was started; want %v endpoints to undergo detachment", tc.wantCurrentlyMigratingCount)
			}

			// Ensure that we didn't modify the addEndpoints and committedEndpoints.
			if diff := cmp.Diff(clonedAddEndpoints, tc.addEndpoints); diff != "" {
				t.Errorf("Unexpected diff in addEndpoints; calculateMigrationEndpointsToDetach(...) should not modify addEndpoints; (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(clonedCommittedEndpoints, tc.committedEndpoints); diff != "" {
				t.Errorf("Unexpected diff in committedEndpoints; calculateMigrationEndpointsToDetach(...) should not modify committedEndpoints; (-want +got):\n%s", diff)
			}

			// Ensure that the correct number of endpoints were removed from
			// "migrationEndpoints" and added to "removeEndpoints".
			if gotCurrentlyMigratingCount := endpointsCount(clonedMigrationEndpoints) - endpointsCount(tc.migrationEndpoints); gotCurrentlyMigratingCount != tc.wantCurrentlyMigratingCount {
				t.Errorf("Unexpected number of endpoints removed from migrationEndpoints set; got removed count = %v; want = %v", gotCurrentlyMigratingCount, tc.wantCurrentlyMigratingCount)
			}
			if gotCurrentlyMigratingCount := endpointsCount(tc.removeEndpoints) - endpointsCount(clonedRemoveEndpoints); gotCurrentlyMigratingCount != tc.wantCurrentlyMigratingCount {
				t.Errorf("Unexpected number of endpoints added to removeEndpoints set; got newly added count = %v; want = %v", gotCurrentlyMigratingCount, tc.wantCurrentlyMigratingCount)
			}

			// Ensure that only the endpoints from the migrationZone were modified.
			removedMigrationEndpoints := clonedMigrationEndpoints[migrationZone].Difference(tc.migrationEndpoints[migrationZone])
			if gotCurrentlyMigratingCount := removedMigrationEndpoints.Len(); gotCurrentlyMigratingCount != tc.wantCurrentlyMigratingCount {
				t.Errorf("Unexpected number of endpoints removed from migrationEndpoints[%v] set; got removed count = %v; want = %v", migrationZone, gotCurrentlyMigratingCount, tc.wantCurrentlyMigratingCount)
			}

			// Ensure that all the endpoints removed from migrationEndpoints were
			// added to the removeEndpoints.
			newlyAddedEndpointsWithinRemoveSet := tc.removeEndpoints[migrationZone].Difference(clonedRemoveEndpoints[migrationZone])
			if diff := cmp.Diff(removedMigrationEndpoints, newlyAddedEndpointsWithinRemoveSet); diff != "" {
				t.Errorf("Unexpected diff between the endpoints removed from migrationEndpoints[%v] and endpoints added to removeEndpoints[%v] (-want +got):\n%s", migrationZone, migrationZone, diff)
			}
		})
	}
}

func TestFindAndFilterMigrationEndpoints(t *testing.T) {
	testCases := []struct {
		name                              string
		addEndpoints                      map[string]types.NetworkEndpointSet
		removeEndpoints                   map[string]types.NetworkEndpointSet
		wantMigrationEndpointsInAddSet    map[string]types.NetworkEndpointSet
		wantMigrationEndpointsInRemoveSet map[string]types.NetworkEndpointSet
	}{
		{
			name: "detect multiple migrating endpoints",
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
					{IP: "c", IPv6: "C"},
					{IP: "d"}, // migrating
				}...),
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "e", IPv6: "E"}, // migrating
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "f", IPv6: "F"},
					{IP: "d", IPv6: "D"}, // migrating
				}...),
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "E"}, // migrating
				}...),
			},
			wantMigrationEndpointsInAddSet: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "d"},
				}...),
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "e", IPv6: "E"},
				}...),
			},
			wantMigrationEndpointsInRemoveSet: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"},
					{IP: "d", IPv6: "D"},
				}...),
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "E"},
				}...),
			},
		},
		{
			name: "partial IP change without stack change is not considered migrating",
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", Port: "B"},
				}...),
			},
			wantMigrationEndpointsInAddSet:    map[string]types.NetworkEndpointSet{},
			wantMigrationEndpointsInRemoveSet: map[string]types.NetworkEndpointSet{},
		},
		{
			name: "difference in port or node is not considered migrating",
			addEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A", Port: "80"},
					{IP: "b", Node: "node2"},
				}...),
			},
			removeEndpoints: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", Port: "81"},
					{IP: "b", IPv6: "B", Node: "node1"},
				}...),
			},
			wantMigrationEndpointsInAddSet:    map[string]types.NetworkEndpointSet{},
			wantMigrationEndpointsInRemoveSet: map[string]types.NetworkEndpointSet{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotMigrationEndpointsInAddSet, gotMigrationEndpointsInRemoveSet := findAndFilterMigrationEndpoints(tc.addEndpoints, tc.removeEndpoints)

			if diff := cmp.Diff(tc.wantMigrationEndpointsInAddSet, gotMigrationEndpointsInAddSet); diff != "" {
				t.Errorf("findAndFilterMigrationEndpoints(tc.addEndpoints, tc.removeEndpoints) returned unexpected diff for migrationEndpointsInAddSet (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tc.wantMigrationEndpointsInRemoveSet, gotMigrationEndpointsInRemoveSet); diff != "" {
				t.Errorf("findAndFilterMigrationEndpoints(tc.addEndpoints, tc.removeEndpoints) returned unexpected diff for migrationEndpointsInRemoveSet (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMoveEndpoint(t *testing.T) {
	testCases := []struct {
		name        string
		endpoint    types.NetworkEndpoint
		inputSource map[string]types.NetworkEndpointSet
		inputDest   map[string]types.NetworkEndpointSet
		wantSource  map[string]types.NetworkEndpointSet
		wantDest    map[string]types.NetworkEndpointSet
		zone        string
		wantSuccess bool
	}{
		{
			name:     "completely valid input, shoud successfully move",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "b", IPv6: "B"},
				}...),
			},
			inputDest: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b", IPv6: "B"},
				}...),
			},
			wantDest: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone:        "zone1",
			wantSuccess: true,
		},
		{
			name:     "zone does not exist in source",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			inputDest: map[string]types.NetworkEndpointSet{
				"zone3": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			wantDest: map[string]types.NetworkEndpointSet{
				"zone3": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone: "zone3",
		},
		{
			name:     "zone does not exist in destination, shoud successfully move",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "b", IPv6: "B"},
				}...),
			},
			inputDest: map[string]types.NetworkEndpointSet{
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b", IPv6: "B"},
				}...),
			},
			wantDest: map[string]types.NetworkEndpointSet{
				"zone1": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
				"zone2": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone:        "zone1",
			wantSuccess: true,
		},
		{
			name:     "source is nil",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputDest: map[string]types.NetworkEndpointSet{
				"zone3": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantDest: map[string]types.NetworkEndpointSet{
				"zone3": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone: "zone3",
		},
		{
			name:     "destination is nil",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[string]types.NetworkEndpointSet{
				"zone3": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[string]types.NetworkEndpointSet{
				"zone3": types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			zone: "zone3",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotSuccess := moveEndpoint(tc.endpoint, tc.inputSource, tc.inputDest, tc.zone)

			if gotSuccess != tc.wantSuccess {
				t.Errorf("moveEndpoint(%v, ...) = %v, want = %v", tc.endpoint, gotSuccess, tc.wantSuccess)
			}
			if diff := cmp.Diff(tc.wantSource, tc.inputSource); diff != "" {
				t.Errorf("moveEndpoint(%v, ...) returned unexpected diff for source (-want +got):\n%s", tc.endpoint, diff)
			}
			if diff := cmp.Diff(tc.wantDest, tc.inputDest); diff != "" {
				t.Errorf("moveEndpoint(%v, ...) returned unexpected diff for destination (-want +got):\n%s", tc.endpoint, diff)
			}
		})
	}
}

func cloneZoneNetworkEndpointsMap(m map[string]types.NetworkEndpointSet) map[string]types.NetworkEndpointSet {
	clone := make(map[string]types.NetworkEndpointSet)
	for zone, endpointSet := range m {
		clone[zone] = types.NewNetworkEndpointSet(endpointSet.List()...)
	}
	return clone
}

func newMigratorForTest(t *testing.T, enableDualStackNEG bool) *Migrator {
	logger, _ := ktesting.NewTestContext(t)
	return NewMigrator(enableDualStackNEG, &fakeSyncable{}, types.NegSyncerKey{}, metrics.FakeSyncerMetrics(), &fakeErrorStateChecker{}, logger)
}
