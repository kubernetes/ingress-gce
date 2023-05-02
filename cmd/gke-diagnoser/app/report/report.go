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

package report

import (
	"encoding/json"
)

const (
	// Passed is the constant value for the Result field of a passed check
	Passed string = "PASSED"
	// Failed is the constant value for the Result field of a failed check
	Failed string = "FAILED"
	// Skipped is the constant value for the Result field of a skipped check
	Skipped string = "SKIPPED"
)

const (
	//JSONOutput is the constant value for output type JSON
	JSONOutput string = "json"
)

// Report represents the final output of the analyzer
type Report struct {
	Resources []*Resource `json:"resources"`
}

// Resource represents the a resource of the cluster and all the checks done on it
type Resource struct {
	Kind      string   `json:"kind"`
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Checks    []*Check `json:"checks"`
}

// Check represents the result of a check
type Check struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Result  string `json:"result"`
}

func JsonReport(report *Report) (string, error) {
	jsonRaw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(jsonRaw), nil
}

// SupportedOutputs returns a string list of output formats supported by this package
func SupportedOutputs() []string {
	return []string{
		JSONOutput,
	}
}
