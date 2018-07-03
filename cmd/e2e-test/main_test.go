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
	"time"

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
		run              bool
		inCluster        bool
		kubeconfig       string
		project          string
		seed             int64
		destroySandboxes bool
		handleSIGINT     bool
		handleSIGTERM    bool
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
	flag.BoolVar(&flags.run, "run", false, "set to true to run tests (suppresses test suite from 'go test ./...')")
	flag.BoolVar(&flags.inCluster, "inCluster", false, "set to true if running in the cluster")
	flag.StringVar(&flags.project, "project", "", "GCP project")
	flag.Int64Var(&flags.seed, "seed", -1, "random seed")
	flag.BoolVar(&flags.destroySandboxes, "destroySandboxes", true, "set to false to leave sandboxed resources for debugging")
	flag.BoolVar(&flags.handleSIGINT, "handleSIGINT", true, "catch SIGINT to perform clean")
	flag.BoolVar(&flags.handleSIGTERM, "handleSIGTERM", true, "catch SIGTERM to perform clean")
}

// TestMain is the entrypoint for the end-to-end test suite. This is where
// global resource setup should be done.
func TestMain(m *testing.M) {
	flag.Parse()

	if !flags.inCluster && !flags.run {
		fmt.Fprintln(os.Stderr, "Set -run to run the tests.")
		// Return 0 here so 'go test ./...' will succeed.
		os.Exit(0)
	}
	if flags.project == "" {
		fmt.Fprintln(os.Stderr, "-project must be set to the Google Cloud test project")
		os.Exit(1)
	}

	fmt.Printf("Version: %q, Commit: %q\n", version.Version, version.GitCommit)

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

	if flags.seed == -1 {
		flags.seed = time.Now().UnixNano()
	}
	glog.Infof("Using random seed = %d", flags.seed)

	Framework = e2e.NewFramework(kubeconfig, e2e.Options{
		Project:          flags.project,
		Seed:             flags.seed,
		DestroySandboxes: flags.destroySandboxes,
	})
	if flags.handleSIGINT || flags.handleSIGTERM {
		Framework.CatchSignals(flags.handleSIGINT, flags.handleSIGTERM)
	}
	if err := Framework.SanityCheck(); err != nil {
		glog.Fatalf("Framework sanity check failed: %v", err)
	}

	os.Exit(m.Run())
}
