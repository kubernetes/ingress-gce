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
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/cmd/workload-daemon/app"
	"k8s.io/klog"

	workloadv1a1 "k8s.io/ingress-gce/pkg/apis/workload/v1alpha1"
	workloadclient "k8s.io/ingress-gce/pkg/experimental/workload/client/clientset/versioned"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

const updateInterval time.Duration = 30 * time.Second

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %v [command] \n", os.Args[0])
		return
	}
	switch os.Args[1] {
	case "get-credentials":
		credentials := app.NewCloudVM().Credentials()
		app.OutputCredentials(credentials)
		return
	case "start":
		vm := app.NewCloudVM()
		var workload app.WorkloadInfo = vm
		var helper app.ConnectionHelper = vm

		// Generate KubeConfig and connect to it
		config := helper.KubeConfig()
		var clientSet workloadclient.Interface
		clientSet, err := workloadclient.NewForConfig(config)
		if err != nil {
			klog.Fatalf("unable to connect to the cluster: %+v", err)
		}

		// Create the workload resource
		client := clientSet.NetworkingV1alpha1().Workloads(corev1.NamespaceDefault)
		_, err = client.Create(context.TODO(), getWorkloadCR(workload), metav1.CreateOptions{})
		if err != nil {
			klog.Fatalf("unable to create the workload cr: %+v", err)
		}
		klog.V(0).Infof("CR created %s", workload.Name())

		// Update the heartbeat regularly
		ticker := time.NewTicker(updateInterval)
		quit := make(chan interface{})
		sigs := make(chan os.Signal, 1)
		go updatingCR(workload, clientSet, ticker, sigs, quit)

		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		client.Delete(context.TODO(), workload.Name(), metav1.DeleteOptions{})
		klog.V(0).Infof("CR deleted")

		return
	default:
		fmt.Printf("Usage: %v [command] \n", os.Args[0])
		return
	}
}

func updatingCR(workload app.WorkloadInfo, clientSet workloadclient.Interface, ticker *time.Ticker,
	sigs chan os.Signal, quit chan interface{}) {
	for {
		select {
		case <-ticker.C:
			patchStr := `[{"op": "replace", "path": "/status/heartbeat", "value": "%s"}]`
			patch := []byte(fmt.Sprintf(patchStr, time.Now().UTC().Format(time.RFC3339)))
			vmInstClient := clientSet.NetworkingV1alpha1().Workloads(corev1.NamespaceDefault)
			_, err := vmInstClient.Patch(context.TODO(), workload.Name(), types.JSONPatchType, patch, metav1.PatchOptions{})
			if err != nil {
				klog.V(0).Infof("CR update failed: %+v", err)
			} else {
				klog.V(0).Infof("CR updated %s", workload.Name())
			}
		case <-sigs:
			ticker.Stop()
			quit <- true
			return
		}
	}
}

func getWorkloadCR(workload app.WorkloadInfo) *workloadv1a1.Workload {
	return &workloadv1a1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:   workload.Name(),
			Labels: workload.Labels(),
		},
		Spec: workloadv1a1.WorkloadSpec{
			InstanceName: workload.Name(),
			HostName:     workload.Hostname(),
			IP:           workload.IP(),
			Locality:     workload.Locality(),
		},
		Status: workloadv1a1.WorkloadStatus{
			Heartbeat: time.Now().UTC().Format(time.RFC3339),
		},
	}
}
