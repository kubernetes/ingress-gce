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

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	workloadv1a1 "k8s.io/ingress-gce/pkg/experimental/apis/workload/v1alpha1"
	workloadclient "k8s.io/ingress-gce/pkg/experimental/workload/client/clientset/versioned"
	daemonutils "k8s.io/ingress-gce/pkg/experimental/workload/daemon/utils"
	"k8s.io/klog"
)

// RunDaemon executes the workload daemon
func RunDaemon(
	workload daemonutils.WorkloadInfo,
	connHelper daemonutils.ConnectionHelper,
	updateInterval time.Duration,
) {
	name, nameExist := workload.Name()
	if !nameExist {
		klog.Fatalf("Workload must have a name")
	}

	// Generate KubeConfig and connect to it
	config, err := connHelper.KubeConfig()
	if err != nil {
		klog.Fatalf("unable to create KubeConfig: %+v", err)
	}
	var clientset workloadclient.Interface
	clientset, err = workloadclient.NewForConfig(config)
	if err != nil {
		klog.Fatalf("unable to connect to the cluster: %+v", err)
	}

	// Create the workload resource
	client := clientset.NetworkingV1alpha1().Workloads(corev1.NamespaceDefault)
	_, err = client.Create(context.Background(), getWorkloadCR(workload), metav1.CreateOptions{})
	if err != nil {
		klog.Fatalf("unable to create the workload resource: %+v", err)
	}
	klog.V(2).Infof("workload resource created: %s", name)

	// Update the heartbeat regularly
	ticker := time.NewTicker(updateInterval)
	quit := make(chan interface{})
	sigs := make(chan os.Signal, 1)
	go updateCR(workload, clientset, ticker, sigs, quit)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	klog.V(0).Infof("receiving quit signal, try to delete the workload resource")

	err = client.Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("unable to delete the workload resource: %+v", err)
	} else {
		klog.V(2).Infof("workload resource deleted")
	}
}

func updateCR(workload daemonutils.WorkloadInfo, clientset workloadclient.Interface, ticker *time.Ticker,
	sigs chan os.Signal, quit chan interface{}) {
	for {
		select {
		case <-ticker.C:
			patchStr := `[{"op": "replace", "path": "/status/heartbeat", "value": "%s"}]`
			patch := []byte(fmt.Sprintf(patchStr, time.Now().UTC().Format(time.RFC3339)))
			vmInstClient := clientset.NetworkingV1alpha1().Workloads(corev1.NamespaceDefault)
			_, err := vmInstClient.Patch(context.Background(), stringOrEmpty(workload.Name()), types.JSONPatchType,
				patch, metav1.PatchOptions{})
			if err != nil {
				klog.Errorf("failed to update the workload resource: %+v", err)
			} else {
				klog.V(2).Infof("workload resource updated")
			}
		case <-sigs:
			ticker.Stop()
			quit <- true
			return
		}
	}
}

func getWorkloadCR(workload daemonutils.WorkloadInfo) *workloadv1a1.Workload {
	heartbeat := time.Now().UTC().Format(time.RFC3339)
	return &workloadv1a1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:   stringOrEmpty(workload.Name()),
			Labels: workload.Labels(),
		},
		Spec: workloadv1a1.WorkloadSpec{
			InstanceName: stringOrEmpty(workload.Name()),
			HostName:     stringOrEmpty(workload.Hostname()),
			IP:           stringOrEmpty(workload.IP()),
			Locality:     stringOrEmpty(workload.Region()) + "/" + stringOrEmpty(workload.Zone()),
		},
		Status: workloadv1a1.WorkloadStatus{
			// Heartbeat: &metav1.Time{Time: time.Now()},
			Heartbeat: &heartbeat,
		},
	}
}

func stringOrEmpty(str string, exist bool) string {
	if exist {
		return str
	} else {
		return ""
	}
}

// OutputCredentials prints the credentials to stdout
func OutputCredentials(credentials daemonutils.ClusterCredentials) {
	ret, err := json.Marshal(credentials)
	if err != nil {
		klog.Fatalf("unable to serialize credentials: %+v", err)
	}
	fmt.Println(string(ret))
}
