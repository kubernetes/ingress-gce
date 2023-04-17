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

	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils"
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

const (
	Truncated         = "truncated"
	TruncationFailure = "truncation_failed"
	OtherError        = "other_error"
)

var (
	ErrLabelTruncated        = errors.New("label is truncated")
	ErrLabelTruncationFailed = errors.New("failed to truncate label")
)

// minLabelLength defines the minimum space left for the label value.
const minLabelLength = 5

// GetPodLabelMap will return the label map extracted from a pod according to PodLabelPropagationConfig.
// The returned map has the pod label key as key and label value as value.
// This function will raise an error if pod label truncation happens or truncation fails.
func GetPodLabelMap(pod *v1.Pod, lpConfig PodLabelPropagationConfig) (PodLabelMap, error) {
	labelMap := PodLabelMap{}
	var errs []error
	for _, label := range lpConfig.Labels {
		val, ok := pod.Labels[label.Key]
		if ok {
			lpKey := label.Key
			if label.ShortKey != "" {
				lpKey = label.ShortKey
			}
			labelVal, err := truncatePodLabel(lpKey, val, label.MaxLabelSizeBytes)
			if err != nil {
				errs = append(errs, err)
				publishLabelPropagationTruncationMetrics(err)
			}

			// Add the label to the map only if the truncation result is valid
			if err == nil || errors.Is(err, ErrLabelTruncated) {
				labelMap[lpKey] = labelVal
			}
		}
	}
	if len(errs) != 0 {
		return labelMap, utils.JoinErrs(errs)
	}
	return labelMap, nil
}

// publishLabelPropagationTruncationMetrics publishes errors occured during
// label truncation.
func publishLabelPropagationTruncationMetrics(err error) {
	if errors.Is(err, ErrLabelTruncated) {
		metrics.PublishLabelPropagationError(Truncated)
	} else if errors.Is(err, ErrLabelTruncationFailed) {
		metrics.PublishLabelPropagationError(TruncationFailure)
	}
}

// truncatePodLabel calculates the potentially truncated label value to ensure that len(key) + len(label) <= maxTotalSize.
// It will return:
//
//	A string which is the truncated label value.
//	ErrLabelTruncated if truncation happens or ErrLabelTruncationFailed if truncation fails.
func truncatePodLabel(key, label string, maxTotalSize int) (string, error) {
	keyBytes := []byte(key)
	labelBytes := []byte(label)
	totalLen := len(keyBytes) + len(labelBytes)
	if totalLen <= maxTotalSize {
		return label, nil
	}
	if len(keyBytes)+minLabelLength > maxTotalSize {
		return "", fmt.Errorf("%w: `%s:%s` truncation failed because the key exceeded the limit, length: %d, limit %d", ErrLabelTruncationFailed, key, label, len(keyBytes)+minLabelLength, maxTotalSize)
	}
	truncatedVal := string(labelBytes[:maxTotalSize-len(keyBytes)])
	return truncatedVal, fmt.Errorf("%w: `%s:%s` is truncated to `%s:%s` because the total length exceeded the limit, length: %d, limit: %d", ErrLabelTruncated, key, label, key, truncatedVal, len(key)+len(label), maxTotalSize)
}

// PodLabelMapSize calculates the size of a podLabelMap.
func GetPodLabelMapSize(podLabelMap PodLabelMap) int {
	var res int
	for key, val := range podLabelMap {
		res += len([]byte(key))
		res += len([]byte(val))
	}
	return res
}
