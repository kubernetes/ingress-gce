/*
Copyright 2017 The Kubernetes Authors.

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
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"

	// Register the GCP authorization provider.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/ratelimit"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// Sleep interval to retry cloud client creation.
	cloudClientRetryInterval = 10 * time.Second
)

// NewKubeConfig returns a Kubernetes client config given the command line settings.
func NewKubeConfig() (*rest.Config, error) {
	if flags.F.InCluster {
		klog.V(0).Infof("Using in cluster configuration")
		return rest.InClusterConfig()
	}

	klog.V(0).Infof("Using APIServerHost=%q, KubeConfig=%q", flags.F.APIServerHost, flags.F.KubeConfigFile)
	return clientcmd.BuildConfigFromFlags(flags.F.APIServerHost, flags.F.KubeConfigFile)
}

// NewGCEClient returns a client to the GCE environment. This will block until
// a valid configuration file can be read.
func NewGCEClient() *gce.Cloud {
	var configReader func() io.Reader
	if flags.F.ConfigFilePath != "" {
		klog.Infof("Reading config from path %q", flags.F.ConfigFilePath)
		config, err := os.Open(flags.F.ConfigFilePath)
		if err != nil {
			klog.Fatalf("%v", err)
		}
		defer config.Close()

		allConfig, err := ioutil.ReadAll(config)
		if err != nil {
			klog.Fatalf("Error while reading config (%q): %v", flags.F.ConfigFilePath, err)
		}
		klog.V(4).Infof("Cloudprovider config file contains: %q", string(allConfig))

		configReader = generateConfigReaderFunc(allConfig)
	} else {
		klog.V(2).Infof("No cloudprovider config file provided, using default values.")
		configReader = func() io.Reader { return nil }
	}

	// Creating the cloud interface involves resolving the metadata server to get
	// an oauth token. If this fails, the token provider assumes it's not on GCE.
	// No errors are thrown. So we need to keep retrying till it works because
	// we know we're on GCE.
	for {
		provider, err := cloudprovider.GetCloudProvider("gce", configReader())
		if err == nil {
			cloud := provider.(*gce.Cloud)
			// Configure GCE rate limiting
			rl, err := ratelimit.NewGCERateLimiter(flags.F.GCERateLimit.Values(), flags.F.GCEOperationPollInterval)
			if err != nil {
				klog.Fatalf("Error configuring rate limiting: %v", err)
			}
			cloud.SetRateLimiter(rl)
			// If this controller is scheduled on a node without compute/rw
			// it won't be allowed to list backends. We can assume that the
			// user has no need for Ingress in this case. If they grant
			// permissions to the node they will have to restart the controller
			// manually to re-create the client.
			// TODO: why do we bail with success out if there is a permission error???
			if _, err = cloud.ListGlobalBackendServices(); err == nil || utils.IsHTTPErrorCode(err, http.StatusForbidden) {
				return cloud
			}
			klog.Warningf("Failed to list backend services, retrying: %v", err)
		} else {
			klog.Warningf("Failed to get cloud provider, retrying: %v", err)
		}
		time.Sleep(cloudClientRetryInterval)
	}
}

type readerFunc func() io.Reader

func generateConfigReaderFunc(config []byte) readerFunc {
	return func() io.Reader {
		return bytes.NewReader(config)
	}
}
