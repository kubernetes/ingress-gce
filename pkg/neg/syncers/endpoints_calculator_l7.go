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

package syncers

import (
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
)

// L7EndpointsCalculator implements methods to calculate Network endpoints for VM_IP_PORT NEGs
type L7EndpointsCalculator struct {
	zoneGetter          types.ZoneGetter
	servicePortName     string
	podLister           cache.Indexer
	subsetLabels        string
	networkEndpointType types.NetworkEndpointType
	logger              klog.Logger
}

func NewL7EndpointsCalculator(zoneGetter types.ZoneGetter, podLister cache.Indexer, svcPortName, subsetLabels string, endpointType types.NetworkEndpointType, logger klog.Logger) *L7EndpointsCalculator {
	return &L7EndpointsCalculator{
		zoneGetter:          zoneGetter,
		servicePortName:     svcPortName,
		podLister:           podLister,
		subsetLabels:        subsetLabels,
		networkEndpointType: endpointType,
		logger:              logger.WithName("L7EndpointsCalculator"),
	}
}

// Mode indicates the mode that the EndpointsCalculator is operating in.
func (l7calc *L7EndpointsCalculator) Mode() types.EndpointsCalculatorMode {
	return types.L7Mode
}

// CalculateEndpoints determines the endpoints in the NEGs based on the current service endpoints and the current NEGs.
func (l7calc *L7EndpointsCalculator) CalculateEndpoints(eds []types.EndpointsData, _ map[string]types.NetworkEndpointSet) (map[string]types.NetworkEndpointSet, types.EndpointPodMap, error) {
	return toZoneNetworkEndpointMap(eds, l7calc.zoneGetter, l7calc.servicePortName, l7calc.podLister, l7calc.subsetLabels, l7calc.networkEndpointType)
}
