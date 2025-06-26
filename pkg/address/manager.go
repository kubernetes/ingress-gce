/*
Copyright 2019 The Kubernetes Authors.

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

package address

import (
	"fmt"
	"net"
	"net/http"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/utils"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/klog/v2"
)

// IPAddressType defines if IP address is Managed by controller
type IPAddressType int

const (
	IPAddrUndefined IPAddressType = iota // IP Address type could not be determine due to error is address provisioning.
	IPAddrManaged
	IPAddrUnmanaged
)

// IPVersion represents compute.Address IpVersion field
type IPVersion = string

const (
	IPv4Version IPVersion = "IPV4"
	IPv6Version IPVersion = "IPV6"
	// IPv6LBAddressPrefixLength used for reserving IPv6 addresses.
	// Google Cloud reserves not a single IPv6 address, but a /96 range.
	// At the moment, no other ranges are supported
	IPv6LBAddressPrefixLength = 96
	// IPv6EndpointTypeNetLB is a value on Address.Ipv6EndpointType used specify
	// that IPv6 address will be used for NetLB. Required for new IPv6 NetLB address creation.
	IPv6EndpointTypeNetLB = "NETLB"
)

// Original file in https://github.com/kubernetes/legacy-cloud-providers/blob/6aa80146c33550e908aed072618bd7f9998837f6/gce/gce_address_manager.go
type Manager struct {
	svc         gce.CloudAddressService
	name        string
	serviceName string
	targetIP    string
	addressType cloud.LbScheme
	region      string
	subnetURL   string
	tryRelease  bool
	networkTier cloud.NetworkTier
	ipVersion   IPVersion

	frLogger klog.Logger
}

func NewManager(svc gce.CloudAddressService, serviceName, region, subnetURL, name, targetIP string, addressType cloud.LbScheme, networkTier cloud.NetworkTier, ipVersion IPVersion, frLogger klog.Logger) *Manager {
	if targetIP != "" {
		// Store address in normalized format.
		// This is required for IPv6 addresses, to be able to filter by exact address,
		// because filtering is happening by regexp and google cloud stores address in the shortest form.
		targetIP = net.ParseIP(targetIP).String()
	}
	// We don't have verification that targetIP matches IPVersion, we will rely on Google cloud API verification
	return &Manager{
		svc:         svc,
		region:      region,
		serviceName: serviceName,
		name:        name,
		targetIP:    targetIP,
		addressType: addressType,
		tryRelease:  true,
		subnetURL:   subnetURL,
		networkTier: networkTier,
		ipVersion:   ipVersion,
		frLogger:    frLogger.WithName("AddressManager"),
	}
}

// HoldAddress will ensure that the IP is reserved with an address - either owned by the controller
// or by a user. If the address is not the addressManager.name, then it's assumed to be a user's address.
// The string returned is the reserved IP address and IPAddressType indicating if IP address is managed by controller.
func (m *Manager) HoldAddress() (string, IPAddressType, error) {
	// HoldAddress starts with retrieving the address that we use for this load balancer (by name).
	// Retrieving an address by IP will indicate if the IP is reserved and if reserved by the user
	// or the controller, but won't tell us the current state of the controller's IP. The address
	// could be reserving another address; therefore, it would need to be deleted. In the normal
	// case of using a controller address, retrieving the address by name results in the fewest API
	// calls since it indicates whether a Delete is necessary before Reserve.
	m.frLogger.V(4).Info("Attempting hold of IP", "ip", m.targetIP, "addressType", m.addressType)
	// Get the address in case it was orphaned earlier
	addr, err := m.svc.GetRegionAddress(m.name, m.region)
	if err != nil && !utils.IsNotFoundError(err) {
		return "", IPAddrUndefined, err
	}

	if addr != nil {
		// If address exists, check if the address had the expected attributes.
		validationError := m.validateAddress(addr)
		if validationError == nil {
			m.frLogger.V(4).Info("Address already reserves IP. No further action required.", "addressName", addr.Name, "ip", addr.Address, "type", addr.AddressType)
			return addr.Address, IPAddrManaged, nil
		}

		m.frLogger.V(2).Info("Deleting existing address", "reason", validationError)
		err := m.svc.DeleteRegionAddress(addr.Name, m.region)
		if err != nil {
			if utils.IsNotFoundError(err) {
				m.frLogger.V(4).Info("Address was not found. Ignoring.", "addressName", addr.Name)
			} else {
				return "", IPAddrUndefined, err
			}
		} else {
			m.frLogger.V(4).Info("Successfully deleted previous address", "addressName", addr.Name)
		}
	}

	return m.ensureAddressReservation()
}

// ReleaseAddress will release the address if it's owned by the controller.
func (m *Manager) ReleaseAddress() error {
	if !m.tryRelease {
		m.frLogger.V(4).Info("Not attempting release of address", "ip", m.targetIP)
		return nil
	}

	m.frLogger.V(4).Info("Releasing address", "ip", m.targetIP, "addressName", m.name)
	// Controller only ever tries to unreserve the address named with the load balancer's name.
	err := m.svc.DeleteRegionAddress(m.name, m.region)
	if err != nil {
		if utils.IsNotFoundError(err) {
			m.frLogger.Info("Address was not found. Ignoring.", "addressName", m.name)
			return nil
		}

		return err
	}

	m.frLogger.V(4).Info("Successfully released IP named", "ip", m.targetIP, "addressName", m.name)
	return nil
}

// ensureAddressReservation reserves ip address and returns address as a string,
// IPAddressType indicating whether ip address is managed by controller and error.
func (m *Manager) ensureAddressReservation() (string, IPAddressType, error) {
	// Try reserving the IP with controller-owned address name
	// If am.targetIP is an empty string, a new IP will be created.
	newAddr := &compute.Address{
		Name:        m.name,
		Description: fmt.Sprintf(`{"kubernetes.io/service-name":"%s"}`, m.serviceName),
		Address:     m.targetIP,
		AddressType: string(m.addressType),
		Subnetwork:  m.subnetURL,
	}
	// NetworkTier is supported only for External IP Address
	if m.addressType == cloud.SchemeExternal {
		newAddr.NetworkTier = m.networkTier.ToGCEValue()
	}

	// This block mitigates inconsistencies in the gcloud API, as revealed through experimentation.
	if m.ipVersion == IPv6Version {
		// Empty targetIP covers reserving new static address.
		if m.targetIP == "" {
			// The IpVersion should be set to IPv6 only when creating a new IPv6 address.
			// Setting it to IPv4 or setting it while promoting an existing load balancer IPv6 address to static can cause errors.
			newAddr.IpVersion = IPv6Version
			if m.addressType == cloud.SchemeExternal {
				// Ipv6EndpointType is required only when creating a new external IPv6 address.
				newAddr.Ipv6EndpointType = IPv6EndpointTypeNetLB
			}
		} else {
			// Non-empty targetIP covers promoting existent load-balancer IP to static one.

			// PrefixLength is only required when converting an existing IPv6 load balancer address to a static one.
			newAddr.PrefixLength = IPv6LBAddressPrefixLength
			if m.addressType == cloud.SchemeExternal {
				// Specifying a Subnetwork will return error when promoting an existing external IPv6 forwarding rule IP to static.
				newAddr.Subnetwork = ""
			}
		}
	}

	reserveErr := m.svc.ReserveRegionAddress(newAddr, m.region)
	if reserveErr == nil {
		if newAddr.Address != "" {
			m.frLogger.V(4).Info("Successfully reserved IP", "ip", newAddr.Address, "addressName", newAddr.Name)
			return newAddr.Address, IPAddrManaged, nil
		}

		// If an ip address was not specified, get the newly created address resource to determine the assigned address.
		addr, err := m.svc.GetRegionAddress(newAddr.Name, m.region)
		if err != nil {
			return "", IPAddrUndefined, err
		}

		m.frLogger.V(4).Info("Successfully created address which reserved IP", "addressName", addr.Name, "ip", addr.Address)
		return addr.Address, IPAddrManaged, nil
	}

	m.frLogger.V(2).Info("Unable to reserve address, checking for conflict", "err", reserveErr)

	if utils.IsNetworkTierMismatchGCEError(reserveErr) {
		receivedNetworkTier := cloud.NetworkTierPremium
		if receivedNetworkTier == m.networkTier {
			// We don't have information of current ephemeral IP address Network Tier since
			// we try to reserve the address so we need to check against desired Network Tier and set the opposite one.
			// This is just for error message.
			receivedNetworkTier = cloud.NetworkTierStandard
		}
		resource := fmt.Sprintf("Reserved static IP (%v)", m.name)
		networkTierError := utils.NewNetworkTierErr(resource, string(m.networkTier), string(receivedNetworkTier))
		return "", IPAddrUndefined, networkTierError
	}

	if !utils.IsHTTPErrorCode(reserveErr, http.StatusConflict) &&
		!utils.IsHTTPErrorCode(reserveErr, http.StatusBadRequest) &&
		!utils.IsHTTPErrorCode(reserveErr, http.StatusPreconditionFailed) {
		// If the IP is already reserved:
		//    by an internal address: a StatusConflict is returned
		//    by an external address: a BadRequest is returned
		//    by an external IPv6 address: a 412 Precondition not met error is returned
		return "", IPAddrUndefined, reserveErr
	}

	// If the target IP was empty, we cannot try to find which IP caused a conflict.
	// If the name was already used, then the next sync will attempt deletion of that address.
	if m.targetIP == "" {
		return "", IPAddrUndefined, fmt.Errorf("failed to reserve address %q with no specific IP, err: %v", m.name, reserveErr)
	}

	// Reserving the address failed due to a conflict or bad request. The address manager just checked that no address
	// exists with the name, so it may belong to the user.
	addr, err := m.svc.GetRegionAddressByIP(m.region, m.targetIP)
	if err != nil {
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			return "", IPAddrUndefined, utils.NewIPConfigurationError(m.targetIP, "address not found. Check if the IP is valid and is reserved in your VPC.")
		}
		return "", IPAddrUndefined, fmt.Errorf("failed to get address by IP %q after reservation attempt, err: %q, reservation err: %q", m.targetIP, err, reserveErr)
	}

	// Check that the address attributes are as required.
	if err := m.validateAddress(addr); err != nil {
		return "", IPAddrUndefined, fmt.Errorf("address (%q) validation failed, err: %w", addr.Name, err)
	}

	if m.isManagedAddress(addr) {
		// The address with this name is checked at the beginning of 'HoldAddress()', but for some reason
		// it was re-created by this point. May be possible that two controllers are running.
		m.frLogger.Info("Address %q unexpectedly existed with IP %q.", "addressName", addr.Name, "ip", m.targetIP)
		return addr.Address, IPAddrManaged, nil
	}
	// If the retrieved address is not named with the loadbalancer name, then the controller does not own it, but will allow use of it.
	m.frLogger.V(4).Info("Address was already reserved with name: %q, description: %q", "ip", m.targetIP, "addressName", addr.Name, "addressDescription", addr.Description)
	m.tryRelease = false
	return addr.Address, IPAddrUnmanaged, nil

}

func (m *Manager) validateAddress(addr *compute.Address) error {
	if m.targetIP != "" && !IsSameIP(m.targetIP, addr.Address) {
		return fmt.Errorf("IP mismatch, expected %q, actual: %q", m.targetIP, addr.Address)
	}
	if addr.AddressType != string(m.addressType) {
		return utils.NewIPConfigurationError(m.targetIP, fmt.Sprintf("address type mismatch, expected %q, actual: %q", m.addressType, addr.AddressType))
	}
	if addr.NetworkTier != m.networkTier.ToGCEValue() {
		return utils.NewNetworkTierErr(fmt.Sprintf("Static IP (%v)", m.name), m.networkTier.ToGCEValue(), addr.NetworkTier)
	}
	return nil
}

func IsSameIP(ip1 string, ip2 string) bool {
	parsedIP1 := net.ParseIP(ip1)
	parsedIP2 := net.ParseIP(ip2)
	if parsedIP1 == nil || parsedIP2 == nil {
		return false
	}

	return parsedIP1.Equal(parsedIP2)
}

func (m *Manager) isManagedAddress(addr *compute.Address) bool {
	return addr.Name == m.name
}

func EnsureDeleted(svc gce.CloudAddressService, name, region string) error {
	return utils.IgnoreHTTPNotFound(svc.DeleteRegionAddress(name, region))
}

// TearDownAddressIPIfNetworkTierMismatch this function tear down controller managed address IP if it has a wrong Network Tier
func (m *Manager) TearDownAddressIPIfNetworkTierMismatch() error {
	if m.targetIP == "" {
		return nil
	}
	addr, err := m.svc.GetRegionAddressByIP(m.region, m.targetIP)
	if utils.IsNotFoundError(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if addr != nil && addr.NetworkTier != m.networkTier.ToGCEValue() {
		if !m.isManagedAddress(addr) {
			return utils.NewNetworkTierErr(fmt.Sprintf("User specific address IP (%v)", m.name), string(m.networkTier), addr.NetworkTier)
		}
		m.frLogger.V(3).Info("Deleting IP address because it has a wrong network tier", "ip", m.targetIP)
		if err := m.svc.DeleteRegionAddress(addr.Name, m.targetIP); err != nil {
			m.frLogger.Error(err, "Unable to delete region address on target ip", "addressName", addr.Name, "ip", m.targetIP)
		}
	}
	return nil
}
