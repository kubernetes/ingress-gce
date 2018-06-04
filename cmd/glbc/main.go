/*
Copyright 2015 The Kubernetes Authors.

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

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"

	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	neg "k8s.io/ingress-gce/pkg/neg"

	"k8s.io/ingress-gce/cmd/glbc/app"
	"k8s.io/ingress-gce/pkg/backendconfig"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/version"
)

func main() {
	flags.Register()
	flag.Parse()
	if flags.F.Verbose {
		flag.Set("v", "3")
	}

	// TODO: remove this when we do a release so the -logtostderr can be
	// used as a proper argument.
	flag.Lookup("logtostderr").Value.Set("true")

	if flags.F.Version {
		fmt.Printf("Controller version: %s\n", version.Version)
		os.Exit(0)
	}

	glog.V(0).Infof("Starting GLBC image: %q, cluster name %q", version.Version, flags.F.ClusterName)
	glog.V(0).Infof("Latest commit hash: %q", version.GitCommit)
	for i, a := range os.Args {
		glog.V(0).Infof("argv[%d]: %q", i, a)
	}

	glog.V(2).Infof("Flags = %+v", flags.F)

	kubeConfig, err := app.NewKubeConfig()
	if err != nil {
		glog.Fatalf("Failed to create kubernetes client config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to create kubernetes client: %v", err)
	}

	var backendConfigClient backendconfigclient.Interface
	if flags.F.EnableBackendConfig {
		crdClient, err := crdclient.NewForConfig(kubeConfig)
		if err != nil {
			glog.Fatalf("Failed to create kubernetes CRD client: %v", err)
		}
		// TODO(rramkumar): Reuse this CRD handler for other CRD's coming.
		crdHandler := crd.NewCRDHandler(crdClient)
		backendConfigCRDMeta := backendconfig.CRDMeta()
		if _, err := crdHandler.EnsureCRD(backendConfigCRDMeta); err != nil {
			glog.Fatalf("Failed to ensure BackendConfig CRD: %v", err)
		}

		backendConfigClient, err = backendconfigclient.NewForConfig(kubeConfig)
		if err != nil {
			glog.Fatalf("Failed to create BackendConfig client: %v", err)
		}
	}

	namer, err := app.NewNamer(kubeClient, flags.F.ClusterName, controller.DefaultFirewallName)
	if err != nil {
		glog.Fatalf("%v", err)
	}

	cloud := app.NewGCEClient()
	defaultBackendServicePortID := app.DefaultBackendServicePortID(kubeClient)
	clusterManager, err := controller.NewClusterManager(cloud, namer, defaultBackendServicePortID, flags.F.HealthCheckPath, flags.F.DefaultSvcHealthCheckPath)
	if err != nil {
		glog.Fatalf("Error creating cluster manager: %v", err)
	}

	enableNEG := cloud.AlphaFeatureGate.Enabled(gce.AlphaFeatureNetworkEndpointGroup)
	stopCh := make(chan struct{})
	ctx := context.NewControllerContext(kubeClient, backendConfigClient, flags.F.WatchNamespace, flags.F.ResyncPeriod, enableNEG, flags.F.EnableBackendConfig)
	lbc, err := controller.NewLoadBalancerController(kubeClient, stopCh, ctx, clusterManager)
	if err != nil {
		glog.Fatalf("Error creating load balancer controller: %v", err)
	}

	if clusterManager.ClusterNamer.UID() != "" {
		glog.V(0).Infof("Cluster name is %+v", clusterManager.ClusterNamer.UID())
	}
	clusterManager.Init(lbc.Translator, lbc.Translator)
	glog.V(0).Infof("clusterManager initialized")

	if enableNEG {
		negController, _ := neg.NewController(kubeClient, cloud, ctx, lbc.Translator, namer, flags.F.ResyncPeriod)
		go negController.Run(stopCh)
		glog.V(0).Infof("negController started")
	}

	go app.RunHTTPServer(lbc)
	go app.RunSIGTERMHandler(lbc, flags.F.DeleteAllOnQuit)

	ctx.Start(stopCh)
	lbc.Run()

	for {
		glog.Infof("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
	}
}
