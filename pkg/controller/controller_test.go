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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/tls"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
	"strings"
	"testing"
	"time"

	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
)

var (
	nodePortCounter = 30000
	clusterUID      = "aaaaa"
)

// newLoadBalancerController create a loadbalancer controller.
func newLoadBalancerController() *LoadBalancerController {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	(fakeGCE.Compute().(*cloud.MockGCE)).MockGlobalForwardingRules.InsertHook = loadbalancers.InsertGlobalForwardingRuleHook
	namer := utils.NewNamer(clusterUID, "")

	stopCh := make(chan struct{})
	ctxConfig := context.ControllerContextConfig{
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
	lbc.l7Pool = loadbalancers.NewLoadBalancerPool(fakeGCE, namer, events.RecorderProducerMock{})
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
	lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Create(ing)
	lbc.ctx.IngressInformer.GetIndexer().Add(ing)
}

func updateIngress(lbc *LoadBalancerController, ing *extensions.Ingress) {
	lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Update(ing)
	lbc.ctx.IngressInformer.GetIndexer().Update(ing)
}

func setDeletionTimestamp(lbc *LoadBalancerController, ing *extensions.Ingress) {
	ts := meta_v1.NewTime(time.Now())
	ing.SetDeletionTimestamp(&ts)
	updateIngress(lbc, ing)
}

func deleteIngress(lbc *LoadBalancerController, ing *extensions.Ingress) {
	if len(ing.GetFinalizers()) == 0 {
		lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Delete(ing.Name, &meta_v1.DeleteOptions{})
		lbc.ctx.IngressInformer.GetIndexer().Delete(ing)
	}
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

// TestIngressCreateDeleteFinalizer asserts that `sync` will will not return an
// error for a good ingress config. It also tests garbage collection for
// Ingresses that need to be deleted, and keep the ones that don't, depending
// on whether Finalizer Adds and/or Removes are enabled.
func TestIngressCreateDeleteFinalizer(t *testing.T) {
	testCases := []struct {
		enableFinalizerAdd    bool
		enableFinalizerRemove bool
		ingNames              []string
		desc                  string
	}{
		{
			enableFinalizerAdd:    true,
			enableFinalizerRemove: true,
			ingNames:              []string{"ing-1", "ing-2", "ing-3"},
			desc:                  "both FinalizerAdd and FinalizerRemove are enabled",
		},
		{
			ingNames: []string{"ing-1", "ing-2", "ing-3"},
			desc:     "both FinalizerAdd and FinalizerRemove are disabled",
		},
		{
			enableFinalizerAdd: true,
			ingNames:           []string{"ing-1", "ing-2", "ing-3"},
			desc:               "FinalizerAdd is enabled",
		},
		{
			enableFinalizerRemove: true,
			ingNames:              []string{"ing-1", "ing-2", "ing-3"},
			desc:                  "FinalizerRemove is enabled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			flags.F.FinalizerAdd = tc.enableFinalizerAdd
			flags.F.FinalizerRemove = tc.enableFinalizerRemove

			lbc := newLoadBalancerController()
			svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
				Type:  api_v1.ServiceTypeNodePort,
				Ports: []api_v1.ServicePort{{Port: 80}},
			})
			addService(lbc, svc)
			defaultBackend := backend("my-service", intstr.FromInt(80))

			for _, name := range tc.ingNames {
				ing := test.NewIngress(types.NamespacedName{Name: name, Namespace: "default"},
					extensions.IngressSpec{
						Backend: &defaultBackend,
					})
				addIngress(lbc, ing)

				ingStoreKey := getKey(ing, t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
				}

				updatedIng, _ := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})

				// Check Ingress status has IP.
				if len(updatedIng.Status.LoadBalancer.Ingress) != 1 || updatedIng.Status.LoadBalancer.Ingress[0].IP == "" {
					t.Errorf("Get(%q) = status %+v, want non-empty", updatedIng.Name, updatedIng.Status.LoadBalancer.Ingress)
				}

				// Check Ingress has Finalizer if the FinalizerAdd flag is true
				if tc.enableFinalizerAdd && len(updatedIng.GetFinalizers()) != 1 {
					t.Errorf("GetFinalizers() = %+v, want 1", updatedIng.GetFinalizers())
				}

				// Check Ingress DOES NOT have Finalizer if FinalizerAdd flag is false
				if !tc.enableFinalizerAdd && len(updatedIng.GetFinalizers()) == 1 {
					t.Errorf("GetFinalizers() = %+v, want 0", updatedIng.GetFinalizers())
				}
			}

			for i, name := range tc.ingNames {
				ing, _ := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses("default").Get(name, meta_v1.GetOptions{})
				setDeletionTimestamp(lbc, ing)

				ingStoreKey := getKey(ing, t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
				}

				updatedIng, _ := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses("default").Get(name, meta_v1.GetOptions{})
				deleteIngress(lbc, updatedIng)

				updatedIng, _ = lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses("default").Get(name, meta_v1.GetOptions{})
				if tc.enableFinalizerAdd && !tc.enableFinalizerRemove {
					if updatedIng == nil {
						t.Fatalf("Expected Ingress not to be deleted")
					}

					if len(updatedIng.GetFinalizers()) != 1 {
						t.Errorf("GetFinalizers() = %+v, want 0", updatedIng.GetFinalizers())
					}
					continue
				}

				if updatedIng != nil {
					t.Fatalf("Ingress was not deleted, got: %+v", updatedIng)
				}

				remainingIngresses, err := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses("default").List(meta_v1.ListOptions{})
				if err != nil {
					t.Fatalf("List() = err %v", err)
				}

				remainingIngCount := len(tc.ingNames) - i - 1
				if len(remainingIngresses.Items) != remainingIngCount {
					t.Fatalf("Expected %d Ingresses, got: %d", remainingIngCount, len(remainingIngresses.Items))
				}
			}
		})
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
	updatedIng, _ := lbc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	igAnnotationKey := "ingress.gcp.kubernetes.io/instance-groups"
	wantVal := `[{"Name":"k8s-ig--aaaaa","Zone":"zone-a"}]`
	if val, ok := updatedIng.GetAnnotations()[igAnnotationKey]; !ok {
		t.Errorf("Ingress.Annotations does not contain key %q", igAnnotationKey)
	} else if val != wantVal {
		t.Errorf("Ingress.Annotation %q = %q, want %q", igAnnotationKey, val, wantVal)
	}
}

// TestToRuntimeInfoCerts asserts that both pre-shared and secret-based certs
// are included in the RuntimeInfo.
func TestToRuntimeInfoCerts(t *testing.T) {
	lbc := newLoadBalancerController()
	tlsCerts := []*loadbalancers.TLSCerts{&loadbalancers.TLSCerts{Key: "key", Cert: "cert", Name: "tlsCert"}}
	fakeLoader := &tls.FakeTLSSecretLoader{FakeCerts: map[string]*loadbalancers.TLSCerts{"tlsCert": tlsCerts[0]}}
	lbc.tlsLoader = fakeLoader
	presharedCertName := "preSharedCert"
	ing := &extensions.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Annotations: map[string]string{annotations.PreSharedCertKey: presharedCertName},
		},
		Spec: extensions.IngressSpec{
			TLS: []extensions.IngressTLS{
				extensions.IngressTLS{
					SecretName: tlsCerts[0].Name,
				},
			},
		},
	}
	urlMap := &utils.GCEURLMap{}
	lbInfo, err := lbc.toRuntimeInfo(ing, urlMap)
	if err != nil {
		t.Fatalf("lbc.toRuntimeInfo() = err %v", err)
	}
	if lbInfo.TLSName != presharedCertName {
		t.Errorf("lbInfo.TLSName = %v, want %v", lbInfo.TLSName, presharedCertName)
	}
	if len(lbInfo.TLS) != 1 || lbInfo.TLS[0] != tlsCerts[0] {
		t.Errorf("lbInfo.TLS = %v, want %v", lbInfo.TLS, tlsCerts)
	}
}
