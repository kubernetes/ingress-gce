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

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// ConnectionPreference determines how consumers are accepted.
	// +required
	ConnectionPreference string `json:"connectionPreference,omitempty"`

	// NATSubnets contains the list of subnet names for PSC
	// +required
	// +listType=atomic
	NATSubnets []string `json:"natSubnets,omitempty"`

	// ResourceRef is the reference to the K8s resource that created the forwarding rule
	// Only Services can be used as a reference
	// +required
	ResourceRef corev1.TypedLocalObjectReference `json:"resourceRef,omitempty"`

	// ProxyProtocol when set will expose client information TCP/IP information
	// +optional
	ProxyProtocol bool `json:"proxyProtocol,omitempty"`

	// ConsumerAllowList is list of consumer projects that should be allow listed
	// for this ServiceAttachment
	// +optional
	// +listType=atomic
	ConsumerAllowList []ConsumerProject `json:"consumerAllowList,omitempty"`

	// ConsumerRejectList is the list of Consumer Project IDs or Numbers that should
	// be rejected for this ServiceAttachment
	// +optional
	// +listType=atomic
	ConsumerRejectList []string `json:"consumerRejectList,omitempty"`
}

// ConsumerProject is the consumer project and project level configuration
// +k8s:openapi-gen=true
type ConsumerProject struct {
	// ConnectionLimit is the connection limit for this Consumer project
	// +optional
	ConnectionLimit int64 `json:"connectionLimit,omitempty"`

	// Project is the project id or number for the project to set the
	// limit for.
	// +required
	Project string `json:"project,omitempty"`

	// ForceSendFields is a list of field names (e.g. "ConnectionLimit") to
	// unconditionally include in API requests. By default, fields with
	// empty values are omitted from API requests. However, any non-pointer,
	// non-interface field appearing in ForceSendFields will be sent to the
	// server regardless of whether the field is empty or not. This may be
	// used to include empty fields in Patch requests.
	// +optional
	// +listType=atomic
	ForceSendFields []string `json:"forceSendFields,omitempty"`

	// NullFields is a list of field names (e.g. "ConnectionLimit") to
	// include in API requests with the JSON null value. By default, fields
	// with empty values are omitted from API requests. However, any field
	// with an empty value appearing in NullFields will be sent to the
	// server as null. It is an error if a field in this list has a
	// non-empty value. This may be used to include null fields in Patch
	// requests.
	// +optional
	// +listType=atomic
	NullFields []string `json:"nullFields,omitempty"`
}

// ServiceAttachmentStatus is the status for a ServiceAttachment resource
// +k8s:openapi-gen=true
type ServiceAttachmentStatus struct {
	// ServiceAttachmentURL is the URL for the GCE Service Attachment resource
	// +optional
	ServiceAttachmentURL string `json:"serviceAttachmentURL,omitempty"`

	// ForwardingRuleURL is the URL to the GCE Forwarding Rule resource the
	// Service Attachment points to
	// +optional
	ForwardingRuleURL string `json:"forwardingRuleURL,omitempty"`

	// Consumer Forwarding Rules using ts Service Attachment
	// +listType=atomic
	// +optional
	ConsumerForwardingRules []ConsumerForwardingRule `json:"consumerForwardingRules,omitempty"`

	// LastModifiedTimestamp tracks last time Status was updated
	// +optional
	LastModifiedTimestamp metav1.Time `json:"lastModifiedTimestamp,omitempty"`
}

// ConsumerForwardingRule is a reference to the PSC consumer forwarding rule
// +k8s:openapi-gen=true
type ConsumerForwardingRule struct {
	// Forwarding rule consumer created to use ServiceAttachment
	ForwardingRuleURL string `json:"forwardingRuleURL,omitempty"`

	// Status of consumer forwarding rule
	Status string `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// ServiceAttachmentList is a list of ServiceAttachment resources
type ServiceAttachmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ServiceAttachment `json:"items"`
}
