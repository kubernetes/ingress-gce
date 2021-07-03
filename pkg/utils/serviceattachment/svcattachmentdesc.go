/*
Copyright 2021 The Kubernetes Authors.

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

package serviceattachment

import (
	"encoding/json"

	"k8s.io/ingress-gce/pkg/utils/descutils"
	"k8s.io/klog"
)

// ServiceAttachmentDesc stores the description for a Service Attachment.
type ServiceAttachmentDesc struct {
	K8sResource string `json:"k8sResource,omitempty"`
	K8sCluster  string `json:"k8sCluster,omitempty"`
}

func NewServiceAttachmentDesc(namespace, crName, clusterName, location string, regional bool) ServiceAttachmentDesc {
	return ServiceAttachmentDesc{
		K8sResource: descutils.GenerateK8sResourceLink(namespace, "serviceattachments", crName),
		K8sCluster:  descutils.GenerateClusterLink(clusterName, location, regional),
	}
}

// String returns the string representation of a Description.
func (desc ServiceAttachmentDesc) String() string {
	descJson, err := json.Marshal(desc)
	if err != nil {
		klog.Errorf("Failed to generate neg description string: %v, falling back to empty string", err)
		return ""
	}
	return string(descJson)
}

// DescriptionFromString gets a Description from string,
func ServiceAttachmentDescFromString(descString string) (*ServiceAttachmentDesc, error) {
	var desc ServiceAttachmentDesc
	if err := json.Unmarshal([]byte(descString), &desc); err != nil {
		klog.Errorf("Failed to parse service attachment description: %s, falling back to empty list", descString)
		return &ServiceAttachmentDesc{}, err
	}
	return &desc, nil
}
