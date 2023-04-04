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

package labels

import (
	"errors"

	negtypes "k8s.io/ingress-gce/pkg/neg/types"
)

// PodLabelPropagationConfig contains a list of configurations for labels to be propagated to GCE network endpoints.
type PodLabelPropagationConfig struct {
	Labels []Label
}

// Label contains configuration for a label to be propagated to GCE network endpoints.
type Label struct {
	Key               string
	ShortKey          string
	MaxLabelSizeBytes int
}

// PodLabelMap is a map of pod label key, label values.
type PodLabelMap map[string]string

// EndpointPodLabelMap is a map of network endpoint, endpoint annotations.
type EndpointPodLabelMap map[negtypes.NetworkEndpoint]PodLabelMap

var (
	ErrLabelTruncated        = errors.New("label is truncated")
	ErrLabelTruncationFailed = errors.New("failed to truncate label")
)
