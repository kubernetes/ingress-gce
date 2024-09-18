/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:storageversion
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GKENetworkParamSet represent GKE specific parameters for the network.
type GKENetworkParamSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GKENetworkParamSetSpec   `json:"spec,omitempty"`
	Status GKENetworkParamSetStatus `json:"status,omitempty"`
}

// DeviceModeType defines mode in which the devices will be used by the Pod
// +kubebuilder:validation:Enum=DPDK-VFIO;NetDevice;RDMA
type DeviceModeType string

const (
	// DPDKVFIO indicates that NICs are bound to vfio-pci driver
	DPDKVFIO DeviceModeType = "DPDK-VFIO"
	// NetDevice indicates that NICs are bound to kernel driver and used as net device
	NetDevice DeviceModeType = "NetDevice"
	// RDMA indicates that NICs support RDMA
	RDMA DeviceModeType = "RDMA"
)

// SecondaryRanges represents ranges of network addresses.
type SecondaryRanges struct {
	// +kubebuilder:validation:MinItems:=1
	RangeNames []string `json:"rangeNames"`
}

// GKENetworkParamSetSpec contains the specifications for network object
type GKENetworkParamSetSpec struct {
	// VPC specifies the VPC to which the network belongs. Mutually exclusive with NetworkAttachment.
	// This field is required when not connecting across a network attachment
	// +optional
	VPC string `json:"vpc,omitempty"`

	// VPCSubnet is the path of the VPC subnet. Must be set if specifying VPC. Mutually exclusive with
	// NetworkAttachment. This field is required when not connecting across a network attachment
	// +optional
	VPCSubnet string `json:"vpcSubnet,omitempty"`

	// DeviceMode indicates the mode in which the devices will be used by the Pod.
	// This field is required and valid only for "Device" typed network. Mutually exclusive with
	// NetworkAttachment
	// +optional
	DeviceMode DeviceModeType `json:"deviceMode,omitempty"`

	// PodIPv4Ranges specify the names of the secondary ranges of the VPC subnet
	// used to allocate pod IPs for the network.
	// This field is required and valid only for L3 typed network. Mutually exclusive with
	// NetworkAttachment
	// +optional
	PodIPv4Ranges *SecondaryRanges `json:"podIPv4Ranges,omitempty"`

	// NetworkAttachment specifies the network attachment to connect to. Mutually exclusive with VPC,
	// VPCSubnet, DeviceMode, and PodIPv4Ranges
	// +optional
	NetworkAttachment string `json:"networkAttachment,omitempty"`
}

// NetworkRanges represents ranges of network addresses.
type NetworkRanges struct {
	// +kubebuilder:validation:MinItems:=1
	CIDRBlocks []string `json:"cidrBlocks"`
}

// GKENetworkParamSetConditionType is the type for status conditions on
// a GKENetworkParamSet. This type should be used with the
// GKENetworkParamSetStatus.Conditions field.
type GKENetworkParamSetConditionType string

const (
	// GKENetworkParamSetStatusReady is the condition type that holds
	// if the GKENetworkParamSet object is validated
	GKENetworkParamSetStatusReady GKENetworkParamSetConditionType = "Ready"
)

// GKENetworkParamSetConditionReason defines the set of reasons that explain why a
// particular GKENetworkParamSet condition type has been raised.
type GKENetworkParamSetConditionReason string

const (
	// VPCNotFound indicates that the specified VPC was not found.
	VPCNotFound GKENetworkParamSetConditionReason = "VPCNotFound"
	// SubnetNotFound indicates that the specified subnet was not found.
	SubnetNotFound GKENetworkParamSetConditionReason = "SubnetNotFound"
	// SecondaryRangeAndDeviceModeUnspecified indicates that the user didn't specify either a device mode or secondary range
	SecondaryRangeAndDeviceModeUnspecified GKENetworkParamSetConditionReason = "SecondaryRangeAndDeviceModeUnspecified"
	// SecondaryRangeNotFound indicates that the specified secondary range was not found.
	SecondaryRangeNotFound GKENetworkParamSetConditionReason = "SecondaryRangeNotFound"
	// DeviceModeCantBeUsedWithSecondaryRange indicates that device mode was used with a secondary range.
	DeviceModeCantBeUsedWithSecondaryRange GKENetworkParamSetConditionReason = "DeviceModeCantBeUsedWithSecondaryRange"
	// DeviceModeVPCAlreadyInUse indicates that the VPC is already in use by another GKENetworkParamSet resource.
	DeviceModeVPCAlreadyInUse GKENetworkParamSetConditionReason = "DeviceModeVPCAlreadyInUse"
	// DeviceModeSubnetAlreadyInUse indicates that the Subnet is already in use by another GKENetworkParamSet resource.
	DeviceModeSubnetAlreadyInUse GKENetworkParamSetConditionReason = "DeviceModeSubnetAlreadyInUse"
	// DeviceModeCantUseDefaultVPC indicates that a device mode GKENetworkParamSet cannot use the default VPC.
	DeviceModeCantUseDefaultVPC GKENetworkParamSetConditionReason = "DeviceModeCantUseDefaultVPC"
	// DPDKUnsupported indicates that DPDK device mode is not supported on the current cluster.
	DPDKUnsupported GKENetworkParamSetConditionReason = "DPDKUnsupported"
	// GNPReady indicates that this GNP resource has been successfully validated and Ready=True
	GNPReady GKENetworkParamSetConditionReason = "GNPReady"
	// NetworkAttachmentInvalid indicates that the specified network attachment is invalid.
	NetworkAttachmentInvalid GKENetworkParamSetConditionReason = "NetworkAttachmentInvalid"
	// GNPConfigInvalid indicates that the GNP config has an invalid combination of fields specified.
	GNPConfigInvalid GKENetworkParamSetConditionReason = "GNPConfigInvalid"
)

// GNPNetworkParamsReadyConditionReason defines the set of reasons that explains
// the ParamsReady condition on the referencing Network resource.
type GNPNetworkParamsReadyConditionReason string

const (
	// L3SecondaryMissing indicates that the L3 type Network resource is
	// referencing a GKENetworkParamSet with secondary range unspecified.
	L3SecondaryMissing GNPNetworkParamsReadyConditionReason = "L3SecondaryMissing"
	// NetworkAttachmentUnsupported indicates that the Network does not support
	// referencing a GKENetworkParamSet with NetworkAttachment specified.
	NetworkAttachmentUnsupported GNPNetworkParamsReadyConditionReason = "NetworkAttachmentUnsupported"
	// DeviceModeMissing indicates that the Device type Network resource is
	// referencing a GKENetworkParamSet with device mode unspecified.
	DeviceModeMissing GNPNetworkParamsReadyConditionReason = "DeviceModeMissing"
	// GNPDeleted indicates that the referenced GNP resource was deleted
	GNPDeleted GNPNetworkParamsReadyConditionReason = "GNPDeleted"
	// GNPParamsReady indicates that the referenced GNP resource
	// has been successfully validated for use with this Network resource and ParamsReady=True
	GNPParamsReady GNPNetworkParamsReadyConditionReason = "GNPParamsReady"
	// GNPParamsNotReady indicates that the referenced GNP resource
	// needs to be updated and triggers Network resource to update
	GNPParamsNotReady GNPNetworkParamsReadyConditionReason = "GNPParamsNotReady"
)

// GKENetworkParamSetStatus contains the status information related to the network.
type GKENetworkParamSetStatus struct {
	// PodCIDRs specifies the CIDRs from which IPs will be used for Pod interfaces
	// +optional
	PodCIDRs *NetworkRanges `json:"podCIDRs,omitempty"`

	// Conditions is a field representing the current conditions of the GKENetworkParamSet.
	//
	// Known condition types are:
	//
	// * "Ready"
	//
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// NetworkName specifies which Network object is currently referencing this GKENetworkParamSet
	// +optional
	NetworkName string `json:"networkName"`
}

// +genclient
// +genclient:nonNamespaced
// +genclient:onlyVerbs=get
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GKENetworkParamSetList contains a list of GKENetworkParamSet resources.
type GKENetworkParamSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a slice of GKENetworkParamset resources.
	Items []GKENetworkParamSet `json:"items"`
}
