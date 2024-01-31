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
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/utils/descutils"
	"k8s.io/klog/v2"
)

type ClusterInfo struct {
	Name     string
	Location string
	Regional bool
}

type ServiceInfo types.NamespacedName

type HealthcheckConfig string

const (
	DefaultHC        HealthcheckConfig = "Default"
	ReadinessProbeHC HealthcheckConfig = "ReadinessProbe"
	BackendConfigHC  HealthcheckConfig = "BackendConfig"
	TransparentHC    HealthcheckConfig = "TransparentHC"
)

type HealthcheckInfo struct {
	ClusterInfo
	ServiceInfo
	HealthcheckConfig
}

type HealthcheckDesc struct {
	K8sCluster  string            `json:"k8sCluster,omitempty"`
	K8sResource string            `json:"k8sResource,omitempty"`
	Config      HealthcheckConfig `json:"config,omitempty"`
}

func (i *ClusterInfo) generateClusterDescription() string {
	// locType here differs from locType in descutils.GenerateClusterLink().
	locType := "locations"
	return fmt.Sprintf("/%s/%s/clusters/%s", locType, i.Location, i.Name)
}

func NewServiceInfo(namespace, resourceName string) ServiceInfo {
	return ServiceInfo{namespace, resourceName}
}

func (i *ServiceInfo) generateK8sResourceDescription() string {
	return descutils.GenerateK8sResourceLink(i.Namespace, "services", i.Name)
}

func (i *HealthcheckInfo) GenerateHealthcheckDescription(spLogger klog.Logger) string {
	desc := HealthcheckDesc{}
	desc.K8sCluster = i.ClusterInfo.generateClusterDescription()
	desc.K8sResource = i.ServiceInfo.generateK8sResourceDescription()
	desc.Config = i.HealthcheckConfig
	json, err := json.MarshalIndent(desc, "", "    ")
	if err != nil {
		spLogger.Error(err, "Failed to marshall HealthcheckDesc", "desc", fmt.Sprintf("%+v", desc))
	}
	return string(json)
}
