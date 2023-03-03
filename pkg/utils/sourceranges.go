package utils

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/net"
)

const (
	allowAllIPv4Range = "0.0.0.0/0"
	allowAllIPv6Range = "0::0/0"
)

func IPv4ServiceSourceRanges(service *v1.Service) ([]string, error) {
	allRanges, err := getAllSourceRanges(service)
	if err != nil {
		return nil, err
	}

	var ipv4Ranges []string
	for _, ip := range allRanges {
		if net.IsIPv4CIDR(ip) {
			ipv4Ranges = append(ipv4Ranges, ip.String())
		}
	}
	if len(ipv4Ranges) == 0 {
		return []string{allowAllIPv4Range}, nil
	}
	return ipv4Ranges, nil
}

func IPv6ServiceSourceRanges(service *v1.Service) ([]string, error) {
	allRanges, err := getAllSourceRanges(service)
	if err != nil {
		return nil, err
	}

	var ipv6Ranges []string
	for _, ip := range allRanges {
		if net.IsIPv6CIDR(ip) {
			ipv6Ranges = append(ipv6Ranges, ip.String())
		}
	}
	if len(ipv6Ranges) == 0 {
		return []string{allowAllIPv6Range}, nil
	}
	return ipv6Ranges, nil
}

func getAllSourceRanges(service *v1.Service) (net.IPNetSet, error) {
	// if SourceRange field is specified, ignore sourceRange annotation
	if len(service.Spec.LoadBalancerSourceRanges) > 0 {
		specs := service.Spec.LoadBalancerSourceRanges
		for i, spec := range specs {
			specs[i] = strings.TrimSpace(spec)
		}
		ipnets, err := net.ParseIPNets(specs...)
		if err != nil {
			return nil, NewInvalidSpecLoadBalancerSourceRangesError(specs, err)
		}
		return ipnets, err
	}

	val, ok := service.Annotations[v1.AnnotationLoadBalancerSourceRangesKey]
	if !ok {
		return nil, nil
	}
	specs := strings.Split(val, ",")
	for i, spec := range specs {
		specs[i] = strings.TrimSpace(spec)
	}
	ipnets, err := net.ParseIPNets(specs...)
	if err != nil {
		return nil, NewInvalidLoadBalancerSourceRangesAnnotationError(val, err)
	}
	return ipnets, nil
}

// InvalidLoadBalancerSourceRangesSpecError is a struct to define error caused by
// User misconfiguration of the Service.Spec.LoadBalancerSourceRanges field.
type InvalidLoadBalancerSourceRangesSpecError struct {
	LoadBalancerSourceRangesSpec []string
	ParseErr                     error
}

func (e *InvalidLoadBalancerSourceRangesSpecError) Error() string {
	return fmt.Sprintf("service.Spec.LoadBalancerSourceRanges: %v is not valid. Expecting a list of IP ranges. For example, 10.0.0.0/24. Error msg: %v", e.LoadBalancerSourceRangesSpec, e.ParseErr)
}

func NewInvalidSpecLoadBalancerSourceRangesError(specLoadBalancerSourceRanges []string, err error) *InvalidLoadBalancerSourceRangesSpecError {
	return &InvalidLoadBalancerSourceRangesSpecError{
		specLoadBalancerSourceRanges,
		err,
	}
}

// InvalidLoadBalancerSourceRangesAnnotationError is a struct to define error caused by
// User misconfiguration of the Service.Annotations["service.beta.kubernetes.io/load-balancer-source-ranges"] field.
type InvalidLoadBalancerSourceRangesAnnotationError struct {
	LoadBalancerSourceRangesAnnotation string
	ParseErr                           error
}

func (e *InvalidLoadBalancerSourceRangesAnnotationError) Error() string {
	return fmt.Sprintf("Service annotation %s: %s is not valid. Expecting a comma-separated list of source IP ranges. For example, 10.0.0.0/24,192.168.2.0/24", v1.AnnotationLoadBalancerSourceRangesKey, e.LoadBalancerSourceRangesAnnotation)
}

func NewInvalidLoadBalancerSourceRangesAnnotationError(serviceLoadBalancerSourceRangesAnnotation string, err error) *InvalidLoadBalancerSourceRangesAnnotationError {
	return &InvalidLoadBalancerSourceRangesAnnotationError{
		serviceLoadBalancerSourceRangesAnnotation,
		err,
	}
}
