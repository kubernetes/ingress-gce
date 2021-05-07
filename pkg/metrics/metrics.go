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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	pscmetrics "k8s.io/ingress-gce/pkg/psc/metrics"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	label         = "feature"
	statusSuccess = "success"
	statusError   = "error"
)

var (
	metricsInterval = 10 * time.Minute
	ingressCount    = prometheus.NewGaugeVec(
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
	networkEndpointGroupCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_negs",
			Help: "Number of NEGs",
		},
		[]string{label},
	)
	l4ILBCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_l4_ilbs",
			Help: "Number of L4 ILBs",
		},
		[]string{label},
	)
	l4ILBSyncLatencyMetricsLabels = []string{
		"sync_result", // result of the sync
		"sync_type",   // whether this is a new service or an update
	}
	l4ILBSyncLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "l4_ilb_sync_duration_seconds",
			Help: "Latency of an L4 ILB Sync",
			// custom buckets - [30s, 60s, 120s, 240s(4min), 480s(8min), 960s(16m), +Inf]
			Buckets: prometheus.ExponentialBuckets(30, 2, 6),
		},
		l4ILBSyncLatencyMetricsLabels,
	)
	serviceAttachmentCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "number_of_service_attachments",
			Help: "Number of Service Attachments",
		},
		[]string{label},
	)
)

// init registers ingress usage metrics.
func init() {
	klog.V(3).Infof("Registering Ingress usage metrics %v and %v, NEG usage metrics %v", ingressCount, servicePortCount, networkEndpointGroupCount)
	prometheus.MustRegister(ingressCount, servicePortCount, networkEndpointGroupCount)

	klog.V(3).Infof("Registering L4 ILB usage metrics %v", l4ILBCount)
	prometheus.MustRegister(l4ILBCount, l4ILBSyncLatency)

	klog.V(3).Infof("Registering PSC usage metrics %v", serviceAttachmentCount)
	prometheus.MustRegister(serviceAttachmentCount)
}

// NewIngressState returns ingress state for given ingress and service ports.
func NewIngressState(ing *v1.Ingress, fc *frontendconfigv1beta1.FrontendConfig, svcPorts []utils.ServicePort) IngressState {
	return IngressState{ingress: ing, frontendconfig: fc, servicePorts: svcPorts}
}

// PublishL4ILBSyncLatency exports the given sync latency datapoint.
func PublishL4ILBSyncLatency(success bool, syncType string, startTime time.Time) {
	status := statusSuccess
	if !success {
		status = statusError
	}
	l4ILBSyncLatency.WithLabelValues(status, syncType).Observe(time.Since(startTime).Seconds())
}

// ControllerMetrics contains the state of the all ingresses.
type ControllerMetrics struct {
	// ingressMap is a map between ingress key to ingress state
	ingressMap map[string]IngressState
	// negMap is a map between service key to neg state
	negMap map[string]NegServiceState
	// l4ILBServiceMap is a map between service key and L4 ILB service state.
	l4ILBServiceMap map[string]L4ILBServiceState
	pscMap          map[string]pscmetrics.PSCState
	sync.Mutex
}

// NewControllerMetrics initializes ControllerMetrics and starts a go routine to compute and export metrics periodically.
func NewControllerMetrics() *ControllerMetrics {
	return &ControllerMetrics{
		ingressMap:      make(map[string]IngressState),
		negMap:          make(map[string]NegServiceState),
		l4ILBServiceMap: make(map[string]L4ILBServiceState),
		pscMap:          make(map[string]pscmetrics.PSCState),
	}
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
	klog.V(3).Infof("Ingress Metrics initialized. Metrics will be exported at an interval of %v", metricsInterval)
	// Compute and export metrics periodically.
	go func() {
		// Wait for ingress states to be populated in the cache before computing metrics.
		time.Sleep(metricsInterval)
		wait.Until(im.export, metricsInterval, stopCh)
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

// SetNegService implements NegMetricsCollector.
func (im *ControllerMetrics) SetNegService(svcKey string, negState NegServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.negMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	im.negMap[svcKey] = negState
}

// DeleteNegService implements NegMetricsCollector.
func (im *ControllerMetrics) DeleteNegService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.negMap, svcKey)
}

// SetL4ILBService implements L4ILBMetricsCollector.
func (im *ControllerMetrics) SetL4ILBService(svcKey string, state L4ILBServiceState) {
	im.Lock()
	defer im.Unlock()

	if im.l4ILBServiceMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	im.l4ILBServiceMap[svcKey] = state
}

// DeleteL4ILBService implements L4ILBMetricsCollector.
func (im *ControllerMetrics) DeleteL4ILBService(svcKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.l4ILBServiceMap, svcKey)
}

// SetServiceAttachment adds sa state to the map to be counted during metrics computation.
// SetServiceAttachment implments PSCMetricsCollector.
func (im *ControllerMetrics) SetServiceAttachment(saKey string, state pscmetrics.PSCState) {
	im.Lock()
	defer im.Unlock()

	if im.pscMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	im.pscMap[saKey] = state
}

// DeleteServiceAttachment removes sa state to the map to be counted during metrics computation.
// DeleteServiceAttachment implments PSCMetricsCollector.
func (im *ControllerMetrics) DeleteServiceAttachment(saKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.pscMap, saKey)
}

// export computes and exports ingress usage metrics.
func (im *ControllerMetrics) export() {
	ingCount, svcPortCount := im.computeIngressMetrics()
	negCount := im.computeNegMetrics()

	klog.V(3).Infof("Exporting ingress usage metrics. Ingress Count: %#v, Service Port count: %#v, NEG count: %#v", ingCount, svcPortCount, negCount)
	for feature, count := range ingCount {
		ingressCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}

	for feature, count := range svcPortCount {
		servicePortCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}

	for feature, count := range negCount {
		networkEndpointGroupCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	klog.V(3).Infof("Ingress usage metrics exported.")

	ilbCount := im.computeL4ILBMetrics()
	klog.V(3).Infof("Exporting L4 ILB usage metrics: %#v", ilbCount)
	for feature, count := range ilbCount {
		l4ILBCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	klog.V(3).Infof("L4 ILB usage metrics exported.")

	saCount := im.computePSCMetrics()
	klog.V(3).Infof("Exporting PSC Usage Metrics: %#v", saCount)
	for feature, count := range saCount {
		serviceAttachmentCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	klog.V(3).Infof("Exporting PSC Usage Metrics: %#v", saCount)

}

// computeIngressMetrics traverses all ingresses and computes,
// 1. Count of GCE ingresses for each feature.
// 2. Count of service-port pairs that backs up a GCE ingress for each feature.
func (im *ControllerMetrics) computeIngressMetrics() (map[feature]int, map[feature]int) {
	ingCount, svcPortCount := initializeCounts()
	// servicePortFeatures tracks the list of service-ports and their features.
	// This is to avoid re-computing features for a service-port.
	svcPortFeatures := make(map[servicePortKey][]feature)
	klog.V(4).Infof("Computing Ingress usage metrics from ingress state map: %#v", im.ingressMap)
	im.Lock()
	defer im.Unlock()

	for ingKey, ingState := range im.ingressMap {
		// Both frontend and backend associated ingress features are tracked.
		currIngFeatures := make(map[feature]bool)
		klog.V(6).Infof("Computing frontend based features for ingress %s", ingKey)
		// Add frontend associated ingress features.
		for _, feature := range featuresForIngress(ingState.ingress, ingState.frontendconfig) {
			currIngFeatures[feature] = true
		}
		klog.V(6).Infof("Frontend based features for ingress %s: %v", ingKey, currIngFeatures)
		klog.V(6).Infof("Computing backend based features for ingress %s", ingKey)
		for _, svcPort := range ingState.servicePorts {
			svcPortKey := newServicePortKey(svcPort)
			klog.V(6).Infof("Computing features for service-port %s", svcPortKey.string())
			svcFeatures, ok := svcPortFeatures[svcPortKey]
			if !ok {
				svcFeatures = featuresForServicePort(svcPort)
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
				klog.V(4).Infof("Features for service port %s exists, skipping.", svcPortKey.string())
				continue
			}
			svcPortFeatures[svcPortKey] = svcFeatures
			klog.V(6).Infof("Features for service port %s: %v", svcPortKey.string(), svcFeatures)
			// Update service port feature counts.
			updateServicePortCount(svcPortCount, svcFeatures)
		}
		klog.V(6).Infof("Features for ingress %s: %v", ingKey, currIngFeatures)
		// Merge current ingress to update ingress feature counts.
		updateIngressCount(ingCount, currIngFeatures)
	}

	klog.V(4).Infof("Ingress usage metrics computed.")
	return ingCount, svcPortCount
}

// computeNegMetrics aggregates NEG metrics in the cache
func (im *ControllerMetrics) computeNegMetrics() map[feature]int {
	im.Lock()
	defer im.Unlock()
	klog.V(4).Infof("Computing NEG usage metrics from neg state map: %#v", im.negMap)

	counts := map[feature]int{
		standaloneNeg:  0,
		ingressNeg:     0,
		asmNeg:         0,
		neg:            0,
		vmIpNeg:        0,
		vmIpNegLocal:   0,
		vmIpNegCluster: 0,
		customNamedNeg: 0,
		negInSuccess:   0,
		negInError:     0,
	}

	for key, negState := range im.negMap {
		klog.V(6).Infof("For service %s, it has standaloneNegs:%d, ingressNegs:%d, asmNeg:%d and vmPrimaryNeg:%v",
			key, negState.StandaloneNeg, negState.IngressNeg, negState.AsmNeg, negState.VmIpNeg)
		counts[standaloneNeg] += negState.StandaloneNeg
		counts[ingressNeg] += negState.IngressNeg
		counts[asmNeg] += negState.AsmNeg
		counts[neg] += negState.AsmNeg + negState.StandaloneNeg + negState.IngressNeg
		counts[customNamedNeg] += negState.CustomNamedNeg
		counts[negInSuccess] += negState.SuccessfulNeg
		counts[negInError] += negState.ErrorNeg
		if negState.VmIpNeg != nil {
			counts[neg] += 1
			counts[vmIpNeg] += 1
			if negState.VmIpNeg.trafficPolicyLocal {
				counts[vmIpNegLocal] += 1
			} else {
				counts[vmIpNegCluster] += 1
			}
		}
	}
	klog.V(4).Info("NEG usage metrics computed.")
	return counts
}

// computeL4ILBMetrics aggregates L4 ILB metrics in the cache.
func (im *ControllerMetrics) computeL4ILBMetrics() map[feature]int {
	im.Lock()
	defer im.Unlock()
	klog.V(4).Infof("Computing L4 ILB usage metrics from service state map: %#v", im.l4ILBServiceMap)
	counts := map[feature]int{
		l4ILBService:      0,
		l4ILBGlobalAccess: 0,
		l4ILBCustomSubnet: 0,
		l4ILBInSuccess:    0,
		l4ILBInError:      0,
	}

	for key, state := range im.l4ILBServiceMap {
		klog.V(6).Infof("ILB Service %s has EnabledGlobalAccess: %t, EnabledCustomSubnet: %t, InSuccess: %t", key, state.EnabledGlobalAccess, state.EnabledCustomSubnet, state.InSuccess)
		counts[l4ILBService]++
		if !state.InSuccess {
			counts[l4ILBInError]++
			// Skip counting other features if the service is in error state.
			continue
		}
		counts[l4ILBInSuccess]++
		if state.EnabledGlobalAccess {
			counts[l4ILBGlobalAccess]++
		}
		if state.EnabledCustomSubnet {
			counts[l4ILBCustomSubnet]++
		}
	}
	klog.V(4).Info("L4 ILB usage metrics computed.")
	return counts
}

func (im *ControllerMetrics) computePSCMetrics() map[feature]int {
	im.Lock()
	defer im.Unlock()
	klog.V(4).Infof("Compute PSC Usage metrics from psc state map: %+v", im.pscMap)

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

// initializeCounts initializes feature count maps for ingress and service ports.
// This is required in order to reset counts for features that do not exist now
// but existed before.
func initializeCounts() (map[feature]int, map[feature]int) {
	return map[feature]int{
			ingress:                   0,
			externalIngress:           0,
			internalIngress:           0,
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
			servicePort:               0,
			externalServicePort:       0,
			internalServicePort:       0,
			neg:                       0,
			cloudCDN:                  0,
			cloudArmor:                0,
			cloudIAP:                  0,
			backendTimeout:            0,
			backendConnectionDraining: 0,
			clientIPAffinity:          0,
			cookieAffinity:            0,
			customRequestHeaders:      0,
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
		servicePort:         true,
		externalServicePort: true,
		internalServicePort: true,
	}
	return serviceFeatures[ftr]
}
