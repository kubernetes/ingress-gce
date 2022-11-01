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

package workload

import (
	"context"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	workloadv1a1 "k8s.io/ingress-gce/pkg/experimental/apis/workload/v1alpha1"
	workloadclient "k8s.io/ingress-gce/pkg/experimental/workload/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/common"
)

// newWorkloadController create a Workload controller.
func newWorkloadController() *Controller {
	kubeClient := fake.NewSimpleClientset()
	workloadClient := workloadclient.NewSimpleClientset()

	ctx := NewControllerContext(kubeClient, workloadClient, corev1.NamespaceAll, 1*time.Minute)
	wlc := NewController(ctx)
	wlc.hasSynced = func() bool { return true }

	return wlc
}

func addService(wlc *Controller, svc *corev1.Service) {
	wlc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	wlc.ctx.ServiceInformer.GetIndexer().Add(svc)
}

func addWorkload(wlc *Controller, wl *workloadv1a1.Workload) {
	wlc.ctx.WorkloadClient.NetworkingV1alpha1().Workloads(wl.Namespace).Create(context.TODO(), wl, metav1.CreateOptions{})
	wlc.ctx.WorkloadInformer.GetIndexer().Add(wl)
}

func updateWorkload(wlc *Controller, wl *workloadv1a1.Workload) {
	wlc.ctx.WorkloadClient.NetworkingV1alpha1().Workloads(wl.Namespace).Update(context.TODO(), wl, metav1.UpdateOptions{})
	wlc.ctx.WorkloadInformer.GetIndexer().Update(wl)
}

func deleteWorkload(wlc *Controller, wl *workloadv1a1.Workload) {
	wlc.ctx.WorkloadClient.NetworkingV1alpha1().Workloads(wl.Namespace).Delete(
		context.TODO(),
		wl.Name,
		metav1.DeleteOptions{},
	)
	wlc.ctx.WorkloadInformer.GetIndexer().Delete(wl)
}

func getEndpointSliceAddr(wlc *Controller, svc *corev1.Service, t *testing.T) []string {
	sliceName := endpointsliceName(svc.Name)
	eps, err := wlc.ctx.KubeClient.DiscoveryV1().EndpointSlices(svc.Namespace).Get(
		context.Background(),
		sliceName,
		metav1.GetOptions{},
	)
	if err != nil {
		// In this case, no slice is equivalent to an empty slice
		if errors.IsNotFound(err) {
			return []string{}
		}
		t.Errorf("Unable to get EndpointSlices %s", sliceName)
	}
	ret := []string{}
	for _, ep := range eps.Endpoints {
		ret = append(ret, ep.Addresses...)
	}
	return ret
}

// addEndpointSliceToLister adds the EndpointSlice to the lister so the controller knows the existence of it.
func addEndpointSliceToLister(wlc *Controller, svc *corev1.Service, t *testing.T) {
	sliceName := endpointsliceName(svc.Name)
	eps, err := wlc.ctx.KubeClient.DiscoveryV1().EndpointSlices(svc.Namespace).Get(
		context.Background(),
		sliceName,
		metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("Unable to get EndpointSlices %s", sliceName)
	}
	wlc.endpointSliceLister.Add(eps)
}

func newWorkload(name types.NamespacedName, addr string) *workloadv1a1.Workload {
	return &workloadv1a1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
			Labels: map[string]string{
				"type": "workload",
			},
		},
		Spec: workloadv1a1.WorkloadSpec{
			EnableHeartbeat: true,
			EnablePing:      true,
			Addresses: []workloadv1a1.ExternalWorkloadAddress{
				{
					AddressType: workloadv1a1.AddressTypeIPv4,
					Address:     addr,
				},
			},
		},
		Status: workloadv1a1.WorkloadStatus{
			Conditions: []workloadv1a1.Condition{
				{
					Type:               workloadv1a1.WorkloadConditionHeartbeat,
					Status:             workloadv1a1.ConditionStatusTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "Heartbeat",
				},
			},
		},
	}
}

func getServiceKey(svc *corev1.Service, t *testing.T) string {
	key, err := common.KeyFunc(svc)
	if err != nil {
		t.Fatalf("Unexpected error getting key for Service %v: %v", svc.Name, err)
	}
	return key
}

func TestWorkloadChange(t *testing.T) {
	wlc := newWorkloadController()
	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, corev1.ServiceSpec{
		Type:  corev1.ServiceTypeClusterIP,
		Ports: []corev1.ServicePort{{Port: 80}},
		Selector: map[string]string{
			"type": "workload",
		},
	})
	addService(wlc, svc)

	svcKey := getServiceKey(svc, t)
	processAndCheckAddresses := func(addrs []string) bool {
		if err := wlc.processService(svcKey); err != nil {
			t.Fatalf("lbc.sync(%v) = err %v", svcKey, err)
		}
		epsAddrs := getEndpointSliceAddr(wlc, svc, t)
		return reflect.DeepEqual(epsAddrs, addrs)
	}

	wl := newWorkload(types.NamespacedName{Name: "workload", Namespace: "default"}, "192.168.1.1")
	addWorkload(wlc, wl)
	if !processAndCheckAddresses([]string{"192.168.1.1"}) {
		t.Error("EndpointSlice does not contain correct workload address after Workload is created")
	}
	addEndpointSliceToLister(wlc, svc, t)

	wl.Spec.Addresses[0].Address = "192.168.1.2"
	updateWorkload(wlc, wl)
	if !processAndCheckAddresses([]string{"192.168.1.2"}) {
		t.Error("EndpointSlice does not contain correct workload address after Workload is updated")
	}

	deleteWorkload(wlc, wl)
	if !processAndCheckAddresses([]string{}) {
		t.Error("EndpointSlice is not empty after Workload is deleted")
	}
}
