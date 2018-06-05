/*
Copyright 2018 The Kubernetes Authors.

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
	"path/filepath"
	"testing"

	"github.com/golang/glog"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/version"
	// Pull in the auth library for GCP.
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

var (
	flags struct {
		inCluster  bool
		kubeconfig string
	}

	Framework *e2e.Framework
)

func init() {
	home := os.Getenv("HOME")
	if home != "" {
		flag.StringVar(&flags.kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&flags.kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.BoolVar(&flags.inCluster, "inCluster", false, "set to true if running in the cluster")
}

// TestMain is the entrypoint for the end-to-end test suite. This is where
// global resource setup should be done.
func TestMain(m *testing.M) {
	flag.Parse()

	if os.Getenv("RUN_INGRESS_E2E") != "true" {
		fmt.Fprintln(os.Stderr, "You must set the RUN_INGRESS_E2E environment variable to 'true' run the tests.")
		return
	}

	fmt.Printf("Version: %q\n", version.Version)
	fmt.Printf("Commit: %q\n", version.GitCommit)

	var err error
	var kubeconfig *rest.Config

	if flags.inCluster {
		kubeconfig, err = rest.InClusterConfig()
		if err != nil {
			glog.Fatalf("Error creating InClusterConfig(): %v", err)
		}
	} else {
		kubeconfig, err = clientcmd.BuildConfigFromFlags("", flags.kubeconfig)
		if err != nil {
			glog.Fatalf("Error creating kubernetes clientset from %q: %v", flags.kubeconfig, err)
		}
	}

	Framework = e2e.NewFramework(kubeconfig)
	if err := Framework.SanityCheck(); err != nil {
		glog.Fatalf("Framework sanity check failed: %v", err)
	}

	os.Exit(m.Run())
}
