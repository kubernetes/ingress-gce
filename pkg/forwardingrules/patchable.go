package forwardingrules

import "k8s.io/ingress-gce/pkg/composite"

func FilterPatchableFields(existing, new *composite.ForwardingRule) (*composite.ForwardingRule, bool) {
	existingCopy := *existing
	newCopy := *new

	// Set AllowGlobalAccess and NetworkTier fields to the same value in both copies
	existingCopy.AllowGlobalAccess = new.AllowGlobalAccess
	existingCopy.NetworkTier = new.NetworkTier

	equal, err := Equal(&existingCopy, &newCopy)

	// Something is different other than AllowGlobalAccess and NetworkTier
	if err != nil || !equal {
		return nil, false
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
	return filtered, true
}
