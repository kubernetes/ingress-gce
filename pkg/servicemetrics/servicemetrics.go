package servicemetrics

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/metrics"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

// Controller is the controller that watches Service resources and populates metrics containing various stats about them.
type Controller struct {
	ctx      *context.ControllerContext
	stopCh   chan struct{}
	svcQueue utils.TaskQueue
}

// NewController creates a new Controller.
func NewController(ctx *context.ControllerContext, stopCh chan struct{}) *Controller {
	svcMetrics := &Controller{
		ctx:    ctx,
		stopCh: stopCh,
	}
	svcQueue := utils.NewPeriodicTaskQueueWithMultipleWorkers("serviceMetrics", "services", ctx.NumServiceMetricWorkers, svcMetrics.sync)
	svcMetrics.svcQueue = svcQueue
	ctx.ServiceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			addSvc := obj.(*v1.Service)
			svcQueue.Enqueue(addSvc)

		},
		UpdateFunc: func(old, cur interface{}) {
			curSvc := cur.(*v1.Service)
			svcQueue.Enqueue(curSvc)
		},
		DeleteFunc: func(obj interface{}) {
			delSvc := obj.(*v1.Service)
			svcQueue.Enqueue(delSvc)
		},
	})
	return svcMetrics
}

// Run starts the controller until stopped via the stop channel.
func (c *Controller) Run() {
	klog.Infof("Starting Service Metric Stats controller")
	defer c.svcQueue.Shutdown()
	c.svcQueue.Run()
	<-c.stopCh
}

func (c *Controller) sync(key string) error {
	service, exists, err := c.ctx.Services().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists || service.ObjectMeta.DeletionTimestamp != nil {
		c.syncDeletion(key)
		return nil
	}

	svcKey := utils.ServiceKeyFunc(service.Namespace, service.Name)
	serviceType := getServiceType(service)
	internalTrafficPolicy := getInternalTrafficPolicy(service)
	externalTrafficPolicy := getExternalTrafficPolicy(service)
	l4Protocol := metrics.ServiceL4ProtocolMetricState{
		Type:                  serviceType,
		ExternalTrafficPolicy: externalTrafficPolicy,
		InternalTrafficPolicy: internalTrafficPolicy,
		SessionAffinityConfig: getSessionAffinityConfig(service),
		NumberOfPorts:         getPortsBucket(service.Spec.Ports),
		Protocol:              getProtocol(service.Spec.Ports),
	}
	ipStack := metrics.ServiceIPStackMetricState{
		Type:                  serviceType,
		ExternalTrafficPolicy: externalTrafficPolicy,
		InternalTrafficPolicy: internalTrafficPolicy,
		IPFamilies:            getIPFamilies(service.Spec.IPFamilies),
		IPFamilyPolicy:        getIPFamilyPolicy(service.Spec.IPFamilyPolicy),
		IsStaticIPv4:          service.Spec.LoadBalancerIP != "",
		IsStaticIPv6:          false,
	}
	netTier, _ := utils.GetNetworkTier(service)
	gcpFeatures := metrics.ServiceGCPFeaturesMetricState{
		Type:         serviceType,
		NetworkTier:  string(netTier),
		GlobalAccess: gce.GetLoadBalancerAnnotationAllowGlobalAccess(service),
		CustomSubnet: gce.GetLoadBalancerAnnotationSubnet(service) != "",
	}
	c.ctx.ControllerMetrics.SetServiceMetrics(svcKey, l4Protocol, ipStack, gcpFeatures)
	return nil
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

func (c *Controller) syncDeletion(key string) {
	c.ctx.ControllerMetrics.DeleteServiceMetrics(key)
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
			return "SubsettingILB"
		}
		return "LegacyILB"
	}
	wantsL4NetLB, _ := annotations.WantsL4NetLB(service)
	if wantsL4NetLB {
		if common.HasGivenFinalizer(service.ObjectMeta, common.NetLBFinalizerV2) {
			return "RBSXLB"
		}
		return "LegacyXLB"
	}
	return ""
}

func getSessionAffinityConfig(service *v1.Service) string {
	if service.Spec.SessionAffinity != v1.ServiceAffinityClientIP {
		return ""
	}
	if service.Spec.SessionAffinityConfig == nil ||
		service.Spec.SessionAffinityConfig.ClientIP == nil ||
		service.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds == nil {
		return "10800"
	}
	timeout := *service.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds
	if timeout < 10800 {
		return "0-10799"
	}
	if timeout == 10800 {
		return "10800"
	}
	return "10800+"
}
