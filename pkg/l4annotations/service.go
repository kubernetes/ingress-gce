/*
Copyright 2017 The Kubernetes Authors.

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

package l4annotations

import (
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/negannotation"
)

const (
	// NetworkTierAnnotationKey is annotated on a Service object to indicate which
	// network tier a GCP LB should use.
	// The valid values are "Standard" and "Premium" (default, if unspecified).
	NetworkTierAnnotationKey = "cloud.google.com/network-tier"

	IPv6Suffix = "-ipv6"
	// ServiceStatusPrefix is the prefix used in annotations used to record
	// debug information in the Service annotations. This is applicable to L4 ILB services.
	ServiceStatusPrefix = "service.kubernetes.io"
	// TCPForwardingRuleKey is the annotation key used by l4 controller to record
	// GCP TCP forwarding rule name.
	TCPForwardingRuleKey = ServiceStatusPrefix + "/tcp-" + ForwardingRuleResource
	// UDPForwardingRuleKey is the annotation key used by l4 controller to record
	// GCP UDP forwarding rule name.
	UDPForwardingRuleKey = ServiceStatusPrefix + "/udp-" + ForwardingRuleResource
	// L3ForwardingRuleKey is the annotation key used by l4 controller to record
	// GCP L3_DEFAULT forwarding rule name.
	L3ForwardingRuleKey = ServiceStatusPrefix + "/l3-" + ForwardingRuleResource
	// TCPForwardingRuleIPv6Key is the annotation key used by l4 controller to record
	// GCP IPv6 TCP forwarding rule name.
	TCPForwardingRuleIPv6Key = TCPForwardingRuleKey + IPv6Suffix
	// UDPForwardingRuleIPv6Key is the annotation key used by l4 controller to record
	// GCP IPv6 UDP forwarding rule name.
	UDPForwardingRuleIPv6Key = UDPForwardingRuleKey + IPv6Suffix
	// L3ForwardingRuleIPv6Key is the annotation key used by l4 controller to record
	// GCP IPv6 L3_DEFAULT forwarding rule name.
	L3ForwardingRuleIPv6Key = L3ForwardingRuleKey + IPv6Suffix
	// BackendServiceKey is the annotation key used by l4 controller to record
	// GCP Backend service name.
	BackendServiceKey = ServiceStatusPrefix + "/" + BackendServiceResource
	// FirewallRuleKey is the annotation key used by l4 controller to record
	// GCP Firewall rule name.
	FirewallRuleKey = ServiceStatusPrefix + "/" + FirewallRuleResource
	// FirewallRuleDenyKey is the annotation key used by l4 controllers to record
	// a GCP Deny Firewall rule name
	FirewallRuleDenyKey = FirewallRuleKey + "-deny"
	// FirewallRuleIPv6Key is the annotation key used by l4 controller to record
	// GCP IPv6 Firewall rule name.
	FirewallRuleIPv6Key = FirewallRuleKey + IPv6Suffix
	// FirewallRuleDenyIPv6Key is the annotation key used by l4 controllers to record
	// a GCP IPv6 Deny Firewall rule name
	FirewallRuleDenyIPv6Key = FirewallRuleDenyKey + IPv6Suffix
	// HealthcheckKey is the annotation key used by l4 controller to record
	// GCP Healthcheck name.
	HealthcheckKey = ServiceStatusPrefix + "/" + HealthcheckResource
	// FirewallRuleForHealthcheckKey is the annotation key used by l4 controller to record
	// the firewall rule name that allows healthcheck traffic.
	FirewallRuleForHealthcheckKey = ServiceStatusPrefix + "/" + FirewallForHealthcheckResource
	// FirewallRuleForHealthcheckIPv6Key is the annotation key used by l4 controller to record
	// the firewall rule name that allows IPv6 healthcheck traffic.
	FirewallRuleForHealthcheckIPv6Key  = FirewallRuleForHealthcheckKey + IPv6Suffix
	ForwardingRuleResource             = "forwarding-rule"
	ForwardingRuleIPv6Resource         = ForwardingRuleResource + IPv6Suffix
	BackendServiceResource             = "backend-service"
	FirewallRuleResource               = "firewall-rule"
	FirewallDenyRuleResource           = "firewall-rule-deny"
	FirewallRuleIPv6Resource           = FirewallRuleResource + IPv6Suffix
	FirewallDenyRuleIPv6Resource       = FirewallDenyRuleResource + IPv6Suffix
	HealthcheckResource                = "healthcheck"
	FirewallForHealthcheckResource     = "firewall-rule-for-hc"
	FirewallForHealthcheckIPv6Resource = FirewallRuleForHealthcheckKey + IPv6Suffix
	AddressResource                    = "address"
	// TODO(slavik): import this from gce_annotations when it will be merged in k8s
	RBSAnnotationKey = "cloud.google.com/l4-rbs"
	RBSEnabled       = "enabled"
	// StrongSessionAffinity is a restricted feature that is enabled on
	// allow-listed projects only. If you need access to this feature for your
	// External L4 Load Balancer, please contact Google Cloud support team.
	StrongSessionAffinityAnnotationKey = "networking.gke.io/l4-strong-session-affinity"
	StrongSessionAffinityEnabled       = "enabled"
	// CustomSubnetAnnotationKey is the new way to specify custom subnet both for ILB and NetLB (only for IPv6)
	// Replaces networking.gke.io/internal-load-balancer-subnet with backward compatibility.
	CustomSubnetAnnotationKey = "networking.gke.io/load-balancer-subnet"

	// Service annotation key for using the Weighted load balancing in both ILB and NetlB
	WeightedL4AnnotationKey = "networking.gke.io/weighted-load-balancing"
	// Service annotation value for using pods-per-node Weighted load balancing in both ILB and NetlB
	WeightedL4AnnotationPodsPerNode = "pods-per-node"

	// Service annotation key for specifying config map which contains logging config
	L4LoggingConfigMapKey = "networking.gke.io/l4-logging-config-map"

	// Service annotation key for specifying connection draining timeout for L4 backend services
	// Supports time.Duration format (e.g., "300s", "5m", "1h")
	ConnectionDrainingTimeoutKey = "networking.gke.io/connection-draining-timeout"
)

// Service represents Service annotations.
type Service struct {
	v map[string]string
}

// FromService extracts the annotations from an Service definition.
func FromService(obj *v1.Service) *Service {
	return &Service{obj.Annotations}
}

// WantsL4ILB checks if the given service requires L4 ILB.
// the function returns a boolean as well as the loadbalancer type(string).
func WantsL4ILB(service *v1.Service) (bool, string) {
	if service == nil {
		return false, ""
	}
	if service.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false, fmt.Sprintf("Type : %s", service.Spec.Type)
	}
	if service.Spec.LoadBalancerClass != nil {
		return HasLoadBalancerClass(service, RegionalInternalLoadBalancerClass), fmt.Sprintf("Type : %s", service.Spec.Type)
	}
	ltype := GetLoadBalancerAnnotationType(service)
	if ltype == LBTypeInternal {
		return true, fmt.Sprintf("Type : %s, LBType : %s", service.Spec.Type, ltype)
	}
	return false, fmt.Sprintf("Type : %s, LBType : %s", service.Spec.Type, ltype)
}

// WantsL4NetLB checks if the given service requires L4 NetLb.
func WantsL4NetLB(service *v1.Service) (bool, string) {
	if service == nil {
		return false, ""
	}
	if service.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false, fmt.Sprintf("Type : %s", service.Spec.Type)
	}
	if service.Spec.LoadBalancerClass != nil {
		return HasLoadBalancerClass(service, RegionalExternalLoadBalancerClass), fmt.Sprintf("Type : %s", service.Spec.Type)
	}
	ltype := GetLoadBalancerAnnotationType(service)
	return ltype != LBTypeInternal, fmt.Sprintf("Type : %s, LBType : %s", service.Spec.Type, ltype)
}

// HasRBSAnnotation checks if the given service has the RBS annotation.
func HasRBSAnnotation(service *v1.Service) bool {
	if service == nil {
		return false
	}

	if val, ok := service.Annotations[RBSAnnotationKey]; ok && val == RBSEnabled {
		return true
	}
	return false
}

// HasStrongSessionAffinityAnnotation checks if the given service has the strong session affinity annotation.
func HasStrongSessionAffinityAnnotation(service *v1.Service) bool {
	if service == nil {
		return false
	}

	if val, ok := service.Annotations[StrongSessionAffinityAnnotationKey]; ok && val == StrongSessionAffinityEnabled {
		return true
	}
	return false
}

// HasWeightedLBPodsPerNodeAnnotation checks if the given service has pods-per-node Weighted load balancing annotation
func HasWeightedLBPodsPerNodeAnnotation(service *v1.Service) bool {
	if service == nil {
		return false
	}
	if val, ok := service.Annotations[WeightedL4AnnotationKey]; ok && val == WeightedL4AnnotationPodsPerNode {
		return true
	}
	return false
}

// HasLoadBalancerClass checks if the given service has a specific loadBalancerClass set.
func HasLoadBalancerClass(service *v1.Service, key string) bool {
	if service.Spec.LoadBalancerClass != nil {
		if *service.Spec.LoadBalancerClass == key {
			return true
		}
	}
	return false
}

// OnlyStatusAnnotationsChanged returns true if the only annotation change between the 2 services is the NEG or ILB
// resources annotations.
// Note : This assumes that the annotations in old and new service are different. If they are identical, this will
// return true.
func OnlyStatusAnnotationsChanged(oldService, newService *v1.Service) bool {
	return onlyStatusAnnotationsChanged(oldService, newService) && onlyStatusAnnotationsChanged(newService, oldService)
}

// onlyStatusAnnotationsChanged returns true if the NEG Status or ILB resources annotations are the only extra
// annotations present in the new service but not in the old service.
// Note : This assumes that the annotations in old and new service are different. If they are identical, this will
// return true.
func onlyStatusAnnotationsChanged(oldService, newService *v1.Service) bool {
	for key, val := range newService.ObjectMeta.Annotations {
		if oldVal, ok := oldService.ObjectMeta.Annotations[key]; !ok || oldVal != val {
			if key == negannotation.NEGStatusKey || strings.HasPrefix(key, ServiceStatusPrefix) {
				continue
			}
			return false
		}
	}
	return true
}

// GetL4LoggingConfigMapAnnotation returns name of the config map which contains logging config.
// Returns false if annotation with the name is not specified.
func (svc *Service) GetL4LoggingConfigMapAnnotation() (string, bool) {
	val, ok := svc.v[L4LoggingConfigMapKey]
	if ok {
		return val, ok
	}
	return "", false
}

// GetExternalLoadBalancerAnnotationSubnet returns the configured subnet to assign LoadBalancer IP from.
// Currently useful only for IPv6 External LoadBalancers.
func (svc *Service) GetExternalLoadBalancerAnnotationSubnet() string {
	if val, exists := svc.v[CustomSubnetAnnotationKey]; exists {
		return val
	}
	return ""
}

// GetInternalLoadBalancerAnnotationSubnet returns the configured subnet to assign LoadBalancer IP from.
func (svc *Service) GetInternalLoadBalancerAnnotationSubnet() string {
	// At first try new annotation
	if val, exists := svc.v[CustomSubnetAnnotationKey]; exists {
		return val
	}
	// Fallback to old ILB annotation
	if val, exists := svc.v[gce.ServiceAnnotationILBSubnet]; exists {
		return val
	}
	return ""
}

// GetConnectionDrainingTimeout returns the connection draining timeout in seconds.
// Returns 0 and false if the annotation is not specified or invalid.
// Accepts time.Duration format (e.g., "300s", "5m", "1h").
// Returns the parsed value in seconds (0-3600) and true if valid.
// GCP supports connection draining timeout values from 0 to 3600 seconds.
// Fractional seconds (e.g., "100ms", "1.5s") are not supported and will be rejected.
func (svc *Service) GetConnectionDrainingTimeout() (int64, bool) {
	val, exists := svc.v[ConnectionDrainingTimeoutKey]
	if !exists {
		return 0, false
	}

	duration, err := time.ParseDuration(val)
	if err != nil {
		return 0, false
	}

	// Validate that duration is a whole number of seconds (no fractional seconds)
	if duration%time.Second != 0 {
		return 0, false
	}

	// Convert to seconds
	timeoutSec := int64(duration.Seconds())

	// Validate range: GCP allows 0-3600 seconds
	if timeoutSec < 0 || timeoutSec > 3600 {
		return 0, false
	}

	return timeoutSec, true
}
