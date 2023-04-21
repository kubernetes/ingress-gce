package dualstack

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/ingress-gce/pkg/neg/types"
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
