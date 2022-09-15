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

package utils

import "k8s.io/client-go/rest"

// WorkloadInfo represents basic information of this workload
type WorkloadInfo interface {
	// Name is the name of the workload
	Name() (string, bool)
	// Hostname is the hostname or DNS address of the workload
	Hostname() (string, bool)
	// IP is the IP used to access this workload from the cluster
	IP() (string, bool)
	// Labels are one or more labels associated with the workload
	Labels() map[string]string
	// Region associated with the endpoint.
	Region() (string, bool)
	// Zone associated with the endpoint.
	Zone() (string, bool)
}

// ConnectionHelper provides the identity and config used to connect to the cluster
type ConnectionHelper interface {
	// Credentials contain the credentials used for the daemon to access the cluster.
	// This is output to stdout, for Kubernetes clients to use.
	Credentials() (ClusterCredentials, error)
	// KubeConfig yields the config used to create Kubernetes clientset.
	KubeConfig() (*rest.Config, error)
}

// ClusterCredentials contains the access token to the cluster and its expiry time
type ClusterCredentials struct {
	// AccessToken is the access token to access the cluster
	AccessToken string `json:"access_token"`
	// TokenExpiry is the expiry time of the access token, in RFC3339 format
	TokenExpiry string `json:"token_expiry"`
}
