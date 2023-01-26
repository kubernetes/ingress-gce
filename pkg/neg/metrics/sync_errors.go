/*
Copyright 2020 The Kubernetes Authors.

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

package metrics

import (
	"errors"
	"fmt"
)

type syncError struct {
	Err    error
	Reason string
}

func (s syncError) Error() string {
	return fmt.Sprintf("Error %v: %v", s.Reason, s.Err)
}

var (
	ErrEPCountsDiffer = syncError{
		Reason: "EPCountsDiffer",
		Err:    errors.New("endpoint counts from endpointData and endpointPodMap differ"),
	}
	ErrEPMissingNodeName = syncError{
		Reason: "EPMissingNodeName",
		Err:    errors.New("endpoint has empty nodeName"),
	}
	ErrEPMissingZone = syncError{
		Reason: "EPMissingZone",
		Err:    errors.New("endpoint has empty zone"),
	}
	ErrInvalidEPAttach = syncError{
		Reason: "InvalidEPAttach",
		Err:    errors.New("error in attach endpoint batch information"),
	}
	ErrInvalidEPDetach = syncError{
		Reason: "InvalidEPDetach",
		Err:    errors.New("error in detach endpoint batch information"),
	}
	ErrEPSEndpointCountZero = syncError{
		Reason: "EPSEndpointCountZero",
		Err:    errors.New("endpoint count from endpointData goes to zero"),
	}
	ErrEPCalculationCountZero = syncError{
		Reason: "EPCalculationCountZero",
		Err:    errors.New("endpoint count from endpointPodMap goes to zero"),
	}
	ErrNegNotFound = syncError{
		Reason: "NegNotFound",
		Err:    errors.New("fail to retrieve associated NEG"),
	}
	ErrCurrentEPNotFound = syncError{
		Reason: "CurrentEPNotFound",
		Err:    errors.New("fail to retrieve current endpointPodMap"),
	}
	ErrEPSNotFound = syncError{
		Reason: "EPSNotFound",
		Err:    errors.New("fail to retrieve endpoint slice"),
	}
	ErrNodeNotFound = syncError{
		Reason: "NodeNotFound",
		Err:    errors.New("failed to retrieve associated zone of node"),
	}
	ErrOtherError = syncError{
		Reason: "OtherError",
		Err:    errors.New("failed during sync"),
	}
	Success = syncError{
		Reason: "Success",
		Err:    nil,
	}
)
