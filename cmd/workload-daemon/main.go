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
	"flag"
	"fmt"
	"os"
	"time"

	daemon "k8s.io/ingress-gce/pkg/experimental/workload/daemon"
	gce "k8s.io/ingress-gce/pkg/experimental/workload/daemon/provider/gce"
	"k8s.io/klog/v2"

	// GCP Authentication
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	cmdSet := flag.NewFlagSet("cmd", flag.ExitOnError)
	provider := cmdSet.String("provider", "gce", "The provider of this external workload.")
	updateInterval := cmdSet.Duration("interval", 30*time.Second, "The resync interval.")

	if len(os.Args) < 2 {
		outputHelp()
		return
	}
	err := cmdSet.Parse(os.Args[2:])
	if err != nil {
		klog.Errorf("cmdSet.Parse(%v) returned error: %v", os.Args[2:], err)
	}
	if *provider != "gce" {
		klog.Fatalf("Current implementation only supports gce provider.")
	}

	switch os.Args[1] {
	case "get-credentials":
		vm, err := gce.NewVM()
		if err != nil {
			klog.Fatalf("unable to initialize GCE VM: %+v", err)
		}
		credentials, err := vm.Credentials()
		if err != nil {
			klog.Fatalf("unable to get credentials: %+v", err)
		}
		daemon.OutputCredentials(credentials)
		return
	case "start":
		klog.V(0).Infof("Workload daemon started")
		vm, err := gce.NewVM()
		if err != nil {
			klog.Fatalf("unable to initialize GCE VM: %+v", err)
		}
		daemon.RunDaemon(vm, vm, *updateInterval)
		return
	default:
		outputHelp()
		return
	}
}

func outputHelp() {
	fmt.Printf("Usage: %v [command]\n", os.Args[0])
	fmt.Printf("command:\n    start\n    get-credentials\n")
}
