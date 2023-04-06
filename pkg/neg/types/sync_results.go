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

type Result string

const (
	ResultEPCountsDiffer           = Result("EPCountsDiffer")
	ResultEPNodeMissing            = Result("EPNodeMissing")
	ResultEPNodeNotFound           = Result("EPNodeNotFound")
	ResultEPPodMissing             = Result("EPPodMissing")
	ResultEPPodNotFound            = Result("EPPodNotFound")
	ResultEPPodTypeAssertionFailed = Result("EPPodTypeAssertionFailed")
	ResultEPZoneMissing            = Result("EPZoneMissing")
	ResultEPSEndpointCountZero     = Result("EPSEndpointCountZero")
	ResultEPCalculationCountZero   = Result("EPCalculationCountZero")
	ResultInvalidAPIResponse       = Result("InvalidAPIResponse")
	ResultInvalidEPAttach          = Result("InvalidEPAttach")
	ResultInvalidEPDetach          = Result("InvalidEPDetach")

	// these results have their own errors
	ResultNegNotFound          = Result("NegNotFound")
	ResultCurrentNegEPNotFound = Result("CurrentNegEPNotFound")
	ResultEPSNotFound          = Result("EPSNotFound")
	ResultOtherError           = Result("OtherError")
	ResultInProgress           = Result("InProgress")
	ResultSuccess              = Result("Success")
)

var (
	ErrEPCountsDiffer           = errors.New("endpoint counts from endpointData and endpointPodMap differ")
	ErrEPNodeMissing            = errors.New("endpoint has missing nodeName field")
	ErrEPNodeNotFound           = errors.New("endpoint corresponds to an non-existing node")
	ErrEPPodMissing             = errors.New("endpoint has missing pod field")
	ErrEPPodNotFound            = errors.New("endpoint corresponds to an non-existing pod")
	ErrEPPodTypeAssertionFailed = errors.New("endpoint corresponds to an object that fails pod type assertion")
	ErrEPZoneMissing            = errors.New("endpoint has missing zone field")
	ErrEPSEndpointCountZero     = errors.New("endpoint count from endpointData cannot be zero")
	ErrEPCalculationCountZero   = errors.New("endpoint count from endpointPodMap cannot be zero")
	ErrInvalidAPIResponse       = errors.New("received response error doesn't match googleapi.Error type")
	ErrInvalidEPAttach          = errors.New("endpoint information for attach operation is incorrect")
	ErrInvalidEPDetach          = errors.New("endpoint information for detach operation is incorrect")

	// use this map for conversion between errors and sync results
	ErrorStateResult = map[error]Result{
		ErrEPNodeMissing:            ResultEPNodeMissing,
		ErrEPNodeNotFound:           ResultEPNodeNotFound,
		ErrEPPodMissing:             ResultEPPodMissing,
		ErrEPPodNotFound:            ResultEPPodNotFound,
		ErrEPPodTypeAssertionFailed: ResultEPPodTypeAssertionFailed,
		ErrEPZoneMissing:            ResultEPZoneMissing,
		ErrEPCalculationCountZero:   ResultEPCalculationCountZero,
		ErrEPSEndpointCountZero:     ResultEPSEndpointCountZero,
		ErrEPCountsDiffer:           ResultEPCountsDiffer,
		ErrInvalidAPIResponse:       ResultInvalidAPIResponse,
		ErrInvalidEPAttach:          ResultInvalidEPAttach,
		ErrInvalidEPDetach:          ResultInvalidEPDetach,
	}
)

type NegSyncResult struct {
	Error  error
	Result Result
}

func NewNegSyncResult(err error, result Result) *NegSyncResult {
	return &NegSyncResult{
		Error:  err,
		Result: result,
	}
}
