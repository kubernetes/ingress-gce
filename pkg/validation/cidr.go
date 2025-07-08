/*
Copyright 2025 The Kubernetes Authors.

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

package validation

import (
	"fmt"
	"net"
	"strings"
)

// ValidateHealthCheckSourceCIDRs validates a comma-separated string of CIDR ranges.
// This is a convenience function that calls ParseHealthCheckSourceCIDRs and discards
// the result, only returning validation errors.
func ValidateHealthCheckSourceCIDRs(cidrsInput string) error {
	_, err := ParseHealthCheckSourceCIDRs(cidrsInput)
	return err
}

// ParseHealthCheckSourceCIDRs parses a comma-separated string of CIDR ranges
// and returns a slice of valid CIDR strings. It trims whitespace and validates
// each CIDR using net.ParseCIDR.
func ParseHealthCheckSourceCIDRs(cidrsInput string) ([]string, error) {
	if cidrsInput == "" {
		return nil, nil
	}

	cidrs := strings.Split(cidrsInput, ",")
	var result []string

	for _, cidr := range cidrs {
		trimmed := strings.TrimSpace(cidr)

		if err := ValidateCIDR(trimmed); err != nil {
			return nil, fmt.Errorf("invalid CIDR format %q: %v", trimmed, err)
		}

		result = append(result, trimmed)
	}

	return result, nil
}

// ValidateCIDR validates that a string is a valid CIDR range using net.ParseCIDR.
func ValidateCIDR(cidr string) error {
	_, _, err := net.ParseCIDR(cidr)
	return err
}
