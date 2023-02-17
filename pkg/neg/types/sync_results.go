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

var (
	ResultEPCountsDiffer = "EPCountsDiffer"
	ErrEPCountsDiffer    = errors.New("endpoint counts from endpointData and endpointPodMap differ")

	ResultEPMissingNodeName = "EPMissingNodeName"
	ErrEPMissingNodeName    = errors.New("endpoint has empty nodeName field")

	ResultNodeNotFound = "NodeNotFound"
	ErrNodeNotFound    = errors.New("failed to retrieve associated zone of node")

	ResultEPMissingZone = "EPMissingZone"
	ErrEPMissingZone    = errors.New("endpoint has empty zone field")

	ResultEPSEndpointCountZero = "EPSEndpointCountZero"
	ErrEPSEndpointCountZero    = errors.New("endpoint count from endpointData cannot be zero")

	ResultEPCalculationCountZero = "EPCalculationCountZero"
	ErrEPCalculationCountZero    = errors.New("endpoint count from endpointPodMap cannot be zero")

	// these results have their own errors
	ResultInvalidAPIResponse = "InvalidAPIResponse"
	ResultInvalidEPAttach    = "InvalidEPAttach"
	ResultInvalidEPDetach    = "InvalidEPDetach"
	ResultNegNotFound        = "NegNotFound"
	ResultCurrentEPNotFound  = "CurrentEPNotFound"
	ResultEPSNotFound        = "EPSNotFound"
	ResultOtherError         = "OtherError"
	ResultInProgress         = "InProgress"
	ResultSuccess            = "Success"
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
