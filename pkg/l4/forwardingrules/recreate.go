/*
Copyright 2026 The Kubernetes Authors.

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

package forwardingrules

import (
	"errors"
	"fmt"

	"k8s.io/ingress-gce/pkg/composite"
)

// ErrIPNotReserved is returned when a forwarding rule would have to be deleted
// while its IP address is not held by an address reservation. Between the
// deletion and the creation of the rule that replaces it, an unreserved IP can
// be taken by another resource, so the rule is left in place and the sync is
// retried instead.
var ErrIPNotReserved = errors.New("forwarding rule IP address is not reserved")

// legacyNetworkChecker is the part of *gce.Cloud needed by CheckRecreateAllowed.
type legacyNetworkChecker interface {
	IsLegacyNetwork() bool
}

// CheckRecreateAllowed must be called before deleting a forwarding rule whose IP
// is going to be used again by a rule created later in the same sync (the same
// rule re-created with new attributes, or a rule with the single/mixed protocol
// name replacing one with the other name). It returns an error wrapping
// ErrIPNotReserved unless the IP is protected by an address reservation
// (ipReserved), so that a sync that skipped the reservation can never delete
// the rule.
//
// Addresses can't be reserved in legacy networks; rules there are re-created
// without protection, as they always were.
func CheckRecreateAllowed(cloud legacyNetworkChecker, existing *composite.ForwardingRule, ipReserved bool) error {
	if ipReserved || cloud.IsLegacyNetwork() {
		return nil
	}
	return fmt.Errorf("refusing to delete forwarding rule %s to re-create it, its IP %s could be lost: %w", existing.Name, existing.IPAddress, ErrIPNotReserved)
}
