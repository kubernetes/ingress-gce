package dualstack

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2/ktesting"
	"k8s.io/utils/clock"
	clocktesting "k8s.io/utils/clock/testing"
)

const defaultTestSubnet = "default"

func TestFilter(t *testing.T) {
	testCases := []struct {
		desc                string
		migrator            *Migrator
		addEndpoints        map[types.NEGLocation]types.NetworkEndpointSet
		removeEndpoints     map[types.NEGLocation]types.NetworkEndpointSet
		committedEndpoints  map[types.NEGLocation]types.NetworkEndpointSet
		wantAddEndpoints    map[types.NEGLocation]types.NetworkEndpointSet
		wantRemoveEndpoints map[types.NEGLocation]types.NetworkEndpointSet
		wantMigrationZone   bool
	}{
		{
			desc: "paused migrator should only filter migration endpoints and not start detachment",
			migrator: func() *Migrator {
				m := newMigratorForTest(t, true)
				m.Pause()
				return m
			}(),
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "c", IPv6: "C"},
				}...),
			},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "D"},
				}...),
			},
			wantAddEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b"},
				}...),
			},
			wantRemoveEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					// Migration-endpoints were filtered out but no new migration
					// detachment was started.
					{IP: "c", IPv6: "C"},
				}...),
			},
		},
		{
			desc:     "unpaused migrator should filter migration endpoints AND also start detachment",
			migrator: newMigratorForTest(t, true),
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "c", IPv6: "C"},
				}...),
			},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "D"},
				}...),
			},
			wantAddEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b"},
				}...),
			},
			wantRemoveEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // Migration detachment started.
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantMigrationZone: true,
		},
		{
			desc:     "migrator should do nothing if enableDualStack is false",
			migrator: newMigratorForTest(t, false),
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "c", IPv6: "C"},
				}...),
			},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "D"},
				}...),
			},
			wantAddEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
				}...),
			},
			wantRemoveEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
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

			if tc.wantMigrationZone && gotMigrationZone.Zone == "" && gotMigrationZone.Subnet == "" {
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

// TestFilter_FunctionalTest attempts to test the overall functioning of the
// Filter() method. It will perform the following sequence of actions:
//
//  1. Generate some dual-stack endpoints and their single-stack counterparts
//     which when passed to the Filter() method result in starting migration.
//  2. Use the value returned by the Filter() method to imitate NEG
//     attach/detach operations.
//  3. Invoke Filter() multiple times and then redo step 2 each time.
//
// Pass or failure of the tests will depend on:
//   - whether the desired number of endpoints got detached
//   - and, whether the endpoints got detached within the stipulated number of
//     Filter() invocations.
func TestFilter_FunctionalTest(t *testing.T) {
	testCases := []struct {
		desc     string
		migrator *Migrator
		// The number of endpoints that are initially in the NEG. The test will
		// generate these many number of endpoints.
		initialNEGEndpointsCount int
		// The number of zones among which the initial number of endpoints are
		// distributed. The test will distribute the generated endpoints among these
		// many zones.
		zonesCount int
		// The number of times Filter() should be invoked. This imitates the number
		// of times Sync() for the parent syncer would get called. This value MAY
		// need to be reconfigured in the tests if the value of
		// defaultFractionOfMigratingEndpoints changes significantly.
		syncCount int
		// attachSucceeds denotes whether the test should imitate a successful NEG
		// attach operation.
		attachSucceeds bool
		// errorState configures the value returned by the errorStateChecker. True
		// means that we are in error state.
		errorState bool
		// wantAllDetached tells whether all endpoints should be expected to get
		// detached from the NEG.
		wantAllDetached bool
	}{
		{
			desc:                     "attaches are succeeding, all endpoint should get detached",
			migrator:                 newMigratorForTest(t, true),
			initialNEGEndpointsCount: 10,
			zonesCount:               3,
			syncCount:                10,
			attachSucceeds:           true,
			errorState:               false,
			wantAllDetached:          true,
		},
		{
			desc:                     "many attaches pending due to churn BUT we are not in degraded mode, all endpoints will get detached after enough syncs",
			migrator:                 newMigratorForTest(t, true),
			initialNEGEndpointsCount: 10,
			zonesCount:               3,
			syncCount:                50,    // relatively large sync count
			attachSucceeds:           false, // Many attaches pending because of churn is imitated by failing attach operations.
			errorState:               false,
			wantAllDetached:          true,
		},
		{
			desc:                     "attach is failing AND we are in degraded mode, all endpoints will NOT get detached even after many syncs",
			migrator:                 newMigratorForTest(t, true),
			initialNEGEndpointsCount: 10,
			zonesCount:               3,
			syncCount:                50, // relatively large sync count
			attachSucceeds:           false,
			errorState:               true,
			wantAllDetached:          false,
		},
		{
			desc:                     "attach is succeeding BUT we are in degraded mode (for another reason), all endpoints WILL get detached NORMALLY",
			migrator:                 newMigratorForTest(t, true),
			initialNEGEndpointsCount: 10,
			zonesCount:               3,
			syncCount:                10,
			attachSucceeds:           true,
			errorState:               true,
			wantAllDetached:          true,
		},
		{
			desc:                     "larger number of endpoints and zones, attaches are succeeding, all endpoint should get detached",
			migrator:                 newMigratorForTest(t, true),
			initialNEGEndpointsCount: 1000,
			zonesCount:               10,
			syncCount:                30,
			attachSucceeds:           true,
			errorState:               false,
			wantAllDetached:          true,
		},
		{
			desc:                     "50 endpoints and 50 zones, should require 50 syncs to detach all endpoints",
			migrator:                 newMigratorForTest(t, true),
			initialNEGEndpointsCount: 50,
			zonesCount:               50,
			syncCount:                50,
			attachSucceeds:           true,
			errorState:               false,
			wantAllDetached:          true,
		},
		{
			desc:                     "50 endpoints and 50 zones, all endpoints should NOT get detached if syncs are less than 50",
			migrator:                 newMigratorForTest(t, true),
			initialNEGEndpointsCount: 50,
			zonesCount:               50,
			syncCount:                49,
			attachSucceeds:           true,
			errorState:               false,
			wantAllDetached:          false,
		},
	}

	// doNEGAttachDetach will imitate NEG Attach and Detach operations. It will
	// perform the attachment of filteredAddEndpoints and detachment of
	// filteredRemoveEndpoints.
	//
	// If shouldAttach is false, no attachments will be done.
	doNEGAttachDetach := func(addEndpoints, removeEndpoints, committedEndpoints, filteredAddEndpoints, filteredRemoveEndpoints map[types.NEGLocation]types.NetworkEndpointSet, shouldAttach bool) {
		// Endpoints are detachment by deleting them from the removeEndpoints set.
		for zone, endpointSet := range filteredRemoveEndpoints {
			removeEndpoints[zone].Delete(endpointSet.List()...)
		}
		if !shouldAttach {
			return
		}
		// Endpoints are attached by first deleting them from addEndpoints set and
		// then adding them to the committedEndpoints.
		for zone, endpointSet := range filteredAddEndpoints {
			addEndpoints[zone].Delete(endpointSet.List()...)
			committedEndpoints[zone].Insert(endpointSet.List()...)
		}
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			/////////////////////////////////////////////////////////////////////////
			// Step 1: Generate endpoints to be used as input.
			/////////////////////////////////////////////////////////////////////////

			addEndpoints := make(map[types.NEGLocation]types.NetworkEndpointSet)       // Contains dual-stack endpoints which are to be added to the NEG.
			removeEndpoints := make(map[types.NEGLocation]types.NetworkEndpointSet)    // Contains single-stack endpoints which are to be removed from the NEG.
			committedEndpoints := make(map[types.NEGLocation]types.NetworkEndpointSet) // Initially empty.
			for i := 0; i < tc.initialNEGEndpointsCount; i++ {
				negLocation := types.NEGLocation{Zone: fmt.Sprintf("zone-%v", i%tc.zonesCount), Subnet: defaultTestSubnet}
				ipv4 := fmt.Sprintf("ipv4-%v", 2*i+1)
				ipv6 := fmt.Sprintf("ipv6-%v", 2*i+2)
				if addEndpoints[negLocation] == nil {
					addEndpoints[negLocation] = types.NewNetworkEndpointSet()
					removeEndpoints[negLocation] = types.NewNetworkEndpointSet()
					committedEndpoints[negLocation] = types.NewNetworkEndpointSet()
				}
				addEndpoints[negLocation].Insert(types.NetworkEndpoint{IP: ipv4, IPv6: ipv6})
				removeEndpoints[negLocation].Insert(types.NetworkEndpoint{IP: ipv4})
			}

			tc.migrator.errorStateChecker.(*fakeErrorStateChecker).errorState = tc.errorState

			/////////////////////////////////////////////////////////////////////////
			// Step 2: Invoke Filter(), followed by NEG attach/detach multiple times.
			/////////////////////////////////////////////////////////////////////////

			for i := 0; i < tc.syncCount; i++ {
				filteredAddEndpoints := cloneZoneNetworkEndpointsMap(addEndpoints)
				filteredRemoveEndpoints := cloneZoneNetworkEndpointsMap(removeEndpoints)

				migrationZone := tc.migrator.Filter(filteredAddEndpoints, filteredRemoveEndpoints, committedEndpoints)

				doNEGAttachDetach(addEndpoints, removeEndpoints, committedEndpoints, filteredAddEndpoints, filteredRemoveEndpoints, tc.attachSucceeds)
				fakeClock := tc.migrator.clock.(*clocktesting.FakeClock)
				if migrationZone.Zone != "" || migrationZone.Subnet != "" {
					// Update the time of the last successful detach.
					tc.migrator.previousDetach = fakeClock.Now()
				}
				// Move the clock ahead by some time.
				fakeClock.Step(tc.migrator.migrationWaitDuration)
				t.Logf("fakeClock: Time.Now()=%v", fakeClock.Now())
			}

			/////////////////////////////////////////////////////////////////////////
			// Step 3: Verify the number of endpoints detached.
			/////////////////////////////////////////////////////////////////////////

			if tc.wantAllDetached {
				if count := endpointsCount(removeEndpoints); count != 0 {
					t.Errorf("Got %v endpoints still remaining in removeEndpoints after %v Filter() invocations; want 0 endpoints remaining", count, tc.syncCount)
				}
			} else {
				if count := endpointsCount(removeEndpoints); count == 0 {
					t.Errorf("Got all %v endpoints removed from removeEndpoints after %v Filter() invocations; want non-zero endpoints still remaining in removeEndpoints", tc.initialNEGEndpointsCount, tc.syncCount)
				}
			}
		})
	}
}

func TestPause(t *testing.T) {
	addEndpoints := map[types.NEGLocation]types.NetworkEndpointSet{
		{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
			{IP: "a", IPv6: "A"}, // migrating
			{IP: "b"},
			{IP: "c", IPv6: "C"},
			{IP: "d"}, // migrating
		}...),
	}
	removeEndpoints := map[types.NEGLocation]types.NetworkEndpointSet{
		{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
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
	migrator.Filter(clonedAddEndpoints, clonedRemoveEndpoints, map[types.NEGLocation]types.NetworkEndpointSet{})
	possibleMigrationDetachments := []types.NetworkEndpoint{
		{IP: "a"},            // migrating
		{IP: "d", IPv6: "D"}, // migrating
	}
	if !clonedRemoveEndpoints[types.NEGLocation{Zone: "zone1", Subnet: defaultTestSubnet}].HasAny(possibleMigrationDetachments...) {
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
	migrator.Filter(addEndpoints, removeEndpoints, map[types.NEGLocation]types.NetworkEndpointSet{})
	wantRemoveEndpoints := map[types.NEGLocation]types.NetworkEndpointSet{
		types.NEGLocation{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
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
	migrator := newMigratorForTest(t, true)
	migrator.paused = true
	syncable := migrator.syncer.(*fakeSyncable)

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

	migrator := newMigratorForTest(t, true)
	migrator.paused = true
	migrator.clock = clock.RealClock{} // This test needs to run with a real clock.
	migrator.migrationWaitDuration = 10 * time.Millisecond
	syncable := migrator.syncer.(*fakeSyncable)

	migrator.Continue(nil)

	if !migrator.isPaused() {
		t.Errorf("Continue(nil) most likely unpaused the migrator before returning; want migrator to be unpaused after %v", migrator.migrationWaitDuration)
	}

	// Sleep until we can expect a sync.
	time.Sleep(migrator.migrationWaitDuration)

	if err := wait.PollImmediate(migrator.migrationWaitDuration/10, migrator.migrationWaitDuration, func() (done bool, err error) {
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

	migrator := newMigratorForTest(t, true)
	migrator.paused = true
	syncable := migrator.syncer.(*fakeSyncable)

	for i := 0; i < 10; i++ {
		go migrator.Continue(nil)
	}

	// We wait for some time to ensure that if multiple Continues attempted to
	// resync, atleast one of those go routines finished.
	time.Sleep(10 * time.Millisecond)
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

	migrator := newMigratorForTest(t, true)
	migrator.paused = true
	migrator.clock = clock.RealClock{} // This test needs to run with a real clock.
	migrator.previousDetachThreshold = 20 * time.Millisecond

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
		addEndpoints                map[types.NEGLocation]types.NetworkEndpointSet
		removeEndpoints             map[types.NEGLocation]types.NetworkEndpointSet
		committedEndpoints          map[types.NEGLocation]types.NetworkEndpointSet
		migrationEndpoints          map[types.NEGLocation]types.NetworkEndpointSet
		migrator                    *Migrator
		wantCurrentlyMigratingCount int
	}{
		{
			desc:            "less than or equal to 10 (committed + migration) endpoints should only detach 1 at a time",
			addEndpoints:    map[types.NEGLocation]types.NetworkEndpointSet{},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			migrationEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"}, {IP: "7"}, {IP: "8"}, {IP: "9"}, {IP: "10"},
				}...),
			},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 1,
		},
		{
			desc:            "more than 10 (committed + migration) endpoints can detach more than 1 at a time",
			addEndpoints:    map[types.NEGLocation]types.NetworkEndpointSet{},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			migrationEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
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
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"},
				}...),
			},
			migrationEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
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
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"},
				}...),
			},
			migrationEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
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
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"}, {IP: "5"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "6"},
				}...),
			},
			migrationEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "7"},
				}...),
			},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 1,
		},
		{
			desc: "no detachments started since nothing to migrate",
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "2"},
				}...),
			},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "3"}, {IP: "4"}, {IP: "5"}, {IP: "6"},
				}...),
			},
			migrationEndpoints:          map[types.NEGLocation]types.NetworkEndpointSet{},
			migrator:                    newMigratorForTest(t, true),
			wantCurrentlyMigratingCount: 0,
		},
		{
			// If our calculations suggest that the number of endpoints to migrate is
			// more than the number of endpoints in any single zone, we should not
			// include endpoints from multiple zones.
			desc:            "endpoints from multiple zones should not be detached at once",
			addEndpoints:    map[types.NEGLocation]types.NetworkEndpointSet{},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{},
			committedEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "1"}, {IP: "2"}, {IP: "3"}, {IP: "4"},
				}...),
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "5"}, {IP: "6"}, {IP: "7"}, {IP: "8"},
				}...),
			},
			migrationEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "9"}, {IP: "10"},
				}...),
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
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

			if tc.wantCurrentlyMigratingCount > 0 && (migrationZone.Zone == "" && migrationZone.Subnet == "") {
				t.Fatalf("calculateMigrationEndpointsToDetach(...) returned empty zone and subnet which means no migration detachment was started; want %v endpoints to undergo detachment", tc.wantCurrentlyMigratingCount)
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
		addEndpoints                      map[types.NEGLocation]types.NetworkEndpointSet
		removeEndpoints                   map[types.NEGLocation]types.NetworkEndpointSet
		wantMigrationEndpointsInAddSet    map[types.NEGLocation]types.NetworkEndpointSet
		wantMigrationEndpointsInRemoveSet map[types.NEGLocation]types.NetworkEndpointSet
	}{
		{
			name: "detect multiple migrating endpoints",
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"}, // migrating
					{IP: "b"},
					{IP: "c", IPv6: "C"},
					{IP: "d"}, // migrating
				}...),
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "e", IPv6: "E"}, // migrating
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"}, // migrating
					{IP: "f", IPv6: "F"},
					{IP: "d", IPv6: "D"}, // migrating
				}...),
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "E"}, // migrating
				}...),
			},
			wantMigrationEndpointsInAddSet: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "d"},
				}...),
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "e", IPv6: "E"},
				}...),
			},
			wantMigrationEndpointsInRemoveSet: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a"},
					{IP: "d", IPv6: "D"},
				}...),
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IPv6: "E"},
				}...),
			},
		},
		{
			name: "partial IP change without stack change is not considered migrating",
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", Port: "B"},
				}...),
			},
			wantMigrationEndpointsInAddSet:    map[types.NEGLocation]types.NetworkEndpointSet{},
			wantMigrationEndpointsInRemoveSet: map[types.NEGLocation]types.NetworkEndpointSet{},
		},
		{
			name: "difference in port or node is not considered migrating",
			addEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A", Port: "80"},
					{IP: "b", Node: "node2"},
				}...),
			},
			removeEndpoints: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", Port: "81"},
					{IP: "b", IPv6: "B", Node: "node1"},
				}...),
			},
			wantMigrationEndpointsInAddSet:    map[types.NEGLocation]types.NetworkEndpointSet{},
			wantMigrationEndpointsInRemoveSet: map[types.NEGLocation]types.NetworkEndpointSet{},
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
		inputSource map[types.NEGLocation]types.NetworkEndpointSet
		inputDest   map[types.NEGLocation]types.NetworkEndpointSet
		wantSource  map[types.NEGLocation]types.NetworkEndpointSet
		wantDest    map[types.NEGLocation]types.NetworkEndpointSet
		negLocation types.NEGLocation
		wantSuccess bool
	}{
		{
			name:     "completely valid input, shoud successfully move",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "b", IPv6: "B"},
				}...),
			},
			inputDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b", IPv6: "B"},
				}...),
			},
			wantDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "c", IPv6: "C"},
				}...),
			},
			negLocation: types.NEGLocation{Zone: "zone1", Subnet: defaultTestSubnet},
			wantSuccess: true,
		},
		{
			name:     "zone does not exist in source",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			inputDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone3", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
			},
			wantDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone3", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			negLocation: types.NEGLocation{Zone: "zone3", Subnet: defaultTestSubnet},
		},
		{
			name:     "zone does not exist in destination, shoud successfully move",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
					{IP: "b", IPv6: "B"},
				}...),
			},
			inputDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "b", IPv6: "B"},
				}...),
			},
			wantDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone1", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "a", IPv6: "A"},
				}...),
				{Zone: "zone2", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			negLocation: types.NEGLocation{Zone: "zone1", Subnet: defaultTestSubnet},
			wantSuccess: true,
		},
		{
			name:     "source is nil",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone3", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantDest: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone3", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			negLocation: types.NEGLocation{Zone: "zone3", Subnet: defaultTestSubnet},
		},
		{
			name:     "destination is nil",
			endpoint: types.NetworkEndpoint{IP: "a", IPv6: "A"},
			inputSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone3", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			wantSource: map[types.NEGLocation]types.NetworkEndpointSet{
				{Zone: "zone3", Subnet: defaultTestSubnet}: types.NewNetworkEndpointSet([]types.NetworkEndpoint{
					{IP: "c", IPv6: "C"},
				}...),
			},
			negLocation: types.NEGLocation{Zone: "zone3", Subnet: defaultTestSubnet},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotSuccess := moveEndpoint(tc.endpoint, tc.inputSource, tc.inputDest, tc.negLocation)

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

func cloneZoneNetworkEndpointsMap(m map[types.NEGLocation]types.NetworkEndpointSet) map[types.NEGLocation]types.NetworkEndpointSet {
	clone := make(map[types.NEGLocation]types.NetworkEndpointSet)
	for negLocation, endpointSet := range m {
		clone[negLocation] = types.NewNetworkEndpointSet(endpointSet.List()...)
	}
	return clone
}

func newMigratorForTest(t *testing.T, enableDualStackNEG bool) *Migrator {
	logger, _ := ktesting.NewTestContext(t)
	m := NewMigrator(enableDualStackNEG, &fakeSyncable{}, types.NegSyncerKey{}, metricscollector.FakeSyncerMetrics(), &fakeErrorStateChecker{}, logger)
	m.clock = clocktesting.NewFakeClock(time.Now())
	return m
}
