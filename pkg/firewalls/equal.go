package firewalls

import (
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
func Equal(a, b *compute.Firewall, skipDescription bool) bool {
	switch {
	case a == nil && b == nil:
		return true
	case (a == nil && b != nil) || (a != nil && b == nil):
		return false
	case !equalAllowRules(a.Allowed, b.Allowed):
		return false
	case !utils.EqualStringSets(a.DestinationRanges, b.DestinationRanges):
		return false
	case !utils.EqualStringSets(a.SourceRanges, b.SourceRanges):
		return false
	case !utils.EqualStringSets(a.TargetTags, b.TargetTags):
		return false
	case !skipDescription && a.Description != b.Description:
		return false
	default:
		return true
	}
}

// EqualAllowRules compares the FirewallAllowed arrays and returns whether they are equal.
//
// Arrays are equal when they expose the same set of ports for each protocol.
func equalAllowRules(a, b []*compute.FirewallAllowed) bool {
	am := portsAllowedPerProtocol(a)
	bm := portsAllowedPerProtocol(b)

	return reflect.DeepEqual(am, bm)
}

// We list all the ports, since FirewallAllowed allows to specify ranges using strings like 10-13, and this should be equal to 10,11,12,13
func portsAllowedPerProtocol(a []*compute.FirewallAllowed) map[string]map[int]struct{} {
	portsOpenForProtocol := make(map[string]map[int]struct{})
	for _, rule := range a {
		protocol := strings.ToLower(rule.IPProtocol)
		if _, ok := portsOpenForProtocol[protocol]; !ok {
			portsOpenForProtocol[protocol] = make(map[int]struct{})
		}

		for _, portStr := range rule.Ports {
			start, end := parsePort(portStr)
			for i := start; i <= end; i++ {
				portsOpenForProtocol[protocol][i] = struct{}{}
			}
		}
	}
	return portsOpenForProtocol
}

// parsePort returns [start, end] range (inclusive)
// handles:
// * single port like "12"
// * ports range like "12-15"
func parsePort(p string) (start, end int) {
	if p == "" {
		return 0, -1
	}

	splits := strings.Split(p, "-")
	if len(splits) == 1 {
		num, _ := strconv.Atoi(splits[0])
		return num, num
	}

	start, _ = strconv.Atoi(splits[0])
	end, _ = strconv.Atoi(splits[1])
	return
}
