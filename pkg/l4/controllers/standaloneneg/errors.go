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

package standaloneneg

import (
	"errors"

	"k8s.io/ingress-gce/pkg/l4/resources"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
)

// join errors but if all are UserErrors make the wrapping error a UserError as
// well. This is required to properly attribute sync status in metrics.
func joinMaybeUserErrors(errs ...error) error {
	var nonNilErrs []error
	for _, err := range errs {
		if err != nil {
			nonNilErrs = append(nonNilErrs, err)
		}
	}
	if len(nonNilErrs) == 0 {
		return nil
	}

	allUserErrors := true
	for _, err := range nonNilErrs {
		if !resources.IsUserError(err) {
			allUserErrors = false
			break
		}
	}
	joinedErr := errors.Join(nonNilErrs...)
	if allUserErrors {
		return l4utils.NewUserError(joinedErr)
	}
	return joinedErr
}

// hasNonUserError checks if any of the errors is not a user error.
func hasNonUserError(errs []error) bool {
	for _, err := range errs {
		if err != nil && !resources.IsUserError(err) {
			return true
		}
	}
	return false
}
