/*
Copyright 2023 The Kubernetes Authors.
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

package types

import "errors"

type Reason string

const (
	ReasonEPCountsDiffer            = Reason("EPCountsDiffer")
	ReasonEPNodeMissing             = Reason("EPNodeMissing")
	ReasonEPNodeNotFound            = Reason("EPNodeNotFound")
	ReasonEPNodeTypeAssertionFailed = Reason("EPNodeTypeAssertionFailed")
	ReasonEPPodMissing              = Reason("EPPodMissing")
	ReasonEPPodNotFound             = Reason("EPPodNotFound")
	ReasonEPPodTypeAssertionFailed  = Reason("EPPodTypeAssertionFailed")
	ReasonEPPodTerminal             = Reason("EPPodTerminal")
	ReasonEPZoneMissing             = Reason("EPZoneMissing")
	ReasonEPSEndpointCountZero      = Reason("EPSEndpointCountZero")
	ReasonEPCalculationCountZero    = Reason("EPCalculationCountZero")
	ReasonInvalidAPIResponse        = Reason("InvalidAPIResponse")
	ReasonInvalidEPAttach           = Reason("InvalidEPAttach")
	ReasonInvalidEPDetach           = Reason("InvalidEPDetach")
	ReasonEPIPInvalid               = Reason("EPIPInvalid")
	ReasonEPIPNotFromPod            = Reason("EPIPNotFromPod")
	ReasonEPIPOutOfPodCIDR          = Reason("EPIPOutOfPodCIDR")
	ReasonEPServiceNotFound         = Reason("EPServiceNotFound")
	ReasonEPPodLabelMismatch        = Reason("EPPodLabelMismatch")

	// these are for non error-state error
	ReasonNegNotFound          = Reason("NegNotFound")
	ReasonCurrentNegEPNotFound = Reason("CurrentNegEPNotFound")
	ReasonEPSNotFound          = Reason("EPSNotFound")
	ReasonOtherError           = Reason("OtherError")
	ReasonSuccess              = Reason("Success")
)

var (
	ErrEPCountsDiffer = NegSyncError{
		Err:          errors.New("endpoint counts from endpointData and endpointPodMap should be equal"),
		Reason:       ReasonEPCountsDiffer,
		IsErrorState: true,
	}
	ErrEPNodeMissing = NegSyncError{
		Err:          errors.New("endpoint has missing nodeName field"),
		Reason:       ReasonEPNodeMissing,
		IsErrorState: true,
	}
	ErrEPNodeNotFound = NegSyncError{
		Err:          errors.New("endpoint corresponds to an non-existing node"),
		Reason:       ReasonEPNodeNotFound,
		IsErrorState: true,
	}
	ErrEPNodeTypeAssertionFailed = NegSyncError{
		Err:          errors.New("endpoint corresponds to an object that fails node type assertion"),
		Reason:       ReasonEPNodeTypeAssertionFailed,
		IsErrorState: true,
	}
	ErrEPPodMissing = NegSyncError{
		Err:          errors.New("endpoint has missing pod field"),
		Reason:       ReasonEPPodMissing,
		IsErrorState: true,
	}
	ErrEPPodNotFound = NegSyncError{
		Err:          errors.New("endpoint corresponds to an non-existing pod"),
		Reason:       ReasonEPPodNotFound,
		IsErrorState: true,
	}
	ErrEPPodTypeAssertionFailed = NegSyncError{
		Err:          errors.New("endpoint corresponds to an object that fails pod type assertion"),
		Reason:       ReasonEPPodTypeAssertionFailed,
		IsErrorState: true,
	}
	ErrEPPodTerminal = NegSyncError{
		Err:          errors.New("endpoint corresponds to a terminal pod"),
		Reason:       ReasonEPPodTerminal,
		IsErrorState: true,
	}
	ErrEPZoneMissing = NegSyncError{
		Err:          errors.New("endpoint has missing zone field"),
		Reason:       ReasonEPZoneMissing,
		IsErrorState: true,
	}
	ErrEPSEndpointCountZero = NegSyncError{
		Err:          errors.New("endpoint count from endpointData cannot be zero"),
		Reason:       ReasonEPSEndpointCountZero,
		IsErrorState: true,
	}
	ErrEPCalculationCountZero = NegSyncError{
		Err:          errors.New("endpoint count from endpointPodMap cannot be zero"),
		Reason:       ReasonEPCalculationCountZero,
		IsErrorState: true,
	}
	ErrInvalidAPIResponse = NegSyncError{
		Err:          errors.New("received response error doesn't match googleapi.Error type"),
		Reason:       ReasonInvalidAPIResponse,
		IsErrorState: true,
	}
	ErrInvalidEPAttach = NegSyncError{
		Err:          errors.New("endpoint information for attach operation is incorrect"),
		Reason:       ReasonInvalidEPAttach,
		IsErrorState: true,
	}
	ErrInvalidEPDetach = NegSyncError{
		Err:          errors.New("endpoint information for detach operation is incorrect"),
		Reason:       ReasonInvalidEPDetach,
		IsErrorState: true,
	}
	ErrEPIPInvalid = NegSyncError{
		Err:          errors.New("endpoint has an invalid IP address"),
		Reason:       ReasonEPIPInvalid,
		IsErrorState: true,
	}
	ErrEPIPNotFromPod = NegSyncError{
		Err:          errors.New("endpoint has an IP that does not correspond to its pod"),
		Reason:       ReasonEPIPNotFromPod,
		IsErrorState: true,
	}
	ErrEPIPOutOfPodCIDR = NegSyncError{
		Err:          errors.New("endpoint corresponds to a pod with IP out of PodCIDR range"),
		Reason:       ReasonEPIPOutOfPodCIDR,
		IsErrorState: true,
	}
	ErrEPServiceNotFound = NegSyncError{
		Err:          errors.New("endpoint corresponds to a non-existent service"),
		Reason:       ReasonEPServiceNotFound,
		IsErrorState: true,
	}
	ErrEPPodLabelMismatch = NegSyncError{
		Err:          errors.New("endpoint corresponds to a pod with labels not matching to its service"),
		Reason:       ReasonEPPodLabelMismatch,
		IsErrorState: true,
	}

	ErrNegNotFound = NegSyncError{
		Err:          errors.New("failed to get NEG for service"),
		Reason:       ReasonNegNotFound,
		IsErrorState: false,
	}
	ErrCurrentNegEPNotFound = NegSyncError{
		Err:          errors.New("failed to get current NEG endpoints"),
		Reason:       ReasonCurrentNegEPNotFound,
		IsErrorState: false,
	}
	ErrEPSNotFound = NegSyncError{
		Err:          errors.New("failed to get EPS for service"),
		Reason:       ReasonEPSNotFound,
		IsErrorState: false,
	}
)

// other errors encountered during sync are considered as OtherError in metrics
// and they will not trigger error state
// if any errors need to be collected in sync metrics or used to set error state,
// please define them above
type NegSyncError struct {
	Err          error
	Reason       Reason
	IsErrorState bool
}

func (se NegSyncError) Error() string {
	return se.Err.Error()
}

// ClassifyError takes a non-nil error and checks if this error is an NegSyncError
// if so, it unwraps and returns the NegSyncError,
// else it would wrap it as a NegSyncError with ReasonOtherError and IsErrorState set to false
func ClassifyError(err error) NegSyncError {
	var syncErrType NegSyncError
	if !errors.As(err, &syncErrType) {
		return NegSyncError{
			Err:          err,
			Reason:       ReasonOtherError,
			IsErrorState: false,
		}
	}
	// unwrap error till we get NegSyncError, so we can check error state
	for {
		if syncErr, ok := err.(NegSyncError); ok {
			return syncErr
		}
		err = errors.Unwrap(err)
	}
}
