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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/klog/v2"

	// Register the GCP authorization provider.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"

	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/ratelimit"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/version"
)

const (
	// Sleep interval to retry cloud client creation.
	cloudClientRetryInterval = 10 * time.Second
)

// NewKubeConfigForProtobuf returns a Kubernetes client config that uses protobufs
// for given the command line settings.
func NewKubeConfigForProtobuf(logger klog.Logger) (*rest.Config, error) {
	config, err := NewKubeConfig(logger)
	if err != nil {
		return nil, err
	}
	config = AddIngressUserAgent(config)
	// Use protobufs for communication with apiserver
	config.ContentType = "application/vnd.kubernetes.protobuf"
	return config, nil
}

// NewKubeConfig returns a Kubernetes client config given the command line settings.
func NewKubeConfig(logger klog.Logger) (*rest.Config, error) {
	if flags.F.InCluster {
		logger.V(0).Info("Using in cluster configuration")
		return rest.InClusterConfig()
	}

	logger.V(0).Info("Got APIServerHost and KubeConfig", "APIServerHost", flags.F.APIServerHost, "KubeConfig", flags.F.KubeConfigFile)
	config, err := clientcmd.BuildConfigFromFlags(flags.F.APIServerHost, flags.F.KubeConfigFile)
	if err != nil {
		return nil, err
	}
	return AddIngressUserAgent(config), nil
}

// NewGCEClient returns a client to the GCE environment. This will block until
// a valid configuration file can be read.
func NewGCEClient(logger klog.Logger) *gce.Cloud {
	var configReader func() io.Reader
	if flags.F.ConfigFilePath != "" {
		logger.Info("Reading config from the specified path", "path", flags.F.ConfigFilePath)
		config, err := os.Open(flags.F.ConfigFilePath)
		if err != nil {
			klog.Fatalf("%v", err)
		}
		defer config.Close()

		allConfig, err := io.ReadAll(config)
		if err != nil {
			klog.Fatalf("Error while reading config (%q): %v", flags.F.ConfigFilePath, err)
		}
		logger.V(4).Info("Cloudprovider config file", "config", string(allConfig))

		configReader = generateConfigReaderFunc(allConfig)
	} else {
		logger.V(2).Info("No cloudprovider config file provided, using default values")
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
			rl, err := ratelimit.NewGCERateLimiter(flags.F.GCERateLimit.Values(), flags.F.GCEOperationPollInterval, logger)
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
			logger.Info("Failed to list backend services, retrying", "err", err)
		} else {
			logger.Info("Failed to get cloud provider, retrying", "err", err)
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

// IngressUserAgent returns l7controller/$VERSION ($GOOS/$GOARCH)
func IngressUserAgent() string {
	return fmt.Sprintf("%s/%s (%s/%s)", filepath.Base(os.Args[0]), version.Version, runtime.GOOS, runtime.GOARCH)
}

// AddIngressUserAgent returns an updated config with IngressUserAgent()
func AddIngressUserAgent(config *rest.Config) *rest.Config {
	config.UserAgent = IngressUserAgent()
	return config
}
