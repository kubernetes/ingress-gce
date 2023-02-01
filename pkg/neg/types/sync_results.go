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
	SyncerEPCountsDiffer = "SyncerEPCountsDiffer"
	ErrEPCountsDiffer    = errors.New("endpoint counts from endpointData and endpointPodMap differ")

	ResultEPMissingNodeName = "EPMissingNodeName"
	SyncerEPMissingNodeName = "SyncerEPMissingNodeName"
	ErrEPMissingNodeName    = errors.New("endpoint has empty nodeName field")

	ResultNodeNotFound = "NodeNotFound"
	SyncerNodeNotFound = "SyncerNodeNotFound"
	ErrNodeNotFound    = errors.New("failed to retrieve associated zone of node")

	ResultEPMissingZone = "EPMissingZone"
	SyncerEPMissingZone = "SyncerEPMissingZone"
	ErrEPMissingZone    = errors.New("endpoint has empty zone field")

	ResultEPSEndpointCountZero = "EPSEndpointCountZero"
	SyncerEPSEndpointCountZero = "SyncerEPSEndpointCountZero"
	ErrEPSEndpointCountZero    = errors.New("endpoint count from endpointData cannot be zero")

	ResultEPCalculationCountZero = "EPCalculationCountZero"
	SyncerEPCalculationCountZero = "SyncerEPCalculationCountZero"
	ErrEPCalculationCountZero    = errors.New("endpoint count from endpointPodMap cannot be zero")

	// these results have their own errors
	ResultInvalidEPAttach = "InvalidEPAttach"
	SyncerInvalidEPAttach = "SyncerInvalidEPAttach"

	ResultInvalidEPDetach = "InvalidEPDetach"
	SyncerInvalidEPDetach = "SyncerInvalidEPDetach"

	ResultNegNotFound = "NegNotFound"
	SyncerNegNotFound = "SyncerNegNotFound"

	ResultCurrentEPNotFound = "CurrentEPNotFound"
	SyncerCurrentEPNotFound = "SyncerCurrentEPNotFound"

	ResultEPSNotFound = "EPSNotFound"
	SyncerEPSNotFound = "SyncerEPSNotFound"

	ResultOtherError = "OtherError"
	SyncerOtherError = "SyncerOtherError"

	ResultSuccess = "Success"
	SyncerSuccess = "SyncerSuccess"
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

func GetSyncerStatus(result string) string {
	switch result {
	case ResultEPCountsDiffer:
		return SyncerEPCountsDiffer
	case ResultEPMissingNodeName:
		return SyncerEPMissingNodeName
	case ResultNodeNotFound:
		return SyncerNodeNotFound
	case ResultEPMissingZone:
		return SyncerEPMissingZone
	case ResultEPSEndpointCountZero:
		return SyncerEPSEndpointCountZero
	case ResultEPCalculationCountZero:
		return SyncerEPCalculationCountZero
	case ResultInvalidEPAttach:
		return SyncerInvalidEPAttach
	case ResultInvalidEPDetach:
		return SyncerInvalidEPDetach
	case ResultNegNotFound:
		return SyncerNegNotFound
	case ResultCurrentEPNotFound:
		return SyncerCurrentEPNotFound
	case ResultEPSNotFound:
		return SyncerEPSNotFound
	case ResultSuccess:
		return SyncerSuccess
	default:
		return SyncerOtherError
	}
}
