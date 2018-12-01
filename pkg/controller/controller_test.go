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
	"fmt"
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
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/kubernetes/pkg/util/slice"
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
	ctx := context.NewControllerContext(kubeClient, backendConfigClient, nil, fakeGCE, namer, ctxConfig)
	lbc := NewLoadBalancerController(ctx, stopCh)
	// TODO(rramkumar): Fix this so we don't have to override with our fake
	lbc.instancePool = instances.NewNodePool(instances.NewFakeInstanceGroups(sets.NewString(), namer), namer)
	lbc.l7Pool = loadbalancers.NewLoadBalancerPool(loadbalancers.NewFakeLoadBalancers(clusterUID, namer), namer, nil, events.RecorderProducerMock{})
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

func updateIngress(lbc *LoadBalancerController, ing *extensions.Ingress) {
	lbc.ctx.KubeClient.Extensions().Ingresses(ing.Namespace).Update(ing)
	lbc.ctx.IngressInformer.GetIndexer().Update(ing)
}

func deleteIngress(lbc *LoadBalancerController, ing *extensions.Ingress) {
	lbc.ctx.KubeClient.Extensions().Ingresses(ing.Namespace).Delete(ing.Name, &meta_v1.DeleteOptions{})
	lbc.ctx.IngressInformer.GetIndexer().Delete(ing)
}

func addSecret(lbc *LoadBalancerController, secret *api_v1.Secret) {
	lbc.ctx.KubeClient.CoreV1().Secrets(secret.Namespace).Create(secret)
	lbc.ctx.SecretInformer.GetIndexer().Add(secret)
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

// TestIngressClassChange asserts that `sync` will not return an error for a good ingress config
// status is updated and LB is deleted after class change.
func TestIngressClassChange(t *testing.T) {
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
	ing.ObjectMeta.Annotations = map[string]string{"kubernetes.io/ingress.class": "gce"}
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
	}

	ing.ObjectMeta.Annotations = map[string]string{"kubernetes.io/ingress.class": "new-class"}
	updateIngress(lbc, ing)

	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
	}

	// Check status of LoadBalancer is updated after class changes
	updatedIng, _ := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if len(updatedIng.Status.LoadBalancer.Ingress) != 0 {
		t.Error("Ingress status wasn't updated after class changed")
	}

	// Check LB for ingress is deleted after class changed
	if pool, _ := lbc.l7Pool.Get(ingStoreKey); pool != nil {
		t.Errorf("LB(%v) wasn't deleted after class changed", ingStoreKey)
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

func TestMultiIngressLB(t *testing.T) {
	lbc := newLoadBalancerController()

	defaultSvc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	ing1Svc := test.NewService(types.NamespacedName{Name: "ing1-backend", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 8080}},
	})
	tlsSvc := test.NewService(types.NamespacedName{Name: "tls-backend", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 8082}},
	})

	tlsSecret := test.NewSecret(types.NamespacedName{Name: "tls-secret", Namespace: "default"}, api_v1.SecretTypeTLS, map[string][]byte{
		"tls.crt": []byte("value"),
		"tls.key": []byte("key"),
	})
	tls2Secret := test.NewSecret(types.NamespacedName{Name: "tls2-secret", Namespace: "default"}, api_v1.SecretTypeTLS, map[string][]byte{
		"tls.crt": []byte("value"),
		"tls.key": []byte("key"),
	})
	addService(lbc, defaultSvc)
	addService(lbc, ing1Svc)
	addService(lbc, tlsSvc)
	addSecret(lbc, tlsSecret)
	addSecret(lbc, tls2Secret)

	defaultBackend := backend("my-service", intstr.FromInt(80))
	ing1Backend := backend("ing1-backend", intstr.FromInt(8080))
	tlsBackend := backend("tls-backend", intstr.FromInt(8082))
	// Add first ingress
	ing1 := test.NewIngress(types.NamespacedName{Name: "my-ingress1", Namespace: "default"},
		extensions.IngressSpec{
			Backend: &defaultBackend,
			Rules: []extensions.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: extensions.IngressRuleValue{
						HTTP: &extensions.HTTPIngressRuleValue{
							Paths: []extensions.HTTPIngressPath{
								{
									Path:    "/path1/",
									Backend: ing1Backend,
								},
							},
						},
					},
				},
				{
					Host: "tls-example.com",
					IngressRuleValue: extensions.IngressRuleValue{
						HTTP: &extensions.HTTPIngressRuleValue{
							Paths: []extensions.HTTPIngressPath{
								{
									Path:    "/tls/",
									Backend: tlsBackend,
								},
							},
						},
					},
				},
			},
			TLS: []extensions.IngressTLS{
				{
					Hosts: []string{
						"example.com",
					},
					SecretName: "tls-secret",
				},
				{
					Hosts: []string{
						"tls-example.com",
					},
					SecretName: "tls2-secret",
				},
			},
		})
	ing1.ObjectMeta.Annotations = map[string]string{
		"kubernetes.io/ingress.class":             "gce",
		"kubernetes.io/ingress.loadbalancer-name": "mylb",
	}
	addIngress(lbc, ing1)

	ingStoreKey1 := getKey(ing1, t)
	if err := lbc.sync(ingStoreKey1); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey1, err)
	}

	ing2Backend := backend("ing2-backend", intstr.FromInt(8081))
	ing2Svc := test.NewService(types.NamespacedName{Name: "ing2-backend", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 8081}},
	})
	addService(lbc, ing2Svc)
	// Add second ingress
	ing2 := test.NewIngress(types.NamespacedName{Name: "my-ingress2", Namespace: "default"},
		extensions.IngressSpec{
			Backend: &defaultBackend,
			Rules: []extensions.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: extensions.IngressRuleValue{
						HTTP: &extensions.HTTPIngressRuleValue{
							Paths: []extensions.HTTPIngressPath{
								{
									Path:    "/path2/",
									Backend: ing2Backend,
								},
							},
						},
					},
				},
			},
			TLS: []extensions.IngressTLS{
				{
					Hosts: []string{
						"example.com",
					},
					SecretName: "tls-secret",
				},
			},
		})
	ing2.ObjectMeta.Annotations = map[string]string{
		"kubernetes.io/ingress.class":             "gce",
		"kubernetes.io/ingress.loadbalancer-name": "mylb",
	}
	addIngress(lbc, ing2)

	ingStoreKey2 := getKey(ing2, t)
	if err := lbc.sync(ingStoreKey2); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey2, err)
	}

	updatedIng1, _ := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing1.Namespace).Get(ing1.Name, meta_v1.GetOptions{})
	updatedIng2, _ := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing2.Namespace).Get(ing2.Name, meta_v1.GetOptions{})

	if len(updatedIng1.Status.LoadBalancer.Ingress) == 0 {
		t.Fatalf("Ingress status wasn't updated %+v", updatedIng1)
	}

	if len(updatedIng2.Status.LoadBalancer.Ingress) == 0 {
		t.Fatalf("Ingress status wasn't updated %+v", updatedIng2)
	}

	if updatedIng1.Status.LoadBalancer.Ingress[0].IP != updatedIng2.Status.LoadBalancer.Ingress[0].IP {
		t.Error("Ingresses with the same LB should have 1 IP")
	}

	lbName1, _, _ := lbc.GetLoadBalancerName(ing1)
	lbName2, _, _ := lbc.GetLoadBalancerName(ing2)
	if lbName1 != lbName2 {
		t.Errorf("Ingresses must have equal lb name, but lb1: %+v, lb2: %+v", lbName1, lbName2)
	}

	// TODO: add check for url map
	lb, _ := lbc.l7Pool.Get(lbName1)
	var hostRules []string
	for _, r := range lb.UrlMap().HostRules {
		hostRules = append(hostRules, r.Hosts...)
		pm, err := getPathMatcher(lb, r.PathMatcher)
		if err != nil {
			t.Error(err)
		}
		if slice.ContainsString(r.Hosts, "example.com", nil) {
			if !pathRuleExists(pm.PathRules, "/path1/", ing1Backend.ServiceName) {
				t.Errorf("PathMatcher %+v for host example.com must contain /path1/ to service %+v", pm.Name, ing1Backend.ServiceName)
			}
			if !pathRuleExists(pm.PathRules, "/path2/", ing1Backend.ServiceName) {
				t.Errorf("PathMatcher %+v for host example.com must contain /path2/ to service %+v", pm.Name, ing2Backend.ServiceName)
			}
		}
		if slice.ContainsString(r.Hosts, "tls-example.com", nil) {
			if !pathRuleExists(pm.PathRules, "/tls/", ing1Backend.ServiceName) {
				t.Errorf("PathMatcher %+v for host tls-example.com must contain /path1/ to service %+v", pm.Name, tlsBackend.ServiceName)
			}
		}
	}

	if !slice.ContainsString(hostRules, "example.com", nil) {
		t.Errorf("lb must contain host rule for host example.com")
	}

	if !slice.ContainsString(hostRules, "tls-example.com", nil) {
		t.Errorf("lb must contain host rule for host tls-example.com")
	}

	// TODO: add check for TLS

}

func getPathMatcher(lbc *loadbalancers.L7, pathMatcherName string) (*compute.PathMatcher, error) {
	for _, pm := range lbc.UrlMap().PathMatchers {
		if pm.Name == pathMatcherName {
			return pm, nil
		}
	}
	return nil, fmt.Errorf("failed to find pathMatcher %+v", pathMatcherName)
}

func pathRuleExists(pathRules []*compute.PathRule, path string, service string) bool {
	for _, pr := range pathRules {
		//if pr.Service == service && slice.ContainsString(pr.Paths, path, nil) {
		//	return true
		//}
		if slice.ContainsString(pr.Paths, path, nil) {
			return true
		}
	}
	return false
}
