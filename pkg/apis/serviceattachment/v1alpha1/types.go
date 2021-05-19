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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	acceptAutomatic = "acceptAutomatic"
)

// ServiceAttachment represents a Service Attachment associated with a service/ingress/gateway class
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
type ServiceAttachment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceAttachmentSpec   `json:"spec,omitempty"`
	Status ServiceAttachmentStatus `json:"status,omitempty"`
}

// ServiceAttachmentSpec is the spec for a ServiceAttachment resource
// +k8s:openapi-gen=true
type ServiceAttachmentSpec struct {
	// ConnectionPreference determines how consumers are accepted. Only allowed value is `acceptAutomatic`.
	// +required
	ConnectionPreference string `json:"connectionPreference"`

	// NATSubnets contains the list of subnet names for PSC
	// +required
	// +listType=atomic
	NATSubnets []string `json:"natSubnets"`

	// ResourceRef is the reference to the K8s resource that created the forwarding rule
	// Only Services can be used as a reference
	// +required
	ResourceRef corev1.TypedLocalObjectReference `json:"resourceRef"`

	// ProxyProtocol when set will expose client information TCP/IP information (BETA Compute API)
	// +optional
	ProxyProtocol bool `json:"proxyProtocol"`
}

// ServiceAttachmentStatus is the status for a ServiceAttachment resource
// +k8s:openapi-gen=true
type ServiceAttachmentStatus struct {
	// ServiceAttachmentURL is the URL for the GCE Service Attachment resource
	// +optional
	ServiceAttachmentURL string `json:"serviceAttachmentURL"`

	// ForwardingRuleURL is the URL to the GCE Forwarding Rule resource the
	// Service Attachment points to
	// +optional
	ForwardingRuleURL string `json:"forwardingRuleURL"`

	// Consumer Forwarding Rules using ts Service Attachment (BETA Compute API)
	// +listType=atomic
	// +optional
	ConsumerForwardingRules []ConsumerForwardingRule `json:"consumerForwardingRules"`

	// LastSyncTimestamp tracks last time Status was updated
	// +optional
	LastSyncTimestamp metav1.Time `json:"lastSyncTimestamp"`
}

// ConsumerForwardingRule is a reference to the PSC consumer forwarding rule
// +k8s:openapi-gen=true
type ConsumerForwardingRule struct {
	// Forwarding rule consumer created to use ServiceAttachment
	ForwardingRuleURL string `json:"forwardingRuleURL"`

	// Status of consumer forwarding rule
	Status string `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ServiceAttachmentList is a list of ServiceAttachment resources
type ServiceAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServiceAttachment `json:"items"`
}
