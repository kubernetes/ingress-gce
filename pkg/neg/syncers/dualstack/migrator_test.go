package dualstack

import (
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
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
			desc:     "migrator should do nothing if enableDualStack is false",
			migrator: &Migrator{enableDualStack: false},
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

	migrator := newMigratorForTest(true)

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

func newMigratorForTest(enableDualStackNEG bool) *Migrator {
	return NewMigrator(enableDualStackNEG, &fakeSyncable{}, klog.Background())
}
