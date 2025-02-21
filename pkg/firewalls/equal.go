package firewalls

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/utils"
)

// Equal compares two Firewalls and returns true if they are the same in context of LoadBalancers.
//
// It will always compare Allow rules, DestinationRanges, SourceRanges and TargetTags.
// If skipDescription is set to false it will also compare contents of Description.
//
// Returns error when there is a port definition that isn't an int or range (int-int)
func Equal(a, b *compute.Firewall, skipDescription bool) (bool, error) {
	switch {
	case a == nil && b == nil:
		return true, nil
	case (a == nil && b != nil) || (a != nil && b == nil):
		return false, nil
	case !utils.EqualStringSets(a.DestinationRanges, b.DestinationRanges):
		return false, nil
	case !utils.EqualStringSets(a.SourceRanges, b.SourceRanges):
		return false, nil
	case !utils.EqualStringSets(a.TargetTags, b.TargetTags):
		return false, nil
	case !skipDescription && a.Description != b.Description:
		return false, nil
	default:
		return equalAllowRules(a.Allowed, b.Allowed)
	}
}

// EqualAllowRules compares the FirewallAllowed arrays and returns whether they are equal.
//
// Arrays are equal when they expose the same set of ports for each protocol.
//
// Returns error when there is a port definition that isn't an int or range (int-int)
func equalAllowRules(a, b []*compute.FirewallAllowed) (bool, error) {
	am, err := portsAllowedPerProtocol(a)
	if err != nil {
		return false, err
	}

	bm, err := portsAllowedPerProtocol(b)
	if err != nil {
		return false, err
	}

	return reflect.DeepEqual(am, bm), nil
}

// We list all the ports, since FirewallAllowed allows to specify ranges using strings like 10-13, and this should be equal to 10,11,12,13
//
// Returns error when there is a port definition that isn't an int or range (int-int)
func portsAllowedPerProtocol(a []*compute.FirewallAllowed) (map[string]map[int]struct{}, error) {
	portsOpenForProtocol := make(map[string]map[int]struct{})
	for _, rule := range a {
		protocol := strings.ToLower(rule.IPProtocol)
		if _, ok := portsOpenForProtocol[protocol]; !ok {
			portsOpenForProtocol[protocol] = make(map[int]struct{})
		}

		for _, portStr := range rule.Ports {
			start, end, err := parsePort(portStr)
			if err != nil {
				return nil, err
			}
			for i := start; i <= end; i++ {
				portsOpenForProtocol[protocol][i] = struct{}{}
			}
		}
	}
	return portsOpenForProtocol, nil
}

// parsePort returns [start, end] range (inclusive)
// handles:
// * single port like "12"
// * ports range like "12-15"
func parsePort(p string) (start, end int, err error) {
	if p == "" {
		return 0, -1, fmt.Errorf("failed to parse a port: empty string")
	}

	splits := strings.Split(p, "-")
	if len(splits) == 1 {
		num, err := strconv.Atoi(splits[0])
		if err != nil {
			return 0, -1, fmt.Errorf("failed to parse a port `%s`: %w", splits[0], err)
		}
		return num, num, nil
	}

	if len(splits) != 2 {
		return 0, -1, fmt.Errorf("failed to parse a port `%s`: invalid format", p)
	}

	if start, err = strconv.Atoi(splits[0]); err != nil {
		return 0, -1, fmt.Errorf("failed to parse a port `%s`: %w", splits[0], err)
	}
	if end, err = strconv.Atoi(splits[1]); err != nil {
		return 0, -1, fmt.Errorf("failed to parse a port `%s`: %w", splits[0], err)
	}

	return start, end, nil
}
