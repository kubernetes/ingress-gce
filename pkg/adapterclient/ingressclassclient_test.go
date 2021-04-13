/*
Copyright 2021 The Kubernetes Authors.

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
package adapterclient

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	testcore "k8s.io/client-go/testing"
	"k8s.io/ingress-gce/pkg/utils/patch"
)

var (
	ingressClassName = "my-class"
	v1IngressClass   = &v1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressClassName,
			Labels: map[string]string{"type": "v1"},
		},
	}
	v1beta1IngressClass = &v1beta1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressClassName,
			Labels: map[string]string{"type": "v1beta1"},
		},
	}

	updatedIngressClass = &v1beta1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressClassName,
			Labels: map[string]string{"new": "label"},
		},
	}
	v1UpdatedIngressClass = &v1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressClassName,
			Labels: map[string]string{"new": "label"},
		},
	}
)

func TestIngressClassCreate(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			_, err = adapterClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			v1beta1Class, err := kubeClient.NetworkingV1beta1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if v1Supported && err == nil {
				t.Fatalf("expected ingress not found err when getting v1beta1 IngressClass")
			} else if !v1Supported && err != nil {
				t.Fatalf("unexpected error when getting v1beta1 IngressClass: %q", err)
			}

			v1Class, err := kubeClient.NetworkingV1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if !v1Supported && err == nil {
				t.Fatalf("expected ingressClass not found err when getting v1 IngressClass")
			} else if v1Supported && err != nil {
				t.Fatalf("unexpected error when getting v1 IngressClass: %q", err)
			}
			if v1Supported && !reflect.DeepEqual(v1Class.Labels, v1beta1IngressClass.Labels) {
				t.Fatalf("expected labels on v1 ingressClass to be %+v, but found %+v", v1beta1Class.Labels, v1Class.Labels)
			} else if !v1Supported && !reflect.DeepEqual(v1beta1Class.Labels, v1beta1IngressClass.Labels) {
				t.Fatalf("expected labels on v1beta1 ingressClass to be %+v, but found %+v", v1beta1Class.Labels, v1beta1Class.Labels)
			}
		})
	}
}

func TestIngressClassUpdate(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingressClass: %q", err)
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Create(context.TODO(), v1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingressClass: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			_, err = adapterClient.NetworkingV1beta1().IngressClasses().Update(context.TODO(), updatedIngressClass, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("errored updating v1beta1 ingressClass: %q", err)
			}

			v1beta1Class, err := kubeClient.NetworkingV1beta1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingressClass: %q", err)
			}

			v1Class, err := kubeClient.NetworkingV1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingressClass: %q", err)
			}

			if v1Supported {
				if !reflect.DeepEqual(v1Class.Labels, updatedIngressClass.Labels) {
					t.Fatalf("expected adapter client to use v1 API to update Ingress")
				}

				if !reflect.DeepEqual(v1beta1Class.Labels, v1beta1IngressClass.Labels) {
					t.Fatalf("expected v1beta1 Ingress Class to be unchanged")
				}
			} else {
				if !reflect.DeepEqual(v1beta1Class.Labels, updatedIngressClass.Labels) {
					t.Fatalf("expected adapter client to use v1beta1 API to update Ingress Class")
				}

				if !reflect.DeepEqual(v1Class.Labels, v1IngressClass.Labels) {
					t.Fatalf("expected v1 Ingress Class to be unchanged")
				}
			}
		})
	}
}

func TestIngressClassDelete(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Create(context.TODO(), v1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress class: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			err = adapterClient.NetworkingV1beta1().IngressClasses().Delete(context.TODO(), ingressClassName, metav1.DeleteOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1beta1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if !v1Supported && err == nil {
				t.Fatalf("expected ingress not found error when querying for v1beta1 IngressClass")
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if v1Supported && err == nil {
				t.Fatalf("expected ingress not found error when querying for v1 IngressClass")
			}
		})
	}
}

func TestIngressClassDeleteCollection(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Create(context.TODO(), v1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress class: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			fakeClient := kubeClient.(*fake.Clientset)
			v1APICount := 0
			v1beta1APICount := 0
			reaction := func(action testcore.Action) (bool, runtime.Object, error) {
				if action.GetVerb() == "delete-collection" &&
					action.GetResource().Version == "v1" {
					v1APICount++
				}

				if action.GetVerb() == "delete-collection" &&
					action.GetResource().Version == "v1beta1" {
					v1beta1APICount++
				}
				return false, nil, nil
			}
			fakeClient.Fake.AddReactor("delete-collection", "ingressclasses", reaction)

			err = adapterClient.NetworkingV1beta1().IngressClasses().DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
			if err != nil {
				t.Fatalf("errored deleting collection: %q", err)
			}

			if v1Supported && (v1APICount != 1 || v1beta1APICount != 0) {
				t.Fatalf("adapter client should have only called v1 DeleteCollection API once")
			} else if !v1Supported && (v1APICount != 0 || v1beta1APICount != 1) {

				t.Fatalf("adapter client should have only called v1 DeleteCollection API once")
			}
		})
	}
}

func TestIngressClassGet(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Create(context.TODO(), v1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress class: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			class, err := adapterClient.NetworkingV1beta1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error getting ingress class with adapter client")
			}
			if class == nil {
				t.Fatalf("retrieved class should not be nil")
			}

			var expectedLabels map[string]string
			if v1Supported {
				expectedLabels = v1IngressClass.Labels
			} else {
				expectedLabels = v1beta1IngressClass.Labels
			}

			if !reflect.DeepEqual(class.Labels, expectedLabels) {
				t.Fatalf("expected to get ingress class with labels %+v but found labels %+v", expectedLabels, class.Labels)
			}
		})
	}
}

func TestIngressClassList(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			v1beta1IngressClass2 := v1beta1IngressClass.DeepCopy()
			v1beta1IngressClass2.Name = ingressClassName + "-2"

			v1IngressClass2 := v1IngressClass.DeepCopy()
			v1IngressClass2.Name = ingressClassName + "-2"

			_, err := kubeClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass2, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Create(context.TODO(), v1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Create(context.TODO(), v1IngressClass2, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress class: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			classList, err := adapterClient.NetworkingV1beta1().IngressClasses().List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("error getting ingress class with adapter client")
			}
			if classList == nil {
				t.Fatalf("ingress class list should not be nil")
			}

			var expectedLabels map[string]string
			if v1Supported {
				expectedLabels = v1IngressClass.Labels
			} else {
				expectedLabels = v1beta1IngressClass.Labels
			}

			if len(classList.Items) != 2 {
				t.Fatalf("expected ingress class list to have 2 ingress classes")
			}

			for _, class := range classList.Items {
				if !reflect.DeepEqual(class.Labels, expectedLabels) {
					t.Fatalf("expected to get ingress class %s with labels %+v but found labels %+v", class.Name, expectedLabels, classList.Items[0].Labels)
				}
			}
		})
	}
}

func TestIngressClassPatch(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().IngressClasses().Create(context.TODO(), v1beta1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress class: %q", err)
			}

			_, err = kubeClient.NetworkingV1().IngressClasses().Create(context.TODO(), v1IngressClass, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress class: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			// patch bytes depend on the whether the v1 or v1beta1 API because API fields
			// were renamed, v1beta1 patches cannot safely be applied to v1 resources.
			var patchBytes []byte
			if v1Supported {
				patchBytes, err = patch.StrategicMergePatchBytes(v1IngressClass, v1UpdatedIngressClass, v1.IngressClass{})
				if err != nil {
					t.Fatalf("failed to create v1 patch bytes: %q", err)
				}

			} else {
				patchBytes, err = patch.StrategicMergePatchBytes(v1beta1IngressClass, updatedIngressClass, v1beta1.IngressClass{})
				if err != nil {
					t.Fatalf("failed to create v1beta1 patch bytes: %q", err)
				}
			}

			_, err = adapterClient.NetworkingV1beta1().IngressClasses().Patch(context.TODO(), ingressClassName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
			if err != nil {
				t.Fatalf("errored patching v1beta1 ingressClass : %q", err)
			}

			v1beta1Ing, err := kubeClient.NetworkingV1beta1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingressClass: %q", err)
			}

			v1Ing, err := kubeClient.NetworkingV1().IngressClasses().Get(context.TODO(), ingressClassName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingressClass: %q", err)
			}

			if v1Supported {
				if !reflect.DeepEqual(v1Ing.Labels, v1UpdatedIngress.Labels) {
					t.Fatalf("expected adapter client to use v1 API to update Ingress Class")
				}

				if !reflect.DeepEqual(v1beta1Ing.Labels, v1beta1Ingress.Labels) {
					t.Fatalf("expected v1beta1 Ingress Class to be unchanged")
				}
			} else {
				if !reflect.DeepEqual(v1beta1Ing.Labels, updatedIngress.Labels) {
					t.Fatalf("expected adapter client to use v1beta1 API to update Ingress Class")
				}

				if !reflect.DeepEqual(v1Ing.Labels, v1Ingress.Labels) {
					t.Fatalf("expected v1 Ingress Class to be unchanged")
				}
			}
		})
	}
}

func TestIngressClassAdapterWatch(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapterClient: %q", err)
			}

			adapterWatcher, err := adapterClient.NetworkingV1beta1().IngressClasses().Watch(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("failed to get v1beta1 Ingress Class Watcher: %q", err)
			}

			watchChan := adapterWatcher.ResultChan()

			var fakeWatcher *watch.RaceFreeFakeWatcher
			if v1Supported {
				fakeWatcher = adapterWatcher.(*v1IngressClassAdapterWatcher).Interface.(*watch.RaceFreeFakeWatcher)
			} else {
				fakeWatcher = adapterWatcher.(*watch.RaceFreeFakeWatcher)
			}

			fakeWatcher.Add(v1IngressClass)

			timer := time.NewTimer(1 * time.Second)
			var event watch.Event
			select {
			case <-timer.C:
				adapterWatcher.Stop()
				t.Fatalf("no event within 1 second")
			case event = <-watchChan:
			}

			// NOTE: This is not expected behavior of the watcher, but because there is no validation
			// on the fake watcher, all objects are returned regardless of object type. The adapter
			// client expects v1 Ingress Class objects and converts them to v1beta1
			if v1Supported {
				if _, ok := event.Object.(*v1beta1.IngressClass); !ok {
					t.Fatalf("IngressClass should have converted to v1beta1 type")
				}
			} else {
				if _, ok := event.Object.(*v1.IngressClass); !ok {
					t.Fatalf("IngressClass V1 should not have been converted v1beta1 type")
				}
			}

			adapterWatcher.Stop()
			_, open := <-watchChan
			if open {
				t.Fatal("expected event chan to be closed after adapterWatcher.Stop()")
			}
		})
	}
}
