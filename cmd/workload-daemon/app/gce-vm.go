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

	gkev1 "google.golang.org/api/container/v1"
	"google.golang.org/api/option"
	"google.golang.org/api/transport"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/ingress-gce/cmd/workload-daemon/metadata"
	"k8s.io/klog"
)

// CloudVM represents a VM instance running on Google Cloud.
// It uses the metadata server to fetch all required information.
type CloudVM struct {
	instanceName string
	hostname     string
	internalIP   string
	externalIP   string
	projectID    string
	zone         string

	// The following fields are used to access Kubernetes cluster and create resources.
	// They are stored as custom metadata, if any.

	// clusterName is the Kubernetes cluster name, stored as "k8s-cluster-name".
	clusterName string
	// clusterZone is the zone the Kubernetes cluster locates, stored as "k8s-cluster-zone".
	clusterZone string
	// ksaName is the Kubernetes service account (KSA) name used to access the cluster.
	// Blank in the case when gcloud IAM service account (GSA) is used.
	ksaName string
	// ksaToken is the access token of Kubernetes service account
	// Blank if not applicable.
	ksaToken string

	// vmLabels are labels to use in the workload resource
	// Metadata startwith "k8s-label-" are used to create it.
	// For example, metadata "k8s-label-foo:bar" leads to the label "foo:bar"
	vmLabels map[string]string
}

// gsaAccessToken is an OAuth2 access token fetched from metadata server
type gsaAccessToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type,omitempty"`
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

func decodeGsaAccessToken(jsonData string) (token gsaAccessToken, err error) {
	token = gsaAccessToken{}
	err = json.Unmarshal([]byte(jsonData), &token)
	if err != nil {
		klog.Errorf("malformed service account access token: %+v", err)
		return
	}
	return
}

// Credentials contain the credentials used for the daemon to access the cluster
func (vm *CloudVM) Credentials() (ClusterCredentials, error) {
	jsonData, err := metadata.Get("instance/service-accounts/default/token")
	if err != nil {
		klog.Errorf("failed to get service account access token: %+v", err)
		return ClusterCredentials{}, err
	}

	token, err := decodeGsaAccessToken(jsonData)
	if err != nil {
		klog.Errorf("failed to decode service account access token: %+v", err)
		return ClusterCredentials{}, err
	}
	expiryTime := time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	ret := ClusterCredentials{
		AccessToken: token.AccessToken,
		TokenExpiry: expiryTime.UTC().Format(time.RFC3339),
	}
	return ret, nil
}

// getCluster returns the cluster info
func (vm *CloudVM) getCluster() (cluster *gkev1.Cluster, err error) {
	// These fields should be fetched in NewCloudVM(), but the error info was ignored,
	// as they are optional fields when "~/.kube/config" is used.
	// Therefore, if they are not present, try to fetch again to return the exact error.
	if vm.clusterName == "" {
		vm.clusterName, err = metadata.InstanceAttributeValue("k8s-cluster-name")
		if err != nil {
			klog.Errorf("failed to get k8s-cluster-name from metadata server", err)
			return
		}
	}
	if vm.clusterZone == "" {
		vm.clusterZone, err = metadata.InstanceAttributeValue("k8s-cluster-zone")
		if err != nil {
			klog.Errorf("failed to get k8s-cluster-zone from metadata server", err)
			return
		}
	}

	// Use GCE container APIs to get IP and CA
	// Should be available for "Kubernetes Engine Cluster Viewer" role
	oauthClient, _, err := transport.NewHTTPClient(context.Background(),
		option.WithScopes(gkev1.CloudPlatformScope))
	if err != nil {
		klog.Errorf("failed to initalize http client: %+v", err)
		return
	}
	gkeSvc, err := gkev1.New(oauthClient)
	if err != nil {
		klog.Errorf("failed to initialize gke client: %+v", err)
		return
	}
	clusterSvc := gkev1.NewProjectsZonesClustersService(gkeSvc)
	cluster, err = clusterSvc.Get(vm.projectID, vm.clusterZone, vm.clusterName).Do()
	if err != nil {
		klog.Errorf("failed to get gke cluster: %+v", err)
		return
	}
	return
}

// KubeConfig yields the config used to create Kubernetes clientset.
// It tries the following ways in order:
// - Use ~/.kube/config file.
// - Use Kubernetes service account (KSA) specified by metadata.
// - Use gcloud IAM service account (GSA) associated with this instance.
func (vm *CloudVM) KubeConfig() (config *rest.Config, err error) {
	// CASE1: If there is a kubeConfig file, use that file. E.g. testing on a cloudtop.
	configFile := filepath.Join(homedir.HomeDir(), ".kube", "config")
	config, err = clientcmd.BuildConfigFromFlags("", configFile)
	if err == nil {
		return
	}
	if !os.IsNotExist(err) {
		klog.Errorf("unable to build config from kubeConfig file: %+v", err)
		return
	}

	// Get contianer master address and CA
	cluster, err := vm.getCluster()
	if err != nil {
		klog.Errorf("unable to get the cluster info: %+v", err)
		return
	}

	var kubeConfig []byte
	if vm.ksaName != "" && vm.ksaToken != "" {
		// CASE2: If there is a KSA specified as metadata, use it
		kubeConfig = GenKubeConfigForKSA(cluster.MasterAuth.ClusterCaCertificate, cluster.Endpoint,
			cluster.Name, vm.ksaName, vm.ksaToken)
	} else {
		// CASE3: Use gcloud SA to authenticate as a Kubernetes user
		kubeConfig = GenKubeConfigForUser(cluster.MasterAuth.ClusterCaCertificate, cluster.Endpoint, cluster.Name, "gcp")
	}

	config, err = clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfig))
	if err != nil {
		klog.Errorf("failed to create kubeconfig: %+v", err)
		return
	}

	return
}

func getAttrOrPanic(getter func() (string, error), name string) string {
	ret, err := getter()
	if err != nil {
		klog.Errorf("failed to get %s from metadata server: %+v", name, err)
		// This will be recovered by the NewCloudVM function.
		panic(err)
	}
	return ret
}

func getOptionalMetadata(attr string) string {
	ret, err := metadata.InstanceAttributeValue(attr)
	if err != nil {
		ret = ""
	}
	return ret
}

// NewCloudVM fetches all data needed from the metadata server to create CloudVM
func NewCloudVM() (vm *CloudVM, err error) {
	// Catch the error in getAttrOrPanic
	defer func() {
		e := recover()
		if e != nil {
			err = e.(error)
		}
	}()
	vm = &CloudVM{
		// Fetch basic info that every GCP Instance has
		instanceName: getAttrOrPanic(metadata.InstanceName, "InstanceName"),
		hostname:     getAttrOrPanic(metadata.Hostname, "Hostname"),
		internalIP:   getAttrOrPanic(metadata.InternalIP, "InternalIP"),
		externalIP:   getAttrOrPanic(metadata.ExternalIP, "ExternalIP"),
		projectID:    getAttrOrPanic(metadata.ProjectID, "ProjectID"),
		zone:         getAttrOrPanic(metadata.Zone, "Zone"),
		// Fetch the cluster name and zone
		// Not specified if the user want to use ~/.kube/config file
		clusterName: getOptionalMetadata("k8s-cluster-name"),
		clusterZone: getOptionalMetadata("k8s-cluster-zone"),
		// Fetch the KSA name and token if existing
		ksaName:  getOptionalMetadata("k8s-sa-name"),
		ksaToken: getOptionalMetadata("k8s-sa-token"),
		// Labels to use in the workload resource
		vmLabels: make(map[string]string),
	}

	const (
		labelPrefix = "k8s-label-"
		prefixLen   = len(labelPrefix)
	)

	// Fetch labels
	attrs, err := metadata.InstanceAttributes()
	if err != nil {
		klog.Errorf("failed to get attribute list from metadata server: %+v", err)
		return nil, err
	}
	for _, name := range attrs {
		if strings.HasPrefix(name, labelPrefix) {
			val, err := metadata.InstanceAttributeValue(name)
			if err != nil {
				klog.Errorf("faild to fetch label %s: %+v", name, err)
			}
			vm.vmLabels[name[prefixLen:]] = val
		}
	}

	return
}
