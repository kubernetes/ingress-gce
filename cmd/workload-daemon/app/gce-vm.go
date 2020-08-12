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

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	gkev1 "google.golang.org/api/container/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/transport"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog"
)

// CloudVM represents a VM instance running on Google Cloud.
// It uses the metadata server to fewtch all required information.
type CloudVM struct {
	instanceName string
	hostname     string
	internalIP   string
	externalIP   string
	projectID    string
	clusterName  string
	clusterZone  string
	vmLabels     map[string]string
	ksaName      string
	ksaToken     string
	zone         string
}

// Name is the name of the workload
func (vm *CloudVM) Name() string {
	return vm.instanceName
}

// Hostname is the hostname or DNS address of the workload
func (vm *CloudVM) Hostname() string {
	return vm.hostname
}

// IP is the IP used to access this workload from the cluster
func (vm *CloudVM) IP() string {
	return vm.internalIP
}

// Labels are one or more labels associated with the workload
func (vm *CloudVM) Labels() map[string]string {
	return vm.vmLabels
}

// Locality associated with the endpoint. A locality corresponds to a failure domain.
func (vm *CloudVM) Locality() string {
	return vm.zone
}

// Credentials contain the credentials used for the deamon to access the cluster
func (vm *CloudVM) Credentials() ClusterCredentials {
	jsonData, err := metadata.Get("instance/service-accounts/default/token")
	if err != nil {
		klog.Fatalf("failed to get service account access token: %+v", err)
	}

	token := struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type,omitempty"`
	}{}
	err = json.Unmarshal([]byte(jsonData), &token)
	if err != nil {
		klog.Fatalf("malformed service account access token: %+v", err)
	}

	expiryTime := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return ClusterCredentials{
		AccessToken: token.AccessToken,
		TokenExpiry: expiryTime.UTC().Format(time.RFC3339),
	}
}

// getCluster returns the cluster info
func (vm *CloudVM) getCluster() (cluster *gkev1.Cluster) {
	var err error
	if vm.clusterName == "" {
		vm.clusterName, err = metadata.InstanceAttributeValue("k8s-cluster-name")
		if err != nil {
			klog.Fatalf("failed to get k8s-cluster-name from metadata server", err)
		}
	}
	if vm.clusterZone == "" {
		vm.clusterZone, err = metadata.InstanceAttributeValue("k8s-cluster-zone")
		if err != nil {
			klog.Fatalf("failed to get k8s-cluster-zone from metadata server", err)
		}
	}

	// Use GCE container APIs to get IP and CA
	// Should be available for "Kubernetes Engine Cluster Viewer" role
	oauthClient, _, err := transport.NewHTTPClient(context.TODO(),
		option.WithScopes(gkev1.CloudPlatformScope))
	if err != nil {
		klog.Fatalf("failed to initalize http client: %+v", err)
	}
	gkeSvc, err := gkev1.New(oauthClient)
	if err != nil {
		klog.Fatalf("failed to initialize gke client: %+v", err)
	}
	clusterSvc := gkev1.NewProjectsZonesClustersService(gkeSvc)
	cluster, err = clusterSvc.Get(vm.projectID, vm.clusterZone, vm.clusterName).Do()
	if err != nil {
		klog.Fatalf("failed to get gke cluster: %+v", err)
	}
	return
}

// KubeConfig yields the config used to create Kubernetes clientset
func (vm *CloudVM) KubeConfig() (config *rest.Config) {
	// CASE1: If there is a kubeConfig file, use that file. E.g. testing on a cloudtop.
	home := homedir.HomeDir()
	if home != "" {
		configFile := filepath.Join(home, ".kube", "config")
		if _, err := os.Stat(configFile); !os.IsNotExist(err) {
			config, err = clientcmd.BuildConfigFromFlags("", configFile)
			if err != nil {
				klog.Fatalf("unable to build config from kubeConfig file: %+v", err)
			}
			return
		}
	}

	// Get contianer master address and CA
	cluster := vm.getCluster()

	var kubeConfig []byte
	if vm.ksaName != "" && vm.ksaToken != "" {
		// CASE2: If there is a KSA specified as metadata, use it
		kubeConfig = GenKubeConfigForKSA(cluster.MasterAuth.ClusterCaCertificate, cluster.Endpoint,
			cluster.Name, vm.ksaName, vm.ksaToken)
	} else {
		// CASE3: Use gcloud SA to authenticate as a Kubernetes user
		kubeConfig = GenKubeConfigForUser(cluster.MasterAuth.ClusterCaCertificate, cluster.Endpoint, cluster.Name, "gcp")
	}

	config, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfig))
	if err != nil {
		klog.Fatalf("failed to create kubeconfig: %+v", err)
	}

	return
}

// NewCloudVM fetches all data needed from the metadata server to create CloudVM
func NewCloudVM() *CloudVM {
	var err error
	vm := CloudVM{}

	// Fetch basic info that every machine on gcloud has
	vm.instanceName, err = metadata.InstanceName()
	if err != nil {
		klog.Fatalf("failed to get InstanceName from metadata server: %+v", err)
	}
	vm.hostname, err = metadata.Hostname()
	if err != nil {
		klog.Fatalf("failed to get Hostname from metadata server: %+v", err)
	}
	vm.internalIP, err = metadata.InternalIP()
	if err != nil {
		klog.Fatalf("failed to get InternalIP from metadata server: %+v", err)
	}
	vm.externalIP, err = metadata.ExternalIP()
	if err != nil {
		klog.Fatalf("failed to get ExternalIP from metadata server: %+v", err)
	}
	vm.projectID, err = metadata.ProjectID()
	if err != nil {
		klog.Fatalf("failed to get ProjectID from metadata server: %+v", err)
	}
	vm.zone, err = metadata.Zone()
	if err != nil {
		klog.Fatalf("failed to get Zone from metadata server: %+v", err)
	}

	// Fetch the cluster name and zone
	// Not specified if the user want to use ~/.kube/config file
	vm.clusterName, err = metadata.InstanceAttributeValue("k8s-cluster-name")
	if err != nil {
		vm.clusterName = ""
	}
	vm.clusterZone, err = metadata.InstanceAttributeValue("k8s-cluster-zone")
	if err != nil {
		vm.clusterZone = ""
	}

	// Fetch the KSA name and token if existing
	vm.ksaName, err = metadata.InstanceAttributeValue("k8s-sa-name")
	if err != nil {
		vm.ksaName = ""
	}
	vm.ksaToken, err = metadata.InstanceAttributeValue("k8s-sa-token")
	if err != nil {
		vm.ksaToken = ""
	}

	// Fetch labels to use in the workload resource
	vm.vmLabels = make(map[string]string)
	attrs, err := metadata.InstanceAttributes()
	if err != nil {
		klog.Fatalf("failed to get attribute list from metadata server: %+v", err)
	}
	for _, name := range attrs {
		if strings.HasPrefix(name, "k8s-label-") {
			val, err := metadata.InstanceAttributeValue(name)
			if err != nil {
				klog.V(0).Infof("Faild to fetch label %s: %+v", name, err)
			}
			vm.vmLabels[name[10:]] = val
		}
	}

	return &vm
}
