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
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	label = "Feature"
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
)

// init registers ingress usage metrics.
func init() {
	klog.V(3).Infof("Registering Ingress usage metrics %v and %v", ingressCount, servicePortCount)
	prometheus.MustRegister(ingressCount, servicePortCount)
}

// NewIngressState returns ingress state for given ingress and service ports.
func NewIngressState(ing *v1beta1.Ingress, svcPorts []utils.ServicePort) IngressState {
	return IngressState{ingress: ing, servicePorts: svcPorts}
}

// ControllerMetrics contains the state of the all ingresses.
type ControllerMetrics struct {
	ingressMap map[string]IngressState
	sync.Mutex
}

// NewControllerMetrics initializes ControllerMetrics and starts a go routine to compute and export metrics periodically.
func NewControllerMetrics() *ControllerMetrics {
	return &ControllerMetrics{ingressMap: make(map[string]IngressState)}
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

// SetIngress implements ControllerMetrics.
func (im *ControllerMetrics) SetIngress(ingKey string, ing IngressState) {
	im.Lock()
	defer im.Unlock()

	if im.ingressMap == nil {
		klog.Fatalf("Ingress Metrics failed to initialize correctly.")
	}
	im.ingressMap[ingKey] = ing
}

// DeleteIngress implements ControllerMetrics.
func (im *ControllerMetrics) DeleteIngress(ingKey string) {
	im.Lock()
	defer im.Unlock()

	delete(im.ingressMap, ingKey)
}

// export computes and exports ingress usage metrics.
func (im *ControllerMetrics) export() {
	ingCount, svcPortCount := im.computeMetrics()

	klog.V(3).Infof("Exporting ingress usage metrics. Ingress Count: %#v, Service Port count: %#v", ingCount, svcPortCount)
	for feature, count := range ingCount {
		ingressCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}

	for feature, count := range svcPortCount {
		servicePortCount.With(prometheus.Labels{label: feature.String()}).Set(float64(count))
	}
	klog.V(3).Infof("Ingress usage metrics exported.")
}

// computeMetrics traverses all ingresses and computes,
// 1. Count of GCE ingresses for each feature.
// 2. Count of service-port pairs that backs up a GCE ingress for each feature.
func (im *ControllerMetrics) computeMetrics() (map[feature]int, map[feature]int) {
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
		for _, feature := range featuresForIngress(ingState.ingress) {
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
			neg:                       0,
			cloudCDN:                  0,
			cloudArmor:                0,
			cloudIAP:                  0,
			backendTimeout:            0,
			backendConnectionDraining: 0,
			clientIPAffinity:          0,
			cookieAffinity:            0,
			customRequestHeaders:      0,
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
