package resources

import (
	"errors"

	"k8s.io/ingress-gce/pkg/firewalls"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/utils"
)

// IsUserError checks if given error is caused by User.
func IsUserError(err error) bool {
	var userErr *utils.UserError
	var firewallErr *firewalls.FirewallXPNError

	return utils.IsNetworkTierError(err) ||
		utils.IsIPConfigurationError(err) ||
		utils.IsIPOutOfRangeError(err) ||
		utils.IsConflictingPortsConfigurationError(err) ||
		utils.IsInvalidSubnetConfigurationError(err) ||
		l4utils.IsInvalidLoadBalancerSourceRangesSpecError(err) ||
		l4utils.IsInvalidLoadBalancerSourceRangesAnnotationError(err) ||
		utils.IsUnsupportedNetworkTierError(err) ||
		utils.IsConstraintViolationError(err) ||
		utils.IsFirewallForbiddenError(err) ||
		errors.As(err, &firewallErr) ||
		errors.As(err, &userErr)
}
