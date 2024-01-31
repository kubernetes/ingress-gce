/*
Copyright 2023 The Kubernetes Authors.

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

package healthcheck

import (
	"testing"

	"k8s.io/klog/v2"
)

func TestGenerateClusterDescription(t *testing.T) {
	testName := "cluster-name"
	testRegion := "pl-central7"
	clusterInfo := ClusterInfo{testName, testRegion, true}
	expectedDescription := "/locations/pl-central7/clusters/cluster-name"

	generatedDescription := clusterInfo.generateClusterDescription()
	if generatedDescription != expectedDescription {
		t.Errorf("unexpected cluster description: wanted %s, but got %s", expectedDescription, generatedDescription)
	}

	testZone := "pl-central7-g"
	clusterInfo = ClusterInfo{testName, testZone, false}
	expectedDescription = "/locations/pl-central7-g/clusters/cluster-name"

	generatedDescription = clusterInfo.generateClusterDescription()
	if generatedDescription != expectedDescription {
		t.Errorf("unexpected cluster description: wanted %s, but got %s", expectedDescription, generatedDescription)
	}
}

func TestGenerateK8sResourceDescription(t *testing.T) {
	testName := "service-name"
	testNamespace := "testNamespace"
	serviceInfo := NewServiceInfo(testNamespace, testName)
	expectedDescription := "/namespaces/testNamespace/services/service-name"

	generatedDescription := serviceInfo.generateK8sResourceDescription()
	if generatedDescription != expectedDescription {
		t.Errorf("unexpected service description: wanted %v, but got %v", expectedDescription, generatedDescription)
	}
}

func TestGenerateHealthcheckDescription(t *testing.T) {
	testCluster := "cluster-name"
	testRegion := "pl-central7"
	clusterInfo := ClusterInfo{testCluster, testRegion, true}
	testService := "service-name"
	testNamespace := "testNamespace"
	serviceInfo := NewServiceInfo(testNamespace, testService)
	hcConfig := DefaultHC
	i := HealthcheckInfo{
		ClusterInfo:       clusterInfo,
		ServiceInfo:       serviceInfo,
		HealthcheckConfig: hcConfig,
	}
	expectedDescription := "" +
		"{\n" +
		"    \"k8sCluster\": \"/locations/pl-central7/clusters/cluster-name\",\n" +
		"    \"k8sResource\": \"/namespaces/testNamespace/services/service-name\",\n" +
		"    \"config\": \"Default\"\n" +
		"}"

	generatedDescription := i.GenerateHealthcheckDescription(klog.TODO())
	if generatedDescription != expectedDescription {
		t.Errorf("unexpected healthcheck description: wanted %v, but got %v", expectedDescription, generatedDescription)
	}
}
