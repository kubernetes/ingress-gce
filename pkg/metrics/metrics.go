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

package metrics

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	pscmetrics "k8s.io/ingress-gce/pkg/psc/metrics"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/version"
	"k8s.io/klog/v2"
)

const (
	label = "feature"
	// Env variable for ingress version
	versionVar = "INGRESS_VERSION"
	// Dummy float so we can used bool based timeseries
	versionValue                 = 1.0
	persistentErrorThresholdTime = 20 * time.Minute
)

var (
	ingressCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_ingresses",
			Help: "Number of Ingresses",
		},
		[]string{label},
	)
	servicePortCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_service_ports",
			Help: "Number of Service Ports",
		},
		[]string{label},
	)
	serviceAttachmentCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_service_attachments",
			Help: "Number of Service Attachments",
		},
		[]string{label},
	)
	serviceCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_services",
			Help: "Number of Services",
		},
		[]string{label},
	)
	componentVersion = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "component_version",
			Help: "Metric for exposing the component version",
		},
		[]string{"component_version"},
	)
	MetricExportFailureCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "metric_export_failure",
			Help: "Number of metric export failures",
		},
		[]string{"metric_subset"},
	)
)

// init registers ingress usage metrics.
func init() {
	klog.V(3).Infof("Registering Ingress usage metrics %v and %v", ingressCount, servicePortCount)
	prometheus.MustRegister(ingressCount, servicePortCount)

	klog.V(3).Infof("Registering PSC usage metrics %v", serviceAttachmentCount)
	prometheus.MustRegister(serviceAttachmentCount)
	prometheus.MustRegister(serviceCount)

	klog.V(3).Infof("Registering metric export failures count %v", MetricExportFailureCount)
	prometheus.MustRegister(MetricExportFailureCount)

	klog.V(3).Infof("Registering Component Version metrics %v", componentVersion)
	prometheus.MustRegister(componentVersion)
	// Component version only needs to be recorded once
	recordComponentVersion()
}

// NewIngressState returns ingress state for given ingress and service ports.
func NewIngressState(ing *v1.Ingress, fc *frontendconfigv1beta1.FrontendConfig, svcPorts []utils.ServicePort) IngressState {
	return IngressState{ingress: ing, frontendconfig: fc, servicePorts: svcPorts}
}

// ControllerMetrics contains the state of the all ingresses.
type ControllerMetrics struct {
	// ingressMap is a map between ingress key to ingress state
	ingressMap map[string]IngressState
	// pscMap is a map between the service attachment key and PSC state
	pscMap map[string]pscmetrics.PSCState
	// ServiceMap track the number of services in this cluster
	serviceMap map[string]struct{}
	//TODO(kl52752) remove mutex and change map to sync.map
	sync.Mutex
	// duration between metrics exports
	metricsInterval time.Duration

	logger klog.Logger
}

// NewControllerMetrics initializes ControllerMetrics and starts a go routine to compute and export metrics periodically.
func NewControllerMetrics(exportInterval time.Duration, logger klog.Logger) *ControllerMetrics {
	return &ControllerMetrics{
		ingressMap:      make(map[string]IngressState),
		pscMap:          make(map[string]pscmetrics.PSCState),
		serviceMap:      make(map[string]struct{}),
		metricsInterval: exportInterval,
		logger:          logger.WithName("ControllerMetrics"),
	}
}

// FakeControllerMetrics creates new ControllerMetrics with fixed 10 minutes metricsInterval, to be used in tests
func FakeControllerMetrics() *ControllerMetrics {
	return NewControllerMetrics(10*time.Minute, klog.TODO())
}

// servicePortKey defines a service port uniquely.
// Note that same service port combination used by ILB and XLB are treated as separate service ports.
type servicePortKey struct {
	svcPortID      utils.ServicePortID
	isL7ILBEnabled bool
}

func newServicePortKey(svcPort utils.ServicePort) servicePortKey {
	return servicePortKey{svcPortID: svcPort.ID, isL7ILBEnabled: svcPort.L7ILBEnabled}
}

func (spk servicePortKey) string() string {
	if spk.isL7ILBEnabled {
		return fmt.Sprintf("%s/%s", spk.svcPortID, "ILB")
	}
	return fmt.Sprintf("%s/%s", spk.svcPortID, "XLB")
}

func (im *ControllerMetrics) Run(stopCh <-chan struct{}) {
	im.logger.V(3).Info("Ingress Metrics initialized. Metrics will be exported periodically", "interval", im.metricsInterval)
	// Compute and export metrics periodically.
	go func() {
		// Wait for ingress states to be populated in the cache before computing metrics.
		time.Sleep(im.metricsInterval)
		wait.Until(im.export, im.metricsInterval, stopCh)
	}()
	<-stopCh
}

// SetIngress implements IngressMetricsCollector.
func (im *ControllerMetrics) SetIngress(ingKey string, ing IngressState) {
	im.Lock()
	defer im.Unlock()

	if im.ingressMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	im.ingressMap[ingKey] = ing
}

// DeleteIngress implements IngressMetricsCollector.
func (im *ControllerMetrics) DeleteIngress(ingKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.ingressMap, ingKey)
}

// SetServiceAttachment adds sa state to the map to be counted during metrics computation.
// SetServiceAttachment implements PSCMetricsCollector.
func (im *ControllerMetrics) SetServiceAttachment(saKey string, state pscmetrics.PSCState) {
	im.Lock()
	defer im.Unlock()

	if im.pscMap == nil {
		klog.Fatalf("PSC Metrics failed to initialize correctly.")
	}
	im.pscMap[saKey] = state
}

// DeleteServiceAttachment removes sa state to the map to be counted during metrics computation.
// DeleteServiceAttachment implements PSCMetricsCollector.
func (im *ControllerMetrics) DeleteServiceAttachment(saKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.pscMap, saKey)
}

// SetService adds the service to the map to be counted during metrics computation.
// SetService implements PSCMetricsCollector.
func (im *ControllerMetrics) SetService(serviceKey string) {
	im.Lock()
	defer im.Unlock()

	if im.serviceMap == nil {
		klog.Fatalf("PSC Metrics failed to initialize correctly.")
	}
	im.serviceMap[serviceKey] = struct{}{}
}

// DeleteService removes the service from the map to be counted during metrics computation.
// DeleteService implements PSCMetricsCollector.
func (im *ControllerMetrics) DeleteService(serviceKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.serviceMap, serviceKey)
}

// export computes and exports ingress usage metrics.
func (im *ControllerMetrics) export() {
	defer func() {
		if r := recover(); r != nil {
			im.logger.Error(nil, "failed to export metrics", "recoverMessage", r)
			MetricExportFailureCount.WithLabelValues("main").Inc()
		}
	}()
	ingCount, svcPortCount := im.computeIngressMetrics()

	im.logger.V(3).Info("Exporting ingress usage metrics", "ingressCount", ingCount, "servicePortCount", svcPortCount)
	for feature, count := range ingCount {
		ingressCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}

	for feature, count := range svcPortCount {
		servicePortCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}

	im.logger.V(3).Info("Ingress usage metrics exported")

	saCount := im.computePSCMetrics()
	im.logger.V(3).Info("Exporting PSC Usage Metrics", "serviceAttachmentsCount", saCount)
	for feature, count := range saCount {
		serviceAttachmentCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	im.logger.V(3).Info("Exported PSC Usage Metrics", "serviceAttachmentsCount", saCount)

	services := im.computeServiceMetrics()
	im.logger.V(3).Info("Exporting Service Metrics", "serviceCount", serviceCount)
	for feature, count := range services {
		serviceCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	im.logger.V(3).Info("Exported Service Metrics", "serviceCount", serviceCount)

}

// computeIngressMetrics traverses all ingresses and computes,
// 1. Count of GCE ingresses for each feature.
// 2. Count of service-port pairs that backs up a GCE ingress for each feature.
func (im *ControllerMetrics) computeIngressMetrics() (map[feature]int, map[feature]int) {
	ingCount, svcPortCount := initializeCounts()
	// servicePortFeatures tracks the list of service-ports and their features.
	// This is to avoid re-computing features for a service-port.
	svcPortFeatures := make(map[servicePortKey][]feature)
	im.logger.V(4).Info("Computing Ingress usage metrics from ingress state map", "ingressMap", im.ingressMap)
	im.Lock()
	defer im.Unlock()

	for ingKey, ingState := range im.ingressMap {
		// Both frontend and backend associated ingress features are tracked.
		currIngFeatures := make(map[feature]bool)
		im.logger.V(6).Info("Computing frontend based features for ingress", "ingressKey", ingKey)
		// Add frontend associated ingress features.
		for _, feature := range featuresForIngress(ingState.ingress, ingState.frontendconfig, im.logger) {
			currIngFeatures[feature] = true
		}
		im.logger.V(6).Info("Frontend based features for ingress", "ingressKey", ingKey, "frontendIngressFeatures", currIngFeatures)
		im.logger.V(6).Info("Computing backend based features for ingress", "ingressKey", ingKey)
		for _, svcPort := range ingState.servicePorts {
			svcPortKey := newServicePortKey(svcPort)
			im.logger.V(6).Info("Computing features for service-port", "servicePortKey", svcPortKey.string())
			svcFeatures, ok := svcPortFeatures[svcPortKey]
			if !ok {
				svcFeatures = featuresForServicePort(svcPort, im.logger)
			}
			// Add backend associated ingress features.
			for _, sf := range svcFeatures {
				// Ignore features specific to the service.
				if !isServiceFeature(sf) {
					currIngFeatures[sf] = true
				}
			}
			if ok {
				// Skip re-computing features for a service port.
				im.logger.V(4).Info("Features for service port exists, skipping", "servicePortKey", svcPortKey.string())
				continue
			}
			svcPortFeatures[svcPortKey] = svcFeatures
			im.logger.V(6).Info("Features for service port", "servicePortKey", svcPortKey.string(), "serviceFeatures", svcFeatures)
			// Update service port feature counts.
			updateServicePortCount(svcPortCount, svcFeatures)
		}
		im.logger.V(6).Info("Features for ingress", "ingressKey", ingKey, "ingressFeatures", currIngFeatures)
		// Merge current ingress to update ingress feature counts.
		updateIngressCount(ingCount, currIngFeatures)
	}

	im.logger.V(4).Info("Ingress usage metrics computed")
	return ingCount, svcPortCount
}

func (im *ControllerMetrics) computePSCMetrics() map[feature]int {
	im.Lock()
	defer im.Unlock()
	im.logger.V(4).Info("Compute PSC Usage metrics from psc state map", "pscStateMap", im.pscMap)

	counts := map[feature]int{
		sa:          0,
		saInSuccess: 0,
		saInError:   0,
	}

	for _, state := range im.pscMap {
		counts[sa]++
		if state.InSuccess {
			counts[saInSuccess]++
		} else {
			counts[saInError]++
		}
	}
	return counts
}

func (im *ControllerMetrics) computeServiceMetrics() map[feature]int {
	return map[feature]int{
		services: len(im.serviceMap),
	}
}

// initializeCounts initializes feature count maps for ingress and service ports.
// This is required in order to reset counts for features that do not exist now
// but existed before.
func initializeCounts() (map[feature]int, map[feature]int) {
	return map[feature]int{
			ingress:                   0,
			externalIngress:           0,
			internalIngress:           0,
			regionalExternalIngress:   0,
			httpEnabled:               0,
			hostBasedRouting:          0,
			pathBasedRouting:          0,
			tlsTermination:            0,
			secretBasedCertsForTLS:    0,
			preSharedCertsForTLS:      0,
			managedCertsForTLS:        0,
			staticGlobalIP:            0,
			managedStaticGlobalIP:     0,
			specifiedStaticGlobalIP:   0,
			neg:                       0,
			cloudCDN:                  0,
			cloudArmor:                0,
			cloudIAP:                  0,
			backendTimeout:            0,
			backendConnectionDraining: 0,
			clientIPAffinity:          0,
			cookieAffinity:            0,
			customRequestHeaders:      0,
			sslPolicy:                 0,
		},
		// service port counts
		map[feature]int{
			servicePort:                 0,
			externalServicePort:         0,
			internalServicePort:         0,
			regionalExternalServicePort: 0,
			neg:                         0,
			cloudCDN:                    0,
			cloudArmor:                  0,
			cloudArmorSet:               0,
			cloudArmorEmpty:             0,
			cloudArmorNil:               0,
			cloudIAP:                    0,
			backendTimeout:              0,
			backendConnectionDraining:   0,
			clientIPAffinity:            0,
			cookieAffinity:              0,
			customRequestHeaders:        0,
			transparentHealthChecks:     0,
		}
}

// updateServicePortCount inserts/increments service port counts by 1 for given features.
func updateServicePortCount(svcPortCount map[feature]int, features []feature) {
	for _, feature := range features {
		svcPortCount[feature] += 1
	}
}

// updateIngressCount inserts/increments ingress counts by 1 for given feature map.
func updateIngressCount(ingCount map[feature]int, features map[feature]bool) {
	for feature := range features {
		ingCount[feature] += 1
	}
}

// isServiceFeature returns true if given feature applies only to service-port but
// not to the ingress that references this service-port.
func isServiceFeature(ftr feature) bool {
	serviceFeatures := map[feature]bool{
		servicePort:                 true,
		externalServicePort:         true,
		internalServicePort:         true,
		regionalExternalServicePort: true,
		transparentHealthChecks:     true,
		cloudArmorEmpty:             true,
		cloudArmorNil:               true,
		cloudArmorSet:               true,
	}
	return serviceFeatures[ftr]
}

func recordComponentVersion() {
	var v string
	v, ok := os.LookupEnv(versionVar)
	if !ok {
		klog.V(2).Infof("%q env variable does not exist", versionVar)
		v = version.Version
	}
	componentVersion.WithLabelValues(v).Set(versionValue)
}
