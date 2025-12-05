package denytest

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"google.golang.org/api/compute/v1"
)

// firewallTracker is used to check if there are multiple firewalls
// on the same IP or IP range that have conflicting priority, which
// would result in blocking all traffic.
type firewallTracker struct {
	// firewalls contains all firewalls for IP specified in the key
	// for IPv6 ranges we store just the prefix
	firewalls map[string]map[string]*compute.Firewall

	mu sync.Mutex
}

// patch will return an error if there is a situation that modifying fw
// would
func (f *firewallTracker) patch(fw *compute.Firewall) error {
	defer f.mu.Unlock()
	f.mu.Lock()

	if f.firewalls == nil {
		f.firewalls = make(map[string]map[string]*compute.Firewall)
	}

	if len(fw.DestinationRanges) != 1 {
		return fmt.Errorf("not implemented count of destination ranges %d", len(fw.DestinationRanges))
	}

	key := fw.DestinationRanges[0]
	key = strings.TrimSuffix(key, "/96")

	if f.firewalls[key] == nil {
		f.firewalls[key] = make(map[string]*compute.Firewall)
	}
	f.firewalls[key][fw.Name] = fw

	for key, other := range f.firewalls[key] {
		if fw.Name == other.Name {
			continue
		}
		if areBlocked(fw, other) {
			return fmt.Errorf(
				"two firewalls block each other on %q: %s (priority %d) and %s (priority %d)",
				key, fw.Name, fw.Priority, other.Name, other.Priority,
			)
		}
	}

	return nil
}

func (f *firewallTracker) delete(name string) {
	defer f.mu.Unlock()
	f.mu.Lock()

	if f.firewalls == nil {
		return
	}

	// this could be done a tad quicker with an additional map
	// but this should be fast enough for the test
	for _, fw := range f.firewalls {
		delete(fw, name)
	}
}

// areBlocked only works if fw1 and fw2 are using the same
// destination range, direction, etc
func areBlocked(fw1, fw2 *compute.Firewall) bool {
	if fw1 == nil || fw2 == nil {
		return false
	}

	if len(fw2.Denied) > 0 {
		fw1, fw2 = fw2, fw1
	}

	// Both are deny or allow - won't block themselves
	if len(fw1.Denied) == 0 || len(fw2.Allowed) == 0 {
		return false
	}

	// deny takes precedence over allow if they have the same priority
	return fw1.Priority <= fw2.Priority
}

func TestFirewallTrackerDetectingBlocking(t *testing.T) {
	t.Parallel()
	allowed := []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{"1", "2", "3"}}}
	denied := []*compute.FirewallDenied{{IPProtocol: "ALL"}}

	testCases := []struct {
		desc    string
		ops     func() error
		wantErr bool
	}{
		{
			desc: "allow_modified_to_999",
			ops: func() error {
				tracker := &firewallTracker{}
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv4},
					Priority:          999,
				}); err != nil {
					return err
				}
				if err := tracker.patch(&compute.Firewall{
					Name:              "b",
					Denied:            denied,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				return nil
			},
			wantErr: false,
		},
		{
			desc: "allow_patched_with_the_same_priority_as_deny",
			ops: func() error {
				tracker := &firewallTracker{}
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv4},
					Priority:          999,
				}); err != nil {
					return err
				}
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				if err := tracker.patch(&compute.Firewall{
					Name:              "b",
					Denied:            denied,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				return nil
			},
			wantErr: true,
		},
		{
			desc: "deny_first_followed_by_allow",
			ops: func() error {
				tracker := &firewallTracker{}
				if err := tracker.patch(&compute.Firewall{
					Name:              "b",
					Denied:            denied,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				return nil
			},
			wantErr: true,
		},
		{
			desc: "ipv6_range_denied",
			ops: func() error {
				tracker := &firewallTracker{}
				if err := tracker.patch(&compute.Firewall{
					Name:              "b",
					Denied:            denied,
					DestinationRanges: []string{ipv6Range},
					Priority:          1000,
				}); err != nil {
					return err
				}
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv6},
					Priority:          1000,
				}); err != nil {
					return err
				}
				return nil
			},
			wantErr: true,
		},
		{
			desc: "ipv6_range_allowed",
			ops: func() error {
				tracker := &firewallTracker{}
				if err := tracker.patch(&compute.Firewall{
					Name:              "b",
					Denied:            denied,
					DestinationRanges: []string{ipv6Range},
					Priority:          1000,
				}); err != nil {
					return err
				}
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv6},
					Priority:          999,
				}); err != nil {
					return err
				}
				return nil
			},
			wantErr: false,
		},
		{
			desc: "delete_removes_firewall",
			ops: func() error {
				tracker := &firewallTracker{}
				if err := tracker.patch(&compute.Firewall{
					Name:              "b",
					Denied:            denied,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				tracker.delete("b")
				if err := tracker.patch(&compute.Firewall{
					Name:              "a",
					Allowed:           allowed,
					DestinationRanges: []string{ipv4},
					Priority:          1000,
				}); err != nil {
					return err
				}
				return nil
			},
			wantErr: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()
			if err := tC.ops(); (err != nil) != tC.wantErr {
				t.Errorf("firewallTracker.patch() error = %v, wantErr %v", err, tC.wantErr)
			}
		})
	}
}
