/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"

	"github.com/golang/glog"
	"k8s.io/apiserver/pkg/server"

	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/client"
	"github.com/GoogleCloudPlatform/gke-managed-certs/pkg/controller"
)

const managedCertificatesVersion = "0.0.1"

var cloudConfig = flag.String("cloud-config", "", "The path to the cloud provider configuration file.  Empty string for no configuration file.")

func main() {
	flag.Parse()

	glog.V(1).Infof("Managed certificates %s controller starting", managedCertificatesVersion)

	//To handle SIGINT gracefully
	stopChannel := server.SetupSignalHandler()

	clients, err := client.New(*cloudConfig)
	if err != nil {
		glog.Fatal(err)
	}

	controller := controller.New(clients)

	go clients.McrtInformerFactory.Start(stopChannel)

	if err = controller.Run(stopChannel); err != nil {
		glog.Fatal("Error running controller: %v", err)
	}
}
