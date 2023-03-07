package servicemetrics

import (
	"fmt"
	"k8s.io/utils/net"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	// Names of the labels used by the service metrics.
	labelType                  = "type"
	labelExternalTrafficPolicy = "external_traffic_policy"
	labelInternalTrafficPolicy = "internal_traffic_policy"
	labelSessionAffinityConfig = "session_affinity_config"
	labelProtocol              = "protocol"
	labelIPFamilies            = "ip_families"
	labelIPFamilyPolicy        = "ip_family_policy"
	labelIsStaticIPv4          = "is_static_ip_v4"
	labelIsStaticIPv6          = "is_static_ip_v6"
	labelNetworkTier           = "network_tier"
	labelGlobalAccess          = "global_access"
	labelCustomSubnet          = "custom_subnet"
	labelNumberOfPorts         = "number_of_ports"

	// possible values for the service_type label
	serviceTypeSubsettingILB = "SubsettingILB"
	serviceTypeRBSXLB        = "RBSXLB"
	serviceTypeLegacyILB     = "LegacyILB"
	serviceTypeLegacyXLB     = "LegacyXLB"

	// sessionAffinityTimeoutDefault is the default timeout value for a service session affinity.
	sessionAffinityTimeoutDefault = 10800

	//  possible values for the session_affinity_config label.
	sessionAffinityBucketMoreThanDefault = "10800+"
	sessionAffinityBucketDefault         = "10800"
	sessionAffinityBucketLessThanDefault = "0-10799"
	sessionAffinityBucketNone            = "None"
)

var (
	serviceL4ProtocolStatsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_l4_protocol_stats",
			Help: "Number of services broken down by various stats",
		},
		[]string{
			labelType,
			labelExternalTrafficPolicy,
			labelInternalTrafficPolicy,
			labelSessionAffinityConfig,
			labelProtocol,
			labelNumberOfPorts,
		},
	)
	serviceIPStackStatsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_ip_stack_stats",
			Help: "Number of services broken down by various stats",
		},
		[]string{
			labelType,
			labelExternalTrafficPolicy,
			labelInternalTrafficPolicy,
			labelIPFamilies,
			labelIPFamilyPolicy,
			labelIsStaticIPv4,
			labelIsStaticIPv6,
		},
	)
	serviceGCPFeaturesStatsCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_gcp_features_stats",
			Help: "Number of services broken down by various stats",
		},
		[]string{
			labelType,
			labelNetworkTier,
			labelGlobalAccess,
			labelCustomSubnet,
		},
	)
)

func init() {
	klog.V(3).Infof("Registering Service stats usage metrics %v", serviceL4ProtocolStatsCount)
	prometheus.MustRegister(serviceL4ProtocolStatsCount)
	prometheus.MustRegister(serviceIPStackStatsCount)
	prometheus.MustRegister(serviceGCPFeaturesStatsCount)
}

// Controller is the controller that exposes and populates metrics containing various stats about Services in the cluster.
type Controller struct {
	ctx             *context.ControllerContext
	stopCh          chan struct{}
	svcQueue        utils.TaskQueue
	metricsInterval time.Duration
	serviceInformer cache.SharedIndexInformer
}

// NewController creates a new Controller.
func NewController(ctx *context.ControllerContext, exportInterval time.Duration, stopCh chan struct{}) *Controller {
	svcMetrics := &Controller{
		ctx:             ctx,
		stopCh:          stopCh,
		serviceInformer: ctx.ServiceInformer,
		metricsInterval: exportInterval,
	}
	return svcMetrics
}

// Run starts the controller until stopped via the stop channel.
func (c *Controller) Run() {
	klog.Infof("Starting Service Metric Stats controller")
	go func() {
		time.Sleep(c.metricsInterval)
		wait.Until(c.export, c.metricsInterval, c.stopCh)
	}()
	<-c.stopCh
}

// serviceL4ProtocolMetricState defines metric state related to the L4 protocol
// related part of services.
type serviceL4ProtocolMetricState struct {
	Type                  string
	ExternalTrafficPolicy string
	InternalTrafficPolicy string
	SessionAffinityConfig string
	NumberOfPorts         string
	Protocol              string
}

// serviceIPStackMetricState defines metric state related to the IP stack of services.
type serviceIPStackMetricState struct {
	Type                  string
	ExternalTrafficPolicy string
	InternalTrafficPolicy string
	IPFamilies            string
	IPFamilyPolicy        string
	IsStaticIPv4          bool
	IsStaticIPv6          bool
}

// serviceGCPFeaturesMetricState defines metric state related to the GCP
// specific features of services.
type serviceGCPFeaturesMetricState struct {
	Type         string
	NetworkTier  string
	GlobalAccess bool
	CustomSubnet bool
}

func (c *Controller) export() {
	serviceLister := c.serviceInformer.GetIndexer()
	allServices, err := listers.NewServiceLister(serviceLister).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list services err=%v", err)
		return
	}

	l4ProtocolState, ipStackState, gcpFeaturesState := calculateMetrics(allServices)

	updatePrometheusMetrics(l4ProtocolState, ipStackState, gcpFeaturesState)
}

func calculateMetrics(services []*v1.Service) (map[serviceL4ProtocolMetricState]int64, map[serviceIPStackMetricState]int64, map[serviceGCPFeaturesMetricState]int64) {
	l4ProtocolState := make(map[serviceL4ProtocolMetricState]int64)
	ipStackState := make(map[serviceIPStackMetricState]int64)
	gcpFeaturesState := make(map[serviceGCPFeaturesMetricState]int64)

	for _, service := range services {
		l4Protocol, ipStack, gcpFeatures := metricsFromService(service)
		l4ProtocolState[*l4Protocol]++
		ipStackState[*ipStack]++
		gcpFeaturesState[*gcpFeatures]++
	}
	return l4ProtocolState, ipStackState, gcpFeaturesState
}

func updatePrometheusMetrics(l4ProtocolState map[serviceL4ProtocolMetricState]int64, ipStackState map[serviceIPStackMetricState]int64, gcpFeaturesState map[serviceGCPFeaturesMetricState]int64) {
	for serviceStat, count := range l4ProtocolState {
		serviceL4ProtocolStatsCount.With(prometheus.Labels{
			labelType:                  serviceStat.Type,
			labelExternalTrafficPolicy: serviceStat.ExternalTrafficPolicy,
			labelInternalTrafficPolicy: serviceStat.InternalTrafficPolicy,
			labelSessionAffinityConfig: serviceStat.SessionAffinityConfig,
			labelProtocol:              serviceStat.Protocol,
			labelNumberOfPorts:         serviceStat.NumberOfPorts,
		}).Set(float64(count))
	}

	for serviceStat, count := range ipStackState {
		serviceIPStackStatsCount.With(prometheus.Labels{
			labelType:                  serviceStat.Type,
			labelExternalTrafficPolicy: serviceStat.ExternalTrafficPolicy,
			labelInternalTrafficPolicy: serviceStat.InternalTrafficPolicy,
			labelIPFamilies:            serviceStat.IPFamilies,
			labelIPFamilyPolicy:        serviceStat.IPFamilyPolicy,
			labelIsStaticIPv4:          strconv.FormatBool(serviceStat.IsStaticIPv4),
			labelIsStaticIPv6:          strconv.FormatBool(serviceStat.IsStaticIPv6),
		}).Set(float64(count))
	}

	for serviceStat, count := range gcpFeaturesState {
		serviceGCPFeaturesStatsCount.With(prometheus.Labels{
			labelType:         serviceStat.Type,
			labelNetworkTier:  serviceStat.NetworkTier,
			labelGlobalAccess: strconv.FormatBool(serviceStat.GlobalAccess),
			labelCustomSubnet: strconv.FormatBool(serviceStat.CustomSubnet),
		}).Set(float64(count))
	}
}

func metricsFromService(service *v1.Service) (*serviceL4ProtocolMetricState, *serviceIPStackMetricState, *serviceGCPFeaturesMetricState) {
	serviceType := getServiceType(service)
	internalTrafficPolicy := getInternalTrafficPolicy(service)
	externalTrafficPolicy := getExternalTrafficPolicy(service)
	l4Protocol := &serviceL4ProtocolMetricState{
		Type:                  serviceType,
		ExternalTrafficPolicy: externalTrafficPolicy,
		InternalTrafficPolicy: internalTrafficPolicy,
		SessionAffinityConfig: getSessionAffinityConfig(service),
		NumberOfPorts:         getPortsBucket(service.Spec.Ports),
		Protocol:              getProtocol(service.Spec.Ports),
	}
	ipStack := &serviceIPStackMetricState{
		Type:                  serviceType,
		ExternalTrafficPolicy: externalTrafficPolicy,
		InternalTrafficPolicy: internalTrafficPolicy,
		IPFamilies:            getIPFamilies(service.Spec.IPFamilies),
		IPFamilyPolicy:        getIPFamilyPolicy(service.Spec.IPFamilyPolicy),
		IsStaticIPv4:          isStaticIPv4(service.Spec.LoadBalancerIP),
		IsStaticIPv6:          isStaticIPv6(service.Spec.LoadBalancerIP),
	}
	netTier, _ := utils.GetNetworkTier(service)
	gcpFeatures := &serviceGCPFeaturesMetricState{
		Type:         serviceType,
		NetworkTier:  string(netTier),
		GlobalAccess: gce.GetLoadBalancerAnnotationAllowGlobalAccess(service),
		CustomSubnet: gce.GetLoadBalancerAnnotationSubnet(service) != "",
	}
	return l4Protocol, ipStack, gcpFeatures
}

func isStaticIPv6(loadBalancerIP string) bool {
	return loadBalancerIP != "" && net.IsIPv6String(loadBalancerIP)
}

func isStaticIPv4(loadBalancerIP string) bool {
	return loadBalancerIP != "" && net.IsIPv4String(loadBalancerIP)
}

func getExternalTrafficPolicy(service *v1.Service) string {
	if service.Spec.ExternalTrafficPolicy == "" {
		return string(v1.ServiceExternalTrafficPolicyTypeCluster)
	}
	return string(service.Spec.ExternalTrafficPolicy)
}

func getInternalTrafficPolicy(service *v1.Service) string {
	if service.Spec.InternalTrafficPolicy == nil {
		return string(v1.ServiceInternalTrafficPolicyCluster)
	}
	return string(*service.Spec.InternalTrafficPolicy)
}

func getPortsBucket(ports []v1.ServicePort) string {
	n := len(ports)
	if n <= 1 {
		return fmt.Sprint(n)
	}
	if n <= 5 {
		return "2-5"
	}
	if n <= 100 {
		return "6-100"
	}
	return "100+"
}

func protocolOrDefault(port v1.ServicePort) string {
	if port.Protocol == "" {
		return string(v1.ProtocolTCP)
	}
	return string(port.Protocol)
}

func getProtocol(ports []v1.ServicePort) string {
	if len(ports) == 0 {
		return ""
	}
	protocol := protocolOrDefault(ports[0])
	for _, port := range ports {
		if protocol != protocolOrDefault(port) {
			return "mixed"
		}
	}
	return protocol
}

func getIPFamilies(families []v1.IPFamily) string {
	if len(families) == 2 {
		return fmt.Sprintf("%s-%s", string(families[0]), string(families[1]))
	}
	return string(families[0])
}

func getIPFamilyPolicy(policyType *v1.IPFamilyPolicyType) string {
	if policyType == nil {
		return string(v1.IPFamilyPolicySingleStack)
	}
	return string(*policyType)
}

func getServiceType(service *v1.Service) string {
	if service.Spec.Type != v1.ServiceTypeLoadBalancer {
		return string(service.Spec.Type)
	}
	wantsL4ILB, _ := annotations.WantsL4ILB(service)

	if wantsL4ILB {
		if common.HasGivenFinalizer(service.ObjectMeta, common.ILBFinalizerV2) {
			return serviceTypeSubsettingILB
		}
		return serviceTypeLegacyILB
	}
	wantsL4NetLB, _ := annotations.WantsL4NetLB(service)
	if wantsL4NetLB {
		if common.HasGivenFinalizer(service.ObjectMeta, common.NetLBFinalizerV2) {
			return serviceTypeRBSXLB
		}
		return serviceTypeLegacyXLB
	}
	return ""
}

func getSessionAffinityConfig(service *v1.Service) string {
	if service.Spec.SessionAffinity != v1.ServiceAffinityClientIP {
		return sessionAffinityBucketNone
	}
	if service.Spec.SessionAffinityConfig == nil ||
		service.Spec.SessionAffinityConfig.ClientIP == nil ||
		service.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds == nil {
		return sessionAffinityBucketDefault
	}
	timeout := *service.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds

	if timeout < sessionAffinityTimeoutDefault {
		return sessionAffinityBucketLessThanDefault
	}
	if timeout == sessionAffinityTimeoutDefault {
		return sessionAffinityBucketDefault
	}
	return sessionAffinityBucketMoreThanDefault
}
