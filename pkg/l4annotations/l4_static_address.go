package l4annotations

import (
	"errors"
	"strings"

	"google.golang.org/api/googleapi"
	"k8s.io/cloud-provider-gcp/providers/gce"
)

// IPVersion represents compute.Address IpVersion field
type IPVersion = string

const (
	// StaticL4AddressesAnnotationKey is new annotation key to specify static IPs (by name) for the services.
	// Supports both IPv4 and IPv6
	StaticL4AddressesAnnotationKey           = "networking.gke.io/load-balancer-ip-addresses"
	maxNumberOfAddresses                     = 2
	IPv4Version                    IPVersion = "IPV4"
	IPv6Version                    IPVersion = "IPV6"
)

// IPv4AddressAnnotation return IPv4 address from networking.gke.io/load-balancer-ip-addresses annotation.
// If no IPv4 address found, returns empty string.
func (svc *Service) IPv4AddressAnnotation(cloud *gce.Cloud) (ipAddress, ipName string, err error) {
	return ipAddressFromAnnotation(svc, cloud, IPv4Version)
}

// IPv6AddressAnnotation return IPv6 address from networking.gke.io/load-balancer-ip-addresses annotation.
// If no IPv6 address found, returns empty string.
func (svc *Service) IPv6AddressAnnotation(cloud *gce.Cloud) (ipAddress, ipName string, err error) {
	return ipAddressFromAnnotation(svc, cloud, IPv6Version)
}

// ipAddressFromAnnotation checks annotation networking.gke.io/load-balancer-ip-addresses,
// which should store comma separate names of IP Addresses reserved in google cloud,
// and returns first address and its name that matches required IpVersion (IPV4 or IPV6).
func ipAddressFromAnnotation(svc *Service, cloud *gce.Cloud, ipVersion string) (ipAddress, ipName string, err error) {
	annotationVal, ok := svc.v[StaticL4AddressesAnnotationKey]
	if !ok {
		return "", "", nil
	}

	addressNames := strings.Split(annotationVal, ",")

	// Truncated to 2 values (this is technically maximum, 1 IPv4 and 1 IPv6 address)
	// to not make too many API calls.
	if len(addressNames) > maxNumberOfAddresses {
		addressNames = addressNames[:maxNumberOfAddresses]
	}

	for _, addressName := range addressNames {
		trimmedAddressName := strings.TrimSpace(addressName)
		cloudAddress, err := cloud.GetRegionAddress(trimmedAddressName, cloud.Region())
		if err != nil {
			if isNotFoundError(err) {
				continue
			}
			return "", "", err
		}
		if cloudAddress.IpVersion == "" {
			cloudAddress.IpVersion = IPv4Version
		}

		if cloudAddress.IpVersion == ipVersion {
			return cloudAddress.Address, trimmedAddressName, nil
		}
	}
	return "", "", nil
}

func isNotFoundError(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 404
}
