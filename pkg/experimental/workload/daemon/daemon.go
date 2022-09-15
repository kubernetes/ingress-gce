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
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
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
	wlcr := getWorkloadCR(workload)
	_, err = client.Create(context.Background(), wlcr, metav1.CreateOptions{})
	if err != nil {
		klog.Fatalf("unable to create the workload resource: %+v", err)
	}
	klog.V(2).Infof("workload resource created: %s", name)

	// Update the heartbeat regularly
	ticker := time.NewTicker(updateInterval)
	quit := make(chan interface{})
	sigs := make(chan os.Signal, 1)
	go updateCR(wlcr, clientset, ticker, sigs, quit)

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

func updateCR(
	workload *workloadv1a1.Workload,
	clientset workloadclient.Interface,
	ticker *time.Ticker,
	sigs chan os.Signal,
	quit chan interface{},
) {
	oldStatus := workload.Status
	for {
		select {
		case <-ticker.C:
			newStatus := generateHeartbeatStatus()
			patch, err := preparePatchBytesForWorkloadStatus(oldStatus, newStatus)
			if err != nil {
				klog.Errorf("failed to prepare the patch for workload resource: %+v", err)
				continue
			}
			oldStatus = newStatus

			vmInstClient := clientset.NetworkingV1alpha1().Workloads(corev1.NamespaceDefault)
			// WARNING: This patch does not work with Ping and Ready, since it will overwrite the whole conditions list
			// TODO: The following options are considered:
			//   - Get and Update. The problem is that it may be too heavy for Heartbeat.
			//   - Fix the index of Heartbeat in the list, and do a JSON patch. Maybe too hacky.
			//   - StrategicMerge does not work for CRD now, but hopefully it may work after
			//     we switch to metav1.Condition. Not been tested yet.
			//   - Use server-side Apply Patch. This only works for 1.18+.
			_, err = vmInstClient.Patch(context.Background(), workload.Name, types.MergePatchType,
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
	ret := workloadv1a1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:   stringOrEmpty(workload.Name()),
			Labels: workload.Labels(),
		},
		Spec: workloadv1a1.WorkloadSpec{
			EnableHeartbeat: true,
			EnablePing:      true,
		},
		Status: generateHeartbeatStatus(),
	}
	if region, exist := workload.Region(); exist {
		ret.ObjectMeta.Labels["topology.kubernetes.io/region"] = region
	}
	if zone, exist := workload.Zone(); exist {
		ret.ObjectMeta.Labels["topology.kubernetes.io/region"] = zone
	}
	if hostname, exist := workload.Hostname(); exist {
		ret.Spec.Hostname = &hostname
	}
	if ip, exist := workload.IP(); exist {
		ret.Spec.Addresses = []workloadv1a1.ExternalWorkloadAddress{
			{
				Address:     ip,
				AddressType: workloadv1a1.AddressTypeIPv4,
			},
		}
	}
	return &ret
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

// preparePatchBytesForWorkloadStatus generates patch bytes based on the old and new workload status
func preparePatchBytesForWorkloadStatus(oldStatus, newStatus workloadv1a1.WorkloadStatus) ([]byte, error) {
	patchBytes, err := patch.StrategicMergePatchBytes(
		workloadv1a1.Workload{Status: oldStatus},
		workloadv1a1.Workload{Status: newStatus},
		workloadv1a1.Workload{},
	)
	return patchBytes, err
}

func generateHeartbeatStatus() workloadv1a1.WorkloadStatus {
	return workloadv1a1.WorkloadStatus{
		Conditions: []workloadv1a1.Condition{
			{
				Type:               workloadv1a1.WorkloadConditionHeartbeat,
				Status:             workloadv1a1.ConditionStatusTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "Heartbeat",
			},
		},
	}
}
