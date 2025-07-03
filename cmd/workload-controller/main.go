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

package main

import (
	"fmt"
	"os"
	"time"

	flag "github.com/spf13/pflag"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/ingress-gce/cmd/workload-controller/app"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/experimental/workload"
	workloadclient "k8s.io/ingress-gce/pkg/experimental/workload/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	"k8s.io/ingress-gce/pkg/version"
	"k8s.io/klog/v2"
)

func main() {
	flags.Register()
	flag.Parse()

	if flags.F.Version {
		fmt.Printf("Controller version: %s\n", version.Version)
		os.Exit(0)
	}

	rootLogger := klog.TODO()
	rootLogger.V(0).Info("Starting GLBC image", "version", version.Version, "clusterName", flags.F.ClusterName)
	rootLogger.V(0).Info(fmt.Sprintf("Latest commit hash: %q", version.GitCommit))
	for i, a := range os.Args {
		rootLogger.V(0).Info(fmt.Sprintf("argv[%d]: %q", i, a))
	}

	rootLogger.V(2).Info(fmt.Sprintf("Flags = %+v", flags.F))
	defer klog.Flush()
	// Create kube-config that uses protobufs to communicate with API server.
	kubeConfigForProtobuf, err := app.NewKubeConfigForProtobuf()
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client config for protobuf: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfigForProtobuf)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client: %v", err)
	}

	// Create kube-config for CRDs.
	// TODO(smatti): Migrate to use protobuf once CRD supports.
	kubeConfig, err := app.NewKubeConfig()
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client config: %v", err)
	}
	crdClient, err := crdclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes CRD client: %v", err)
	}
	crdHandler := crd.NewCRDHandler(crdClient, rootLogger)
	workloadCRDMeta := workload.CRDMeta()
	if _, err := crdHandler.EnsureCRD(workloadCRDMeta, true); err != nil {
		klog.Fatalf("Failed to ensure Workload CRD: %v", err)
	}
	workloadClient, err := workloadclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create Workload client: %v", err)
	}

	ctx := workload.NewControllerContext(kubeClient, workloadClient, flags.F.WatchNamespace, flags.F.ResyncPeriod)
	// TODO: Leader Elect and Health Check?

	runController(ctx, rootLogger)
}

func runController(ctx *workload.ControllerContext, logger klog.Logger) {
	stopCh := make(chan struct{})
	controller := workload.NewController(ctx, logger)
	ctx.Start(stopCh)
	logger.V(0).Info("Workload controller started")
	controller.Run(stopCh)

	for {
		logger.Info("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
	}
}
