package forwardingrules

import "k8s.io/ingress-gce/pkg/composite"

// PatchableIPv4 returns true and patched fields if existing rule can be patched to new.
// Patched fields can be used to call Patch method on ForwardingRules struct.
// If the rule can't be patched returns false and nil.
func PatchableIPv4(existing, new *composite.ForwardingRule) (bool, *composite.ForwardingRule) {
	existingCopy := *existing
	newCopy := *new

	// Set AllowGlobalAccess and NetworkTier fields to the same value in both copies
	existingCopy.AllowGlobalAccess = new.AllowGlobalAccess
	existingCopy.NetworkTier = new.NetworkTier

	equal, err := EqualIPv4(&existingCopy, &newCopy)

	// Something is different other than AllowGlobalAccess and NetworkTier
	if err != nil || !equal {
		return false, nil
	}

	filtered := &composite.ForwardingRule{}
	filtered.Id = existing.Id
	filtered.Name = existing.Name
	// AllowGlobalAccess is in the ForceSendFields list, it always need to have the right value
	filtered.AllowGlobalAccess = new.AllowGlobalAccess
	// Send NetworkTier in the patch request only if it has been updated
	if existing.NetworkTier != new.NetworkTier {
		filtered.NetworkTier = new.NetworkTier
	}
	return true, filtered
}
