package utils

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"google.golang.org/api/googleapi"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	gceutils "k8s.io/ingress-gce/pkg/utils"
)

var networkTierErrorRegexp = regexp.MustCompile(`The network tier of external IP is STANDARD|PREMIUM, that of Address must be the same.`)

// NetworkTierError is a struct to define error caused by User misconfiguration of Network Tier.
type NetworkTierError struct {
	resource   string
	desiredNT  string
	receivedNT string
}

// Error function prints out Network Tier error.
func (e *NetworkTierError) Error() string {
	return fmt.Sprintf("Network tier mismatch for resource %s, desired: %s, received: %s", e.resource, e.desiredNT, e.receivedNT)
}

func NewNetworkTierErr(resourceInErr, desired, received string) *NetworkTierError {
	return &NetworkTierError{resource: resourceInErr, desiredNT: desired, receivedNT: received}
}

type UnsupportedNetworkTierError struct {
	resource      string
	unsupportedNT string
}

// Error function prints out Network Tier error.
func (e *UnsupportedNetworkTierError) Error() string {
	return fmt.Sprintf("Network tier %s is not supported for %s", e.unsupportedNT, e.resource)
}

func NewUnsupportedNetworkTierErr(resourceInErr, networkTier string) *UnsupportedNetworkTierError {
	return &UnsupportedNetworkTierError{resource: resourceInErr, unsupportedNT: networkTier}
}

// IPConfigurationError is a struct to define error caused by User misconfiguration the Load Balancer IP.
type IPConfigurationError struct {
	ip     string
	reason string
}

func (e *IPConfigurationError) Error() string {
	return fmt.Sprintf("IP configuration error: \"%s\" %s", e.ip, e.reason)
}

func NewIPConfigurationError(ip, reason string) *IPConfigurationError {
	return &IPConfigurationError{ip: ip, reason: reason}
}

// ConflictingPortsConfigurationError is a struct to define error caused by User misconfiguration of the LB ports.
type ConflictingPortsConfigurationError struct {
	ports  string
	reason string
}

func (e *ConflictingPortsConfigurationError) Error() string {
	return fmt.Sprintf("Conflicting ports configuration error: \"%s\" %s", e.ports, e.reason)
}

func NewConflictingPortsConfigurationError(ports, reason string) *ConflictingPortsConfigurationError {
	return &ConflictingPortsConfigurationError{ports: ports, reason: reason}
}

// InvalidSubnetConfigurationError is a struct to define error caused by User misconfiguration of Load Balancer's subnet.
type InvalidSubnetConfigurationError struct {
	projectName string
	subnetName  string
}

func (e *InvalidSubnetConfigurationError) Error() string {
	return fmt.Sprintf("Subnetwork \"%s\" can't be found for project %s", e.subnetName, e.projectName)
}

func NewInvalidSubnetConfigurationError(projectName, subnetName string) *InvalidSubnetConfigurationError {
	return &InvalidSubnetConfigurationError{projectName: projectName, subnetName: subnetName}
}

// IsNetworkTierMismatchGCEError checks if error is a GCE network tier mismatch for external IP
func IsNetworkTierMismatchGCEError(err error) bool {
	return networkTierErrorRegexp.MatchString(err.Error())
}

// IsNetworkTierError checks if wrapped error is a Network Tier Mismatch error
func IsNetworkTierError(err error) bool {
	var netTierError *NetworkTierError
	return errors.As(err, &netTierError)
}

// IsUnsupportedNetworkTierError checks if wrapped error is an Unsupported Network Tier error
func IsUnsupportedNetworkTierError(err error) bool {
	var netTierError *UnsupportedNetworkTierError
	return errors.As(err, &netTierError)
}

// IsConstraintViolationError checks if the error is a constraint violation error returned by GCP.
func IsConstraintViolationError(err error) bool {
	return gceutils.IsHTTPErrorCode(err, http.StatusPreconditionFailed) && strings.Contains(err.Error(), "Constraint") && strings.Contains(err.Error(), "violated")
}

// IsIPConfigurationError checks if wrapped error is an IP configuration error.
func IsIPConfigurationError(err error) bool {
	var ipConfigError *IPConfigurationError
	return errors.As(err, &ipConfigError)
}

// IsIPOutOfRangeError checks if error is a GCE IP out of range error.
func IsIPOutOfRangeError(err error) bool {
	return gceutils.IsHTTPErrorCode(err, http.StatusBadRequest) && strings.Contains(err.Error(), "Requested internal IP address is outside the network/subnetwork range")
}

// IsConflictingPortsConfigurationError checks if wrapped error is an conflicting ports configuration error.
func IsConflictingPortsConfigurationError(err error) bool {
	var portsConflictConfigError *ConflictingPortsConfigurationError
	return errors.As(err, &portsConflictConfigError)
}

// IsInvalidSubnetConfigurationError checks if wrapped error is an Invalid Subnet Configuration error.
func IsInvalidSubnetConfigurationError(err error) bool {
	var invalidSubnetConfigError *InvalidSubnetConfigurationError
	return errors.As(err, &invalidSubnetConfigError)
}

// UserError is a struct to define error caused by User misconfiguration.
type UserError struct {
	err error
}

func (e *UserError) Error() string {
	return e.err.Error()
}

func (e *UserError) Unwrap() error {
	return e.err
}

func NewUserError(err error) *UserError {
	return &UserError{err}
}

func GetErrorType(err error) string {
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		return http.StatusText(gerr.Code)
	}
	var k8serr *k8serrors.StatusError
	if errors.As(err, &k8serr) {
		return "k8s " + string(k8serrors.ReasonForError(k8serr))
	}
	return ""
}

// IsUnsupportedFeatureError returns true if the error has 400 number,
// and has information that `featureName` isn't supported
//
//	ex: ```Error 400: Invalid value for field
//	resource.connectionTrackingPolicy.enableStrongAffinity': 'true'.
//	EnableStrongAffinity is not supported., invalid```
func IsUnsupportedFeatureError(err error, featureName string) bool {
	if err == nil {
		return false
	}
	isUnsupported := strings.Contains(err.Error(), "is not supported") && gceutils.IsHTTPErrorCode(err, http.StatusBadRequest)
	if strings.Contains(err.Error(), featureName) && isUnsupported {
		return true
	}
	return false
}

// UnsupportedLoadBalancingSchemeError is an error for unsupported load balancing scheme.
type UnsupportedLoadBalancingSchemeError struct {
	frName           string
	scheme           string
	supportedSchemes []string
}

func (e *UnsupportedLoadBalancingSchemeError) Error() string {
	return fmt.Sprintf("forwarding rule %s has unsupported load balancing scheme: %s, supported schemes are: %s",
		e.frName, e.scheme, strings.Join(e.supportedSchemes, ", "))
}

// NewUnsupportedLoadBalancingSchemeError creates a new UnsupportedLoadBalancingSchemeError.
func NewUnsupportedLoadBalancingSchemeError(frName, scheme string, supportedSchemes []string) *UnsupportedLoadBalancingSchemeError {
	return &UnsupportedLoadBalancingSchemeError{
		frName:           frName,
		scheme:           scheme,
		supportedSchemes: supportedSchemes,
	}
}

// UnsupportedProtocolError is an error for unsupported protocol.
type UnsupportedProtocolError struct {
	frName             string
	protocol           string
	supportedProtocols []string
}

func (e *UnsupportedProtocolError) Error() string {
	return fmt.Sprintf("forwarding rule %s has unsupported protocol: %s, supported protocols are: %s",
		e.frName, e.protocol, strings.Join(e.supportedProtocols, ", "))
}

// NewUnsupportedProtocolError creates a new UnsupportedProtocolError.
func NewUnsupportedProtocolError(frName, protocol string, supportedProtocols []string) *UnsupportedProtocolError {
	return &UnsupportedProtocolError{
		frName:             frName,
		protocol:           protocol,
		supportedProtocols: supportedProtocols,
	}
}

// IsUnsupportedLoadBalancingSchemeError checks if wrapped error is an UnsupportedLoadBalancingSchemeError.
func IsUnsupportedLoadBalancingSchemeError(err error) bool {
	var unsupportedLBSchemeErr *UnsupportedLoadBalancingSchemeError
	return errors.As(err, &unsupportedLBSchemeErr)
}

// IsUnsupportedProtocolError checks if wrapped error is an UnsupportedProtocolError.
func IsUnsupportedProtocolError(err error) bool {
	var unsupportedProtocolErr *UnsupportedProtocolError
	return errors.As(err, &unsupportedProtocolErr)
}
