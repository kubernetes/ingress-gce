package forwardingrules

import (
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"

	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
)

// EqualIPv4 compares IPv4 firewall rules and returns true if there are identical from LB point of view.
// Returns an error when `.BackendService` url is invalid.
func EqualIPv4(fr1, fr2 *composite.ForwardingRule) (bool, error) {
	id1, err := cloud.ParseResourceURL(fr1.BackendService)
	if err != nil {
		return false, fmt.Errorf("Equal(): failed to parse backend resource URL from existing FR, err - %w", err)
	}
	id2, err := cloud.ParseResourceURL(fr2.BackendService)
	if err != nil {
		return false, fmt.Errorf("Equal(): failed to parse backend resource URL from wanted FR, err - %w", err)
	}
	return fr1.IPAddress == fr2.IPAddress &&
		strings.EqualFold(fr1.IPProtocol, fr2.IPProtocol) &&
		fr1.LoadBalancingScheme == fr2.LoadBalancingScheme &&
		equalPorts(fr1.Ports, fr2.Ports, fr1.PortRange, fr2.PortRange) &&
		utils.EqualCloudResourceIDs(id1, id2) &&
		fr1.AllowGlobalAccess == fr2.AllowGlobalAccess &&
		fr1.AllPorts == fr2.AllPorts &&
		equalResourcePaths(fr1.Subnetwork, fr2.Subnetwork) &&
		equalResourcePaths(fr1.Network, fr2.Network) &&
		fr1.NetworkTier == fr2.NetworkTier, nil
}

// EqualIPv6 compares IPv6 firewall rules and returns true if there are identical from LB point of view.
// Returns an error when `.BackendService` url is invalid.
func EqualIPv6(fr1, fr2 *composite.ForwardingRule) (bool, error) {
	id1, err := cloud.ParseResourceURL(fr1.BackendService)
	if err != nil {
		return false, fmt.Errorf("EqualIPv6(): failed to parse backend resource URL from FR, err - %w", err)
	}
	id2, err := cloud.ParseResourceURL(fr2.BackendService)
	if err != nil {
		return false, fmt.Errorf("EqualIPv6(): failed to parse resource URL from FR, err - %w", err)
	}
	return strings.EqualFold(fr1.IPProtocol, fr2.IPProtocol) &&
		fr1.LoadBalancingScheme == fr2.LoadBalancingScheme &&
		equalPorts(fr1.Ports, fr2.Ports, fr1.PortRange, fr2.PortRange) &&
		utils.EqualCloudResourceIDs(id1, id2) &&
		fr1.AllowGlobalAccess == fr2.AllowGlobalAccess &&
		fr1.AllPorts == fr2.AllPorts &&
		fr1.Subnetwork == fr2.Subnetwork &&
		fr1.NetworkTier == fr2.NetworkTier, nil
}

// equalPorts compares two port ranges or slices of ports. Before comparison,
// slices of ports are converted into a port range from smallest to largest
// port. This is done so we don't unnecessarily recreate forwarding rules
// when upgrading from port ranges to distinct ports, because recreating
// forwarding rules is traffic impacting.
func equalPorts(existingPorts, newPorts []string, existingPortRange, newPortRange string) bool {
	if !flags.F.EnableDiscretePortForwarding || len(existingPorts) != 0 {
		return utils.EqualStringSets(existingPorts, newPorts) && existingPortRange == newPortRange
	}
	// Existing forwarding rule contains a port range. To keep it that way,
	// compare new list of ports as if it was a port range, too.
	if len(newPorts) != 0 {
		newPortRange = utils.MinMaxPortRange(newPorts)
	}
	return existingPortRange == newPortRange
}

func equalResourcePaths(rp1, rp2 string) bool {
	return rp1 == rp2 || utils.EqualResourceIDs(rp1, rp2)
}
