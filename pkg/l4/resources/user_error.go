package resources

import (
	"errors"

	"k8s.io/ingress-gce/pkg/firewalls"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

// IsUserError checks if given error is caused by User.
func IsUserError(err error) bool {
	var userErr *l4utils.UserError
	var firewallErr *firewalls.FirewallXPNError

	return l4utils.IsNetworkTierError(err) ||
		l4utils.IsIPConfigurationError(err) ||
		l4utils.IsIPOutOfRangeError(err) ||
		l4utils.IsConflictingPortsConfigurationError(err) ||
		l4utils.IsInvalidSubnetConfigurationError(err) ||
		l4utils.IsInvalidLoadBalancerSourceRangesSpecError(err) ||
		l4utils.IsInvalidLoadBalancerSourceRangesAnnotationError(err) ||
		l4utils.IsUnsupportedNetworkTierError(err) ||
		l4utils.IsConstraintViolationError(err) ||
		errors.As(err, &firewallErr) ||
		errors.As(err, &userErr)
}
