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

func cloneZoneNetworkEndpointsMap(m map[string]types.NetworkEndpointSet) map[string]types.NetworkEndpointSet {
	clone := make(map[string]types.NetworkEndpointSet)
	for zone, endpointSet := range m {
		clone[zone] = types.NewNetworkEndpointSet(endpointSet.List()...)
	}
	return clone
}
