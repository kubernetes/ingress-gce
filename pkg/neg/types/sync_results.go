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

const (
	ResultEPCountsDiffer         = "EPCountsDiffer"
	ResultEPMissingNodeName      = "EPMissingNodeName"
	ResultNodeNotFound           = "NodeNotFound"
	ResultEPMissingZone          = "EPMissingZone"
	ResultEPSEndpointCountZero   = "EPSEndpointCountZero"
	ResultEPCalculationCountZero = "EPCalculationCountZero"
	ResultInvalidAPIResponse     = "InvalidAPIResponse"
	ResultInvalidEPAttach        = "InvalidEPAttach"
	ResultInvalidEPDetach        = "InvalidEPDetach"

	// these results have their own errors
	ResultNegNotFound       = "NegNotFound"
	ResultCurrentEPNotFound = "CurrentEPNotFound"
	ResultEPSNotFound       = "EPSNotFound"
	ResultOtherError        = "OtherError"
	ResultInProgress        = "InProgress"
	ResultSuccess           = "Success"
)

var (
	ErrEPCountsDiffer         = errors.New("endpoint counts from endpointData and endpointPodMap differ")
	ErrEPMissingNodeName      = errors.New("endpoint has empty nodeName field")
	ErrNodeNotFound           = errors.New("failed to retrieve associated zone of node")
	ErrEPMissingZone          = errors.New("endpoint has empty zone field")
	ErrEPSEndpointCountZero   = errors.New("endpoint count from endpointData cannot be zero")
	ErrEPCalculationCountZero = errors.New("endpoint count from endpointPodMap cannot be zero")
	ErrInvalidAPIResponse     = errors.New("received response error doesn't match googleapi.Error type")
	ErrInvalidEPAttach        = errors.New("endpoint information for attach operation is incorrect")
	ErrInvalidEPDetach        = errors.New("endpoint information for detach operation is incorrect")

	// use this map for conversion between errors and sync results
	ErrorStateResult = map[error]string{
		ErrEPMissingNodeName:      ResultEPMissingNodeName,
		ErrEPMissingZone:          ResultEPMissingZone,
		ErrEPCalculationCountZero: ResultEPCalculationCountZero,
		ErrEPSEndpointCountZero:   ResultEPSEndpointCountZero,
		ErrEPCountsDiffer:         ResultEPCountsDiffer,
		ErrInvalidAPIResponse:     ResultInvalidAPIResponse,
		ErrInvalidEPAttach:        ResultInvalidEPAttach,
		ErrInvalidEPDetach:        ResultInvalidEPDetach,
	}
)

type NegSyncResult struct {
	Error  error
	Result string
}

func NewNegSyncResult(err error, result string) *NegSyncResult {
	return &NegSyncResult{
		Error:  err,
		Result: result,
	}
}
