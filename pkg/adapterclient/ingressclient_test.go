package adapterclient

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	discoveryfakes "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	testcore "k8s.io/client-go/testing"
	"k8s.io/ingress-gce/pkg/utils/patch"
)

var (
	ingressName   = "my-ingress"
	testNamespace = "test-namespace"
	v1Ingress     = &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressName,
			Labels: map[string]string{"type": "v1"},
		},
	}
	v1beta1Ingress = &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressName,
			Labels: map[string]string{"type": "v1beta1"},
		},
	}

	updatedIngress = &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressName,
			Labels: map[string]string{"new": "label"},
		},
		Spec: v1beta1.IngressSpec{
			Backend: &v1beta1.IngressBackend{
				ServiceName: "my-svc",
			},
		},
	}
	v1UpdatedIngress = &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressName,
			Labels: map[string]string{"new": "label"},
		},
		Spec: v1.IngressSpec{
			DefaultBackend: &v1.IngressBackend{
				Service: &v1.IngressServiceBackend{
					Name: "my-svc",
				},
			},
		},
	}
)

func TestIngressCreate(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			_, err = adapterClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			v1beta1Ing, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if v1Supported && err == nil {
				t.Fatalf("expected ingress not found err when getting v1beta1Ingress")
			} else if !v1Supported && err != nil {
				t.Fatalf("unexpected error when getting v1beta1Ingress: %q", err)
			}

			v1Ing, err := kubeClient.NetworkingV1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if !v1Supported && err == nil {
				t.Fatalf("expected ingress not found err when getting v1Ingress")
			} else if v1Supported && err != nil {
				t.Fatalf("unexpected error when getting v1Ingress: %q", err)
			}
			if v1Supported && !reflect.DeepEqual(v1Ing.Labels, v1beta1Ingress.Labels) {
				t.Fatalf("expected labels on v1 ingress to be %+v, but found %+v", v1beta1Ingress.Labels, v1Ing.Labels)
			} else if !v1Supported && !reflect.DeepEqual(v1beta1Ing.Labels, v1beta1Ingress.Labels) {
				t.Fatalf("expected labels on v1beta1 ingress to be %+v, but found %+v", v1beta1Ingress.Labels, v1beta1Ing.Labels)
			}
		})
	}
}

func TestIngressUpdate(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			_, err = adapterClient.NetworkingV1beta1().Ingresses(testNamespace).Update(context.TODO(), updatedIngress, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("errored updating v1beta1 ingress: %q", err)
			}

			v1beta1Ing, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingress: %q", err)
			}

			v1Ing, err := kubeClient.NetworkingV1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingress: %q", err)
			}

			if v1Supported {
				if !reflect.DeepEqual(v1Ing.Labels, updatedIngress.Labels) {
					t.Fatalf("expected adapter client to use v1 API to update Ingress")
				}

				if !reflect.DeepEqual(v1beta1Ing.Labels, v1beta1Ingress.Labels) {
					t.Fatalf("expected v1beta1 Ingress to be unchanged")
				}
			} else {
				if !reflect.DeepEqual(v1beta1Ing.Labels, updatedIngress.Labels) {
					t.Fatalf("expected adapter client to use v1beta1 API to update Ingress")
				}

				if !reflect.DeepEqual(v1Ing.Labels, v1Ingress.Labels) {
					t.Fatalf("expected v1 Ingress to be unchanged")
				}
			}
		})
	}
}

func TestIngressUpdateStatus(t *testing.T) {
	updatedIngressWithStatus := &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ingressName,
			Labels: map[string]string{"type": "v1beta1"},
		},
		Status: v1beta1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{IP: "1.2.3.4"},
				},
			},
		},
	}

	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			_, err = adapterClient.NetworkingV1beta1().Ingresses(testNamespace).UpdateStatus(context.TODO(), updatedIngressWithStatus, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			v1beta1Ing, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingress: %q", err)
			}

			v1Ing, err := kubeClient.NetworkingV1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingress: %q", err)
			}

			if v1Supported {
				if !reflect.DeepEqual(v1Ing.Status.LoadBalancer, updatedIngressWithStatus.Status.LoadBalancer) {
					t.Fatalf("expected v1 Ingress Status to be %+v, but found %+v", updatedIngressWithStatus.Status.LoadBalancer, v1Ing.Status.LoadBalancer)
				}
			} else {
				if !reflect.DeepEqual(v1beta1Ing.Status.LoadBalancer, updatedIngressWithStatus.Status.LoadBalancer) {
					t.Fatalf("expected v1beta1 Ingress Status to be %+v, but found %+v", updatedIngressWithStatus.Status.LoadBalancer, v1beta1Ing.Status.LoadBalancer)
				}
			}
		})
	}
}

func TestIngressDelete(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			err = adapterClient.NetworkingV1beta1().Ingresses(testNamespace).Delete(context.TODO(), ingressName, metav1.DeleteOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if !v1Supported && err == nil {
				t.Fatalf("expected ingress not found error when querying for v1beta1 Ingress")
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if v1Supported && err == nil {
				t.Fatalf("expected ingress not found error when querying for v1 Ingress")
			}
		})
	}
}

func TestIngressDeleteCollection(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			fakeClient := kubeClient.(*fake.Clientset)
			v1IngressAPICount := 0
			v1beta1IngressAPICount := 0
			reaction := func(action testcore.Action) (bool, runtime.Object, error) {
				if action.GetVerb() == "delete-collection" &&
					action.GetResource().Version == "v1" {
					v1IngressAPICount++
				}

				if action.GetVerb() == "delete-collection" &&
					action.GetResource().Version == "v1beta1" {
					v1beta1IngressAPICount++
				}
				return false, nil, nil
			}
			fakeClient.Fake.AddReactor("delete-collection", "ingresses", reaction)

			err = adapterClient.NetworkingV1beta1().Ingresses(testNamespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
			if err != nil {
				t.Fatalf("errored deleting collection: %q", err)
			}

			if v1Supported && (v1IngressAPICount != 1 || v1beta1IngressAPICount != 0) {
				t.Fatalf("adapter client should have only called v1 DeleteCollection API once")
			} else if !v1Supported && (v1IngressAPICount != 0 || v1beta1IngressAPICount != 1) {

				t.Fatalf("adapter client should have only called v1 DeleteCollection API once")
			}
		})
	}
}

func TestIngressGet(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			ing, err := adapterClient.NetworkingV1beta1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("error getting ingress with adapter client")
			}
			if ing == nil {
				t.Fatalf("retrieved ing should not be nil")
			}

			var expectedLabels map[string]string
			if v1Supported {
				expectedLabels = v1Ingress.Labels
			} else {
				expectedLabels = v1beta1Ingress.Labels
			}

			if !reflect.DeepEqual(ing.Labels, expectedLabels) {
				t.Fatalf("expected to get ingress with labels %+v but found labels %+v", expectedLabels, ing.Labels)
			}
		})
	}
}

func TestIngressList(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)

			v1beta1Ingress2 := v1beta1Ingress.DeepCopy()
			v1beta1Ingress2.Name = ingressClassName + "-2"
			v1Ingress2 := v1Ingress.DeepCopy()
			v1Ingress2.Name = ingressClassName + "-2"

			_, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}
			_, err = kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress2, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}
			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress2, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			ingList, err := adapterClient.NetworkingV1beta1().Ingresses(testNamespace).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("error getting ingress with adapter client")
			}
			if ingList == nil {
				t.Fatalf("ingress list should not be nil")
			}

			var expectedLabels map[string]string
			if v1Supported {
				expectedLabels = v1Ingress.Labels
			} else {
				expectedLabels = v1beta1Ingress.Labels
			}

			if len(ingList.Items) != 2 {
				t.Fatalf("expected ingress list to have 2 ingresses")
			}

			for _, ing := range ingList.Items {
				if !reflect.DeepEqual(ing.Labels, expectedLabels) {
					t.Fatalf("expected to get ingress %s with labels %+v but found labels %+v", ing.Name, expectedLabels, ing.Labels)
				}
			}
		})
	}
}

func TestIngressPatch(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			_, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Create(context.TODO(), v1beta1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1beta1 ingress: %q", err)
			}

			_, err = kubeClient.NetworkingV1().Ingresses(testNamespace).Create(context.TODO(), v1Ingress, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("errored creating v1 ingress: %q", err)
			}

			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapter client: %q", err)
			}

			// patch bytes depend on the whether the v1 or v1beta1 API because API fields
			// were renamed, v1beta1 patches cannot safely be applied to v1 resources.
			var patchBytes []byte
			if v1Supported {
				patchBytes, err = patch.StrategicMergePatchBytes(v1Ingress, v1UpdatedIngress, v1.IngressClass{})
				if err != nil {
					t.Fatalf("failed to create v1 patch bytes: %q", err)
				}

			} else {
				patchBytes, err = patch.StrategicMergePatchBytes(v1beta1Ingress, updatedIngress, v1beta1.Ingress{})
				if err != nil {
					t.Fatalf("failed to create v1beta1 patch bytes: %q", err)
				}
			}

			_, err = adapterClient.NetworkingV1beta1().Ingresses(testNamespace).Patch(context.TODO(), ingressName, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
			if err != nil {
				t.Fatalf("errored patching v1beta1 ingress: %q", err)
			}

			v1beta1Ing, err := kubeClient.NetworkingV1beta1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingress: %q", err)
			}

			v1Ing, err := kubeClient.NetworkingV1().Ingresses(testNamespace).Get(context.TODO(), ingressName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("errored getting v1beta1 ingress: %q", err)
			}

			if v1Supported {
				if !reflect.DeepEqual(v1Ing.Spec, v1UpdatedIngress.Spec) {
					t.Fatalf("expected adapter client to use v1 API to update Ingress")
				}

				if !reflect.DeepEqual(v1beta1Ing.Spec, v1beta1Ingress.Spec) {
					t.Fatalf("expected v1beta1 Ingress to be unchanged")
				}
			} else {
				if !reflect.DeepEqual(v1beta1Ing.Spec, updatedIngress.Spec) {
					t.Fatalf("expected adapter client to use v1beta1 API to update Ingress")
				}

				if !reflect.DeepEqual(v1Ing.Spec, v1Ingress.Spec) {
					t.Fatalf("expected v1 Ingress to be unchanged")
				}
			}
		})
	}
}

func TestIngressAdapterWatch(t *testing.T) {
	for _, v1Supported := range []bool{true, false} {
		t.Run(fmt.Sprintf("V1 Supported:%t", v1Supported), func(t *testing.T) {
			kubeClient := getFakeClients(v1Supported)
			adapterClient, _, err := NewAdapterKubeClient(kubeClient)
			if err != nil {
				t.Fatalf("failed to create adapterClient: %q", err)
			}

			adapterWatcher, err := adapterClient.NetworkingV1beta1().Ingresses(testNamespace).Watch(context.TODO(), metav1.ListOptions{})
			if err != nil {
				t.Fatalf("failed to get v1beta1 Watcher: %q", err)
			}

			watchChan := adapterWatcher.ResultChan()

			var fakeWatcher *watch.RaceFreeFakeWatcher
			if v1Supported {
				fakeWatcher = adapterWatcher.(*v1IngressAdapterWatcher).Interface.(*watch.RaceFreeFakeWatcher)
			} else {
				fakeWatcher = adapterWatcher.(*watch.RaceFreeFakeWatcher)
			}

			fakeWatcher.Add(v1Ingress)

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
			// client expects v1 Ingress objects and converts them to v1beta1
			if v1Supported {
				if _, ok := event.Object.(*v1beta1.Ingress); !ok {
					t.Fatalf("Ingress should have converted to v1beta1 type")
				}
			} else {
				if _, ok := event.Object.(*v1.Ingress); !ok {
					t.Fatalf("Ingress V1 should not have been converted v1beta1 type")
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

// getFakeClients creates a fake kube client and based on v1Supported adds the v1 APIs to
// the discovery service
func getFakeClients(v1Supported bool) kubernetes.Interface {
	kubeClient := fake.NewSimpleClientset()

	discovery := kubeClient.Discovery().(*discoveryfakes.FakeDiscovery)
	var v1Resources []metav1.APIResource
	if v1Supported {
		v1Resources = []metav1.APIResource{
			{Kind: "Ingress"}, {Kind: "IngressClass"},
		}
	}

	discovery.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "networking.k8s.io/v1",
			APIResources: v1Resources,
		},
		{
			GroupVersion: "networking.k8s.io/v1beta1",
			APIResources: []metav1.APIResource{
				{Kind: "Ingress"}, {Kind: "IngressClass"},
			},
		},
	}

	return kubeClient
}
