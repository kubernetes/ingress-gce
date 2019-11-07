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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/tls"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

var (
	nodePortCounter = 30000
	clusterUID      = "aaaaa"
)

// saveFinalizerFlags captures current value of finalizer flags and
// restore them after a test is finished.
type saveFinalizerFlags struct{ add, remove bool }

func (s *saveFinalizerFlags) save() {
	s.add = flags.F.FinalizerAdd
	s.remove = flags.F.FinalizerRemove
}
func (s *saveFinalizerFlags) reset() {
	flags.F.FinalizerAdd = s.add
	flags.F.FinalizerRemove = s.remove
}

// newLoadBalancerController create a loadbalancer controller.
func newLoadBalancerController() *LoadBalancerController {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	(fakeGCE.Compute().(*cloud.MockGCE)).MockGlobalForwardingRules.InsertHook = loadbalancers.InsertGlobalForwardingRuleHook
	namer := namer_util.NewNamer(clusterUID, "")

	stopCh := make(chan struct{})
	ctxConfig := context.ControllerContextConfig{
		Namespace:                     api_v1.NamespaceAll,
		ResyncPeriod:                  1 * time.Minute,
		DefaultBackendSvcPort:         test.DefaultBeSvcPort,
		HealthCheckPath:               "/",
		DefaultBackendHealthCheckPath: "/healthz",
	}

	ctx := context.NewControllerContext(nil, kubeClient, backendConfigClient, nil, fakeGCE, namer, ctxConfig)
	lbc := NewLoadBalancerController(ctx, stopCh)
	// TODO(rramkumar): Fix this so we don't have to override with our fake
	lbc.instancePool = instances.NewNodePool(instances.NewFakeInstanceGroups(sets.NewString(), namer), namer)
	lbc.l7Pool = loadbalancers.NewLoadBalancerPool(fakeGCE, namer, events.RecorderProducerMock{}, namer_util.NewFrontendNamerFactory(namer))
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

func addIngress(lbc *LoadBalancerController, ing *v1beta1.Ingress) {
	lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Create(ing)
	lbc.ctx.IngressInformer.GetIndexer().Add(ing)
}

func updateIngress(lbc *LoadBalancerController, ing *v1beta1.Ingress) {
	lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Update(ing)
	lbc.ctx.IngressInformer.GetIndexer().Update(ing)
}

func setDeletionTimestamp(lbc *LoadBalancerController, ing *v1beta1.Ingress) {
	ts := meta_v1.NewTime(time.Now())
	ing.SetDeletionTimestamp(&ts)
	updateIngress(lbc, ing)
}

func deleteIngress(lbc *LoadBalancerController, ing *v1beta1.Ingress) {
	if len(ing.GetFinalizers()) == 0 {
		lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Delete(ing.Name, &meta_v1.DeleteOptions{})
		lbc.ctx.IngressInformer.GetIndexer().Delete(ing)
	}
}

// getKey returns the key for an ingress.
func getKey(ing *v1beta1.Ingress, t *testing.T) string {
	key, err := common.KeyFunc(ing)
	if err != nil {
		t.Fatalf("Unexpected error getting key for Ingress %v: %v", ing.Name, err)
	}
	return key
}

func backend(name string, port intstr.IntOrString) v1beta1.IngressBackend {
	return v1beta1.IngressBackend{
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
		v1beta1.IngressSpec{
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
	var flagSaver saveFinalizerFlags
	flagSaver.save()
	defer flagSaver.reset()
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
					v1beta1.IngressSpec{
						Backend: &defaultBackend,
					})
				addIngress(lbc, ing)

				ingStoreKey := getKey(ing, t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
				}

				updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})

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
				ing, _ := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses("default").Get(name, meta_v1.GetOptions{})
				setDeletionTimestamp(lbc, ing)

				ingStoreKey := getKey(ing, t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
				}

				updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses("default").Get(name, meta_v1.GetOptions{})
				deleteIngress(lbc, updatedIng)

				updatedIng, _ = lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses("default").Get(name, meta_v1.GetOptions{})
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

				remainingIngresses, err := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses("default").List(meta_v1.ListOptions{})
				if err != nil {
					t.Fatalf("List() = %v, want nil", err)
				}

				expectedRemainingIngCount := len(tc.ingNames) - i - 1
				remainingIngCount := len(remainingIngresses.Items)
				if remainingIngCount != expectedRemainingIngCount {
					t.Errorf("List() = count %d, want %d; ingress count mismatch", remainingIngCount, expectedRemainingIngCount)
				}
			}
		})
	}
}

// TestIngressClassChangeWithFinalizer asserts that `sync` will not return an error for
// a good ingress config status is updated and LB is deleted after class change.
// Note: This test cannot be run in parallel as it stubs global flags.
func TestIngressClassChangeWithFinalizer(t *testing.T) {
	var flagSaver saveFinalizerFlags
	flagSaver.save()
	defer flagSaver.reset()
	flags.F.FinalizerAdd = true
	flags.F.FinalizerRemove = true
	lbc := newLoadBalancerController()
	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	anns := map[string]string{annotations.IngressClassKey: "gce"}
	addService(lbc, svc)
	defaultBackend := backend("my-service", intstr.FromInt(80))
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		v1beta1.IngressSpec{
			Backend: &defaultBackend,
		})
	ing.ObjectMeta.Annotations = anns
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}
	// Check if finalizer is added when an Ingress resource is created with finalizer enabled.
	updatedIng, err := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%v) = %v, want nil", ingStoreKey, err)
	}
	ingFinalizers := updatedIng.GetFinalizers()
	if len(ingFinalizers) != 1 || ingFinalizers[0] != common.FinalizerKey {
		t.Fatalf("updatedIng.GetFinalizers() = %+v, want [%s]; failed to add finalizer, updatedIng = %+v", ingFinalizers, common.FinalizerKey, updatedIng)
	}

	anns[annotations.IngressClassKey] = "new-class"
	updatedIng.ObjectMeta.Annotations = anns
	updateIngress(lbc, updatedIng)

	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}
	// Check if finalizer is removed after class changes.
	updatedIng, err = lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%v) = %v, want nil", ingStoreKey, err)
	}
	if l := len(updatedIng.GetFinalizers()); l != 0 {
		t.Fatalf("len(updatedIng.GetFinalizers()) =  %d, want 0; failed to remove finalizer, updatedIng = %+v", l, updatedIng)
	}
}

// TestIngressesWithSharedResourcesWithFinalizer asserts that `sync` does not return error when
// multiple ingresses with shared resources are added or deleted.
// Note: This test cannot be run in parallel as it stubs global flags.
func TestIngressesWithSharedResourcesWithFinalizer(t *testing.T) {
	var flagSaver saveFinalizerFlags
	flagSaver.save()
	defer flagSaver.reset()
	flags.F.FinalizerAdd = true
	flags.F.FinalizerRemove = true
	lbc := newLoadBalancerController()
	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)
	defaultBackend := backend("my-service", intstr.FromInt(80))
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		v1beta1.IngressSpec{
			Backend: &defaultBackend,
		})
	otherIng := test.NewIngress(types.NamespacedName{Name: "my-other-ingress", Namespace: "default"},
		v1beta1.IngressSpec{
			Backend: &defaultBackend,
		})
	addIngress(lbc, ing)
	addIngress(lbc, otherIng)

	ingStoreKey := getKey(ing, t)
	otherIngStoreKey := getKey(otherIng, t)
	if err1, err2 := lbc.sync(ingStoreKey), lbc.sync(otherIngStoreKey); err1 != nil || err2 != nil {
		if err1 != nil {
			t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err1)
		} else {
			t.Fatalf("lbc.sync(%v) = %v, want nil", otherIngStoreKey, err2)
		}
	}

	// Assert service ports are being shared.
	ingSvcPorts := lbc.ToSvcPorts([]*v1beta1.Ingress{ing})
	otherIngSvcPorts := lbc.ToSvcPorts([]*v1beta1.Ingress{otherIng})
	comparer := cmp.Comparer(func(a, b utils.ServicePort) bool {
		return reflect.DeepEqual(a, b)
	})
	if diff := cmp.Diff(ingSvcPorts, otherIngSvcPorts, comparer); diff != "" {
		t.Errorf("lbc.ToSVCPorts(_) mismatch (-want +got):\n%s", diff)
	}

	deleteIngress(lbc, ing)
	deleteIngress(lbc, otherIng)

	if err1, err2 := lbc.sync(ingStoreKey), lbc.sync(otherIngStoreKey); err1 != nil || err2 != nil {
		if err1 != nil {
			t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err1)
		} else {
			t.Fatalf("lbc.sync(%v) = %v, want nil", otherIngStoreKey, err2)
		}
	}

	remainingIngresses, err := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses("default").List(meta_v1.ListOptions{})
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}

	// assert if all the ingressesToCleanup deleted safely
	if l := len(remainingIngresses.Items); l != 0 {
		t.Fatalf("len(remainingIngresses.Items) = %d, want 0; failed to remove all the ingresses", l)
	}
}

// TestEnableFinalizer asserts that `sync` does not return error and finalizer is added
// to existing ingressesToCleanup when lbc is upgraded to enable finalizer.
// Note: This test cannot be run in parallel as it stubs global flags.
func TestEnableFinalizer(t *testing.T) {
	var flagSaver saveFinalizerFlags
	flagSaver.save()
	defer flagSaver.reset()
	lbc := newLoadBalancerController()
	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "namespace1"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)
	defaultBackend := backend("my-service", intstr.FromInt(80))
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "namespace1"},
		v1beta1.IngressSpec{
			Backend: &defaultBackend,
		})
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}

	// Ensure that no finalizer is added.
	updatedIng, err := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%v) = %v, want nil", ingStoreKey, err)
	}
	if l := len(updatedIng.GetFinalizers()); l != 0 {
		t.Fatalf("len(updatedIng.GetFinalizers()) =  %d, want 0; failed to remove finalizer, updatedIng = %+v", l, updatedIng)
	}

	// enable finalizer
	flags.F.FinalizerAdd = true

	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}

	// Check if finalizer is added after finalizer is enabled.
	updatedIng, err = lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%v) = %v, want nil", ingStoreKey, err)
	}
	ingFinalizers := updatedIng.GetFinalizers()
	if len(ingFinalizers) != 1 || ingFinalizers[0] != common.FinalizerKey {
		t.Fatalf("updatedIng.GetFinalizers() = %+v, want [%s]; failed to add finalizer, updatedIng = %+v", ingFinalizers, common.FinalizerKey, updatedIng)
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
		v1beta1.IngressSpec{
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
	updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
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
		v1beta1.IngressSpec{
			Backend: &defaultBackend,
		})
	ing.ObjectMeta.Annotations = map[string]string{"kubernetes.io/ingress.class": "gce-multi-cluster"}
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
	}

	// Check Ingress has annotations noting the instance group name.
	updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(ing.Namespace).Get(ing.Name, meta_v1.GetOptions{})
	igAnnotationKey := "ingress.gcp.kubernetes.io/instance-groups"
	wantVal := `[{"Name":"k8s-ig--aaaaa","Zone":"zone-a"}]`
	if val, ok := updatedIng.GetAnnotations()[igAnnotationKey]; !ok {
		t.Errorf("Ingress.Annotations does not contain key %q", igAnnotationKey)
	} else if val != wantVal {
		t.Errorf("Ingress.Annotation %q = %q, want %q", igAnnotationKey, val, wantVal)
	}
}

// TestMCIngressIG asserts that instances groups are deleted only after multi-cluster ingresses are cleaned up.
func TestMCIngressIG(t *testing.T) {
	lbc := newLoadBalancerController()

	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)

	defaultBackend := backend("my-service", intstr.FromInt(80))
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		v1beta1.IngressSpec{
			Backend: &defaultBackend,
		})
	mcIng := test.NewIngress(types.NamespacedName{Name: "my-mc-ingress", Namespace: "default"},
		v1beta1.IngressSpec{
			Backend: &defaultBackend,
		})
	mcIng.ObjectMeta.Annotations = map[string]string{"kubernetes.io/ingress.class": "gce-multi-cluster"}
	addIngress(lbc, ing)
	addIngress(lbc, mcIng)

	ingStoreKey := getKey(ing, t)
	mcIngStoreKey := getKey(mcIng, t)
	if err1, err2 := lbc.sync(ingStoreKey), lbc.sync(mcIngStoreKey); err1 != nil || err2 != nil {
		if err1 != nil {
			t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err1)
		} else {
			t.Fatalf("lbc.sync(%v) = %v, want nil", mcIngStoreKey, err2)
		}
	}

	// Check multi-cluster Ingress has annotations noting the instance group name.
	updatedMcIng, err := lbc.ctx.KubeClient.NetworkingV1beta1().Ingresses(mcIng.Namespace).Get(mcIng.Name, meta_v1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%v) = %v, want nil", mcIngStoreKey, err)
	}
	igAnnotationKey := annotations.InstanceGroupsAnnotationKey
	instanceGroupName := fmt.Sprintf("k8s-ig--%s", clusterUID)
	wantVal := fmt.Sprintf(`[{"Name":%q,"Zone":"zone-a"}]`, instanceGroupName)
	if val, ok := updatedMcIng.GetAnnotations()[igAnnotationKey]; !ok {
		t.Errorf("updatedMcIng.GetAnnotations()[%q]= (_, %v), want true; invalid key, updatedMcIng = %v", igAnnotationKey, ok, updatedMcIng)
	} else if diff := cmp.Diff(wantVal, val); diff != "" {
		t.Errorf("updatedMcIng.GetAnnotations()[%q] mismatch (-want +got):\n%s", igAnnotationKey, diff)
	}

	// Ensure that instance group exists.
	instanceGroups, err := lbc.instancePool.List()
	if err != nil {
		t.Errorf("lbc.instancePool.List() = _, %v, want nil", err)
	}
	if diff := cmp.Diff([]string{instanceGroupName}, instanceGroups); diff != "" {
		t.Errorf("lbc.instancePool.List()() mismatch (-want +got):\n%s", diff)
	}

	// Delete GCE ingress resource ing, ensure that instance group is not deleted.
	deleteIngress(lbc, ing)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}

	// Ensure that instance group still exists.
	instanceGroups, err = lbc.instancePool.List()
	if err != nil {
		t.Errorf("lbc.instancePool.List() = _, %v, want nil", err)
	}
	if diff := cmp.Diff([]string{instanceGroupName}, instanceGroups); diff != "" {
		t.Errorf("lbc.instancePool.List()() mismatch (-want +got):\n%s", diff)
	}

	// Delete GCE multi-cluster ingress mcIng and verify that instance group is deleted.
	deleteIngress(lbc, updatedMcIng)
	if err := lbc.sync(mcIngStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", mcIngStoreKey, err)
	}

	// Ensure that instance group is cleaned up.
	instanceGroups, err = lbc.instancePool.List()
	if err != nil {
		t.Errorf("lbc.instancePool.List() = _, %v, want nil", err)
	}
	var wantInstanceGroups []string
	if diff := cmp.Diff(wantInstanceGroups, instanceGroups); diff != "" {
		t.Errorf("lbc.instancePool.List()() mismatch (-want +got):\n%s", diff)
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
	ing := &v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Annotations: map[string]string{annotations.PreSharedCertKey: presharedCertName},
		},
		Spec: v1beta1.IngressSpec{
			TLS: []v1beta1.IngressTLS{
				v1beta1.IngressTLS{
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
