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

package controller

import (
	"strings"
	"testing"
	"time"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	"k8s.io/ingress-gce/pkg/context"
)

var (
	nodePortCounter = 30000
	clusterUID      = "aaaaa"
)

// newLoadBalancerController create a loadbalancer controller.
func newLoadBalancerController() *LoadBalancerController {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	fakeGCE := gce.FakeGCECloud(gce.DefaultTestClusterValues())
	namer := utils.NewNamer(clusterUID, "")

	stopCh := make(chan struct{})
	ctxConfig := context.ControllerContextConfig{
		NEGEnabled:                    true,
		BackendConfigEnabled:          false,
		Namespace:                     api_v1.NamespaceAll,
		ResyncPeriod:                  1 * time.Minute,
		DefaultBackendSvcPortID:       test.DefaultBeSvcPort.ID,
		HealthCheckPath:               "/",
		DefaultBackendHealthCheckPath: "/healthz",
	}
	ctx := context.NewControllerContext(kubeClient, backendConfigClient, fakeGCE, namer, ctxConfig)
	lbc := NewLoadBalancerController(ctx, stopCh)
	// TODO(rramkumar): Fix this so we don't have to override with our fake
	lbc.instancePool = instances.NewNodePool(instances.NewFakeInstanceGroups(sets.NewString(), namer), namer)
	lbc.l7Pool = loadbalancers.NewLoadBalancerPool(loadbalancers.NewFakeLoadBalancers(clusterUID, namer), namer, false)
	lbc.instancePool.Init(&instances.FakeZoneLister{Zones: []string{"zone-a"}})

	lbc.hasSynced = func() bool { return true }

	// Create the default-backend service.
	defaultSvc := test.NewService(test.DefaultBeSvcPort.ID.Service, api_v1.ServiceSpec{
		Type: api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{
			{
				Name: "http",
				Port: 80,
			},
		},
	})
	addService(lbc, defaultSvc)

	return lbc
}

func addService(lbc *LoadBalancerController, svc *api_v1.Service) {
	if svc.Spec.Type == api_v1.ServiceTypeNodePort {
		for x, p := range svc.Spec.Ports {
			if p.NodePort == 0 {
				svc.Spec.Ports[x].NodePort = int32(nodePortCounter)
				nodePortCounter++
			}
		}
	}

	lbc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Create(svc)
	lbc.ctx.ServiceInformer.GetIndexer().Add(svc)
}

func addIngress(lbc *LoadBalancerController, ing *extensions.Ingress) {
	lbc.ctx.KubeClient.Extensions().Ingresses(ing.Namespace).Create(ing)
	lbc.ctx.IngressInformer.GetIndexer().Add(ing)
}

func deleteIngress(lbc *LoadBalancerController, ing *extensions.Ingress) {
	lbc.ctx.KubeClient.Extensions().Ingresses(ing.Namespace).Delete(ing.Name, &meta_v1.DeleteOptions{})
	lbc.ctx.IngressInformer.GetIndexer().Delete(ing)
}

// getKey returns the key for an ingress.
func getKey(ing *extensions.Ingress, t *testing.T) string {
	key, err := utils.KeyFunc(ing)
	if err != nil {
		t.Fatalf("Unexpected error getting key for Ingress %v: %v", ing.Name, err)
	}
	return key
}

func backend(name string, port intstr.IntOrString) extensions.IngressBackend {
	return extensions.IngressBackend{
		ServiceName: name,
		ServicePort: port,
	}
}

// TestIngressSyncError asserts that `sync` will bubble an error when an ingress cannot be synced
// due to configuration problems.
func TestIngressSyncError(t *testing.T) {
	lbc := newLoadBalancerController()

	someBackend := backend("my-service", intstr.FromInt(80))
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		extensions.IngressSpec{
			Backend: &someBackend,
		})
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	err := lbc.sync(ingStoreKey)
	if err == nil {
		t.Fatalf("lbc.sync(%v) = nil, want error", ingStoreKey)
	}

	if !strings.Contains(err.Error(), someBackend.ServiceName) {
		t.Errorf("lbc.sync(%v) = %v, want error containing %q", ingStoreKey, err, someBackend.ServiceName)
	}
}

// TestIngressCreateDelete asserts that `sync` will not return an error for a good ingress config
// and will not return an error when the ingress is deleted.
func TestIngressCreateDelete(t *testing.T) {
	lbc := newLoadBalancerController()

	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)

	defaultBackend := backend("my-service", intstr.FromInt(80))
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		extensions.IngressSpec{
			Backend: &defaultBackend,
		})
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
	}

	// Check Ingress status has IP.
	updatedIng, _ := lbc.ctx.KubeClient.Extensions().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if len(updatedIng.Status.LoadBalancer.Ingress) != 1 || updatedIng.Status.LoadBalancer.Ingress[0].IP == "" {
		t.Errorf("Get(%q) = status %+v, want non-empty", updatedIng.Name, updatedIng.Status.LoadBalancer.Ingress)
	}

	deleteIngress(lbc, ing)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
	}
}

// TestEnsureMCIngress asserts a multi-cluster ingress will result with correct status annotations.
func TestEnsureMCIngress(t *testing.T) {
	lbc := newLoadBalancerController()

	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)

	defaultBackend := backend("my-service", intstr.FromInt(80))
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		extensions.IngressSpec{
			Backend: &defaultBackend,
		})
	ing.ObjectMeta.Annotations = map[string]string{"kubernetes.io/ingress.class": "gce-multi-cluster"}
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
	}

	// Check Ingress has annotations noting the instance group name.
	updatedIng, _ := lbc.ctx.KubeClient.Extensions().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	igAnnotationKey := "ingress.gcp.kubernetes.io/instance-groups"
	wantVal := `[{"Name":"k8s-ig--aaaaa","Zone":"zone-a"}]`
	if val, ok := updatedIng.GetAnnotations()[igAnnotationKey]; !ok {
		t.Errorf("Ingress.Annotations does not contain key %q", igAnnotationKey)
	} else if val != wantVal {
		t.Errorf("Ingress.Annotation %q = %q, want %q", igAnnotationKey, val, wantVal)
	}
}
