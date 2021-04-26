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
	context2 "context"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/common/operator"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/instances"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

var (
	nodePortCounter = 30000
	clusterUID      = "aaaaa"
	fakeZone        = "zone-a"
)

// newLoadBalancerController create a loadbalancer controller.
func newLoadBalancerController() *LoadBalancerController {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	(fakeGCE.Compute().(*cloud.MockGCE)).MockGlobalForwardingRules.InsertHook = loadbalancers.InsertGlobalForwardingRuleHook
	namer := namer_util.NewNamer(clusterUID, "")

	stopCh := make(chan struct{})
	ctxConfig := context.ControllerContextConfig{
		Namespace:             api_v1.NamespaceAll,
		ResyncPeriod:          1 * time.Minute,
		DefaultBackendSvcPort: test.DefaultBeSvcPort,
		HealthCheckPath:       "/",
	}
	ctx := context.NewControllerContext(nil, kubeClient, backendConfigClient, nil, nil, nil, nil, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig)
	lbc := NewLoadBalancerController(ctx, stopCh)
	// TODO(rramkumar): Fix this so we don't have to override with our fake
	lbc.instancePool = instances.NewNodePool(instances.NewFakeInstanceGroups(sets.NewString(), namer), namer, &test.FakeRecorderSource{})
	lbc.l7Pool = loadbalancers.NewLoadBalancerPool(fakeGCE, namer, events.RecorderProducerMock{}, namer_util.NewFrontendNamerFactory(namer, ""))
	lbc.instancePool.Init(&instances.FakeZoneLister{Zones: []string{fakeZone}})

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

	lbc.ctx.KubeClient.CoreV1().Services(svc.Namespace).Create(context2.TODO(), svc, meta_v1.CreateOptions{})
	lbc.ctx.ServiceInformer.GetIndexer().Add(svc)
}

func addIngress(lbc *LoadBalancerController, ing *networkingv1.Ingress) {
	lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Create(context2.TODO(), ing, meta_v1.CreateOptions{})
	lbc.ctx.IngressInformer.GetIndexer().Add(ing)
}

func updateIngress(lbc *LoadBalancerController, ing *networkingv1.Ingress) {
	lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Update(context2.TODO(), ing, meta_v1.UpdateOptions{})
	lbc.ctx.IngressInformer.GetIndexer().Update(ing)
}

func setDeletionTimestamp(lbc *LoadBalancerController, ing *networkingv1.Ingress) {
	ts := meta_v1.NewTime(time.Now())
	ing.SetDeletionTimestamp(&ts)
	updateIngress(lbc, ing)
}

func deleteIngress(lbc *LoadBalancerController, ing *networkingv1.Ingress) {
	if len(ing.GetFinalizers()) == 0 {
		deleteIngressWithFinalizer(lbc, ing)
	}
}

func deleteIngressWithFinalizer(lbc *LoadBalancerController, ing *networkingv1.Ingress) {
	lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Delete(context2.TODO(), ing.Name, meta_v1.DeleteOptions{})
	lbc.ctx.IngressInformer.GetIndexer().Delete(ing)
}

// getKey returns the key for an ingress.
func getKey(ing *networkingv1.Ingress, t *testing.T) string {
	key, err := common.KeyFunc(ing)
	if err != nil {
		t.Fatalf("Unexpected error getting key for Ingress %v: %v", ing.Name, err)
	}
	return key
}

func backend(name string, port networkingv1.ServiceBackendPort) networkingv1.IngressBackend {
	return networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: name,
			Port: port,
		},
	}
}

// TestIngressSyncError asserts that `sync` will bubble an error when an ingress cannot be synced
// due to configuration problems.
func TestIngressSyncError(t *testing.T) {
	lbc := newLoadBalancerController()

	someBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &someBackend,
		})
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	err := lbc.sync(ingStoreKey)
	if err == nil {
		t.Fatalf("lbc.sync(%v) = nil, want error", ingStoreKey)
	}

	if !strings.Contains(err.Error(), someBackend.Service.Name) {
		t.Errorf("lbc.sync(%v) = %v, want error containing %q", ingStoreKey, err, someBackend.Service.Name)
	}
}

// TestNEGOnlyIngress asserts that `sync` will not create IG when there is only NEG backends for the ingress
func TestNEGOnlyIngress(t *testing.T) {
	lbc := newLoadBalancerController()

	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	negAnnotation := annotations.NegAnnotation{Ingress: true}
	svc.Annotations = map[string]string{
		annotations.NEGAnnotationKey: negAnnotation.String(),
	}
	addService(lbc, svc)
	someBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &someBackend,
		})
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	err := lbc.sync(ingStoreKey)
	if err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}

	ig, err := lbc.instancePool.Get(lbc.ctx.ClusterNamer.InstanceGroup(), fakeZone)
	if err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		t.Errorf("lbc.ctx.Cloud.ListInstanceGroups(%v) = %v, %v, want [], http.StatusNotFound", fakeZone, ig, err)
	}
}

// TestIngressCreateDeleteFinalizer asserts that `sync` will will not return an
// error for a good ingress config. It also tests garbage collection for
// Ingresses that need to be deleted, and keep the ones that don't, depending
// on whether Finalizer Adds and/or Removes are enabled.
func TestIngressCreateDeleteFinalizer(t *testing.T) {
	flagSaver := test.NewFlagSaver()
	flagSaver.Save(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	defer flagSaver.Reset(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	flagSaver.Save(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	defer flagSaver.Reset(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
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
			defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})

			for _, name := range tc.ingNames {
				ing := test.NewIngress(types.NamespacedName{Name: name, Namespace: "default"},
					networkingv1.IngressSpec{
						DefaultBackend: &defaultBackend,
					})
				addIngress(lbc, ing)

				ingStoreKey := getKey(ing, t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
				}

				updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})

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
				ing, _ := lbc.ctx.KubeClient.NetworkingV1().Ingresses("default").Get(context2.TODO(), name, meta_v1.GetOptions{})
				setDeletionTimestamp(lbc, ing)

				ingStoreKey := getKey(ing, t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
				}

				updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1().Ingresses("default").Get(context2.TODO(), name, meta_v1.GetOptions{})
				deleteIngress(lbc, updatedIng)

				updatedIng, _ = lbc.ctx.KubeClient.NetworkingV1().Ingresses("default").Get(context2.TODO(), name, meta_v1.GetOptions{})
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

				remainingIngresses, err := lbc.ctx.KubeClient.NetworkingV1().Ingresses("default").List(context2.TODO(), meta_v1.ListOptions{})
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
	flagSaver := test.NewFlagSaver()
	flagSaver.Save(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	defer flagSaver.Reset(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	flagSaver.Save(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	defer flagSaver.Reset(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	flags.F.FinalizerAdd = true
	flags.F.FinalizerRemove = true
	lbc := newLoadBalancerController()
	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	anns := map[string]string{annotations.IngressClassKey: "gce"}
	addService(lbc, svc)
	defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
		})
	ing.ObjectMeta.Annotations = anns
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}
	// Check if finalizer is added when an Ingress resource is created with finalizer enabled.
	updatedIng, err := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
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
	updatedIng, err = lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
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
	flagSaver := test.NewFlagSaver()
	flagSaver.Save(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	defer flagSaver.Reset(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	flagSaver.Save(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	defer flagSaver.Reset(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	flags.F.FinalizerAdd = true
	flags.F.FinalizerRemove = true
	lbc := newLoadBalancerController()
	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)
	defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
		})
	otherIng := test.NewIngress(types.NamespacedName{Name: "my-other-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
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
	ingSvcPorts := lbc.ToSvcPorts([]*networkingv1.Ingress{ing})
	otherIngSvcPorts := lbc.ToSvcPorts([]*networkingv1.Ingress{otherIng})
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

	remainingIngresses, err := lbc.ctx.KubeClient.NetworkingV1().Ingresses("default").List(context2.TODO(), meta_v1.ListOptions{})
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
	flagSaver := test.NewFlagSaver()
	flagSaver.Save(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	defer flagSaver.Reset(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	lbc := newLoadBalancerController()
	svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "namespace1"}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)
	defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "namespace1"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
		})
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}

	// Ensure that no finalizer is added.
	updatedIng, err := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
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
	updatedIng, err = lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
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
	defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
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
	updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
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

	defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
		})
	ing.ObjectMeta.Annotations = map[string]string{"kubernetes.io/ingress.class": "gce-multi-cluster"}
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = err %v", ingStoreKey, err)
	}

	// Check Ingress has annotations noting the instance group name.
	updatedIng, _ := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
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

	defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
		})
	mcIng := test.NewIngress(types.NamespacedName{Name: "my-mc-ingress", Namespace: "default"},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
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
	updatedMcIng, err := lbc.ctx.KubeClient.NetworkingV1().Ingresses(mcIng.Namespace).Get(context2.TODO(), mcIng.Name, meta_v1.GetOptions{})
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
	secretsMap := map[string]*api_v1.Secret{
		"tlsCert": &api_v1.Secret{
			ObjectMeta: meta_v1.ObjectMeta{
				Name: "tlsCert",
			},
			Data: map[string][]byte{
				api_v1.TLSCertKey:       []byte("cert"),
				api_v1.TLSPrivateKeyKey: []byte("key"),
			},
		},
	}
	tlsCerts := []*translator.TLSCerts{{Key: "key", Cert: "cert", Name: "tlsCert", CertHash: translator.GetCertHash("cert")}}

	for _, v := range secretsMap {
		lbc.ctx.KubeClient.CoreV1().Secrets("").Create(context2.TODO(), v, meta_v1.CreateOptions{})
	}

	presharedCertName := "preSharedCert"
	ing := &networkingv1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Annotations: map[string]string{annotations.PreSharedCertKey: presharedCertName},
		},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{
				{
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

	diff := cmp.Diff(tlsCerts[0], lbInfo.TLS[0])
	if len(lbInfo.TLS) != 1 || diff != "" {
		t.Errorf("got diff comparing tls certs (-want, +got) %v", diff)
	}
}

// TestIngressTagging asserts that appropriate finalizer that defines frontend naming scheme,
// is added to ingress being synced.
func TestIngressTagging(t *testing.T) {
	flagSaver := test.NewFlagSaver()
	flagSaver.Save(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	defer flagSaver.Reset(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	flagSaver.Save(test.EnableV2FrontendNamerFlag, &flags.F.EnableV2FrontendNamer)
	defer flagSaver.Reset(test.EnableV2FrontendNamerFlag, &flags.F.EnableV2FrontendNamer)
	testCases := []struct {
		enableFinalizerAdd bool
		enableV2Namer      bool
		vipExists          bool
		urlMapExists       bool
		expectedFinalizer  string
	}{
		{enableFinalizerAdd: false, expectedFinalizer: ""},
		{enableFinalizerAdd: true, enableV2Namer: false, expectedFinalizer: common.FinalizerKey},
		{enableFinalizerAdd: true, enableV2Namer: true, vipExists: false, expectedFinalizer: common.FinalizerKeyV2},
		{enableFinalizerAdd: true, enableV2Namer: true, vipExists: true, urlMapExists: false, expectedFinalizer: common.FinalizerKeyV2},
		{enableFinalizerAdd: true, enableV2Namer: true, vipExists: true, urlMapExists: true, expectedFinalizer: common.FinalizerKey},
	}

	for idx, tc := range testCases {
		var desc string
		switch idx {
		case 0:
			desc = fmt.Sprintf("enableFinalizerAdd %t", tc.enableFinalizerAdd)
		case 1:
			desc = fmt.Sprintf("enableFinalizerAdd %t enableV2Namer %t ", tc.enableFinalizerAdd, tc.enableV2Namer)
		case 2:
			desc = fmt.Sprintf("enableFinalizerAdd %t enableV2Namer %t vipExists %t", tc.enableFinalizerAdd, tc.enableV2Namer, tc.vipExists)
		default:
			desc = fmt.Sprintf("enableFinalizerAdd %t enableV2Namer %t vipExists %t urlMapExists %t", tc.enableFinalizerAdd, tc.enableV2Namer, tc.vipExists, tc.urlMapExists)
		}
		t.Run(desc, func(t *testing.T) {
			flags.F.FinalizerAdd = tc.enableFinalizerAdd
			flags.F.EnableV2FrontendNamer = tc.enableV2Namer

			lbc := newLoadBalancerController()
			svc := test.NewService(types.NamespacedName{Name: "my-service", Namespace: "default"}, api_v1.ServiceSpec{
				Type:  api_v1.ServiceTypeNodePort,
				Ports: []api_v1.ServicePort{{Port: 80}},
			})
			addService(lbc, svc)

			defaultBackend := backend("my-service", networkingv1.ServiceBackendPort{Number: 80})
			ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"},
				networkingv1.IngressSpec{
					DefaultBackend: &defaultBackend,
				})
			if tc.vipExists {
				ing.Status.LoadBalancer.Ingress = []api_v1.LoadBalancerIngress{{IP: "0.0.0.0"}}
			}
			addIngress(lbc, ing)
			// Create URL map if enabled.
			if tc.urlMapExists {
				lbName := lbc.ctx.ClusterNamer.LoadBalancer(common.IngressKeyFunc(ing))
				lbc.ctx.Cloud.CreateURLMap(&compute.UrlMap{Name: lbc.ctx.ClusterNamer.UrlMap(lbName)})
			}

			ingStoreKey := getKey(ing, t)
			if err := lbc.sync(ingStoreKey); err != nil {
				t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
			}

			updatedIng, err := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
			if err != nil {
				t.Fatalf("Get(%v) = %v, want nil", ingStoreKey, err)
			}
			ingFinalizers := updatedIng.GetFinalizers()
			// Verify that no finalizer is added when finalizer flag is disabled.
			if !flags.F.FinalizerAdd {
				if l := len(ingFinalizers); l != 0 {
					t.Fatalf("len(updatedIng.GetFinalizers()) =  %d, want 0; updatedIng = %+v", l, updatedIng)
				}
				return
			}
			// Verify that appropriate finalizer is added
			if l := len(ingFinalizers); l != 1 {
				t.Fatalf("len(updatedIng.GetFinalizers()) = %d, want 1; updatedIng = %+v", l, updatedIng)
			}
			if diff := cmp.Diff(tc.expectedFinalizer, ingFinalizers[0]); diff != "" {
				t.Fatalf("Got diff for Finalizer (-want +got):\n%s", diff)
			}
		})
	}
}

// TestGC asserts that GC workflow deletes multiple ingresses on a single sync
// if delete events for those ingresses are lost.
// Note that this workflow is valid only when finalizer is disabled.
func TestGCMultiple(t *testing.T) {
	flagSaver := test.NewFlagSaver()
	flagSaver.Save(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	defer flagSaver.Reset(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	flags.F.FinalizerRemove = true
	const namespace = "namespace"

	ings := []string{"v1-ing1", "v1-ing2", "v1-ing3", "v1-ing4"}
	expectedIngresses := []string{"namespace/v1-ing3", "namespace/v1-ing4"}
	// expected ingresses after creation.
	expectedIngressKeys := make([]string, 0)
	for _, ingName := range ings {
		expectedIngressKeys = append(expectedIngressKeys, fmt.Sprintf("%s/%s", namespace, ingName))
	}
	lbc := newLoadBalancerController()
	// Create ingresses and run sync on them.
	var updatedIngs []*networkingv1.Ingress
	for _, ing := range ings {
		updatedIngs = append(updatedIngs, ensureIngress(t, lbc, namespace, ing, namer_util.V1NamingScheme))
	}

	allIngressKeys := lbc.ctx.Ingresses().ListKeys()
	sort.Strings(allIngressKeys)
	if diff := cmp.Diff(expectedIngressKeys, allIngressKeys); diff != "" {
		t.Fatalf("Got diff for ingresses (-want +got):\n%s", diff)
	}

	// delete multiple ingresses during a single sync.
	timestamp := meta_v1.NewTime(time.Now())
	// Add deletion timestamp to mock ingress deletions.
	// Delete half of the ingresses.
	for i := 0; i < len(updatedIngs)/2; i++ {
		updatedIngs[i].SetDeletionTimestamp(&timestamp)
		updateIngress(lbc, updatedIngs[i])
	}
	// Sync on the last ingress.
	ingStoreKey := getKey(updatedIngs[len(updatedIngs)-1], t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}

	// Update ingress store with any potential changes made by controller.
	// This step is needed only because we use a mock controller setup.
	for _, ing := range updatedIngs {
		lbc.ctx.IngressInformer.GetIndexer().Update(getUpdatedIngress(t, lbc, ing))
	}

	// Assert that controller returns same ingresses as expected ingresses.
	// Controller sync removes finalizers from all deletion candidates.
	// Filter ingresses with finalizer for un-deleted ingresses.
	allIngresses := operator.Ingresses(lbc.ctx.Ingresses().List()).Filter(func(ing *networkingv1.Ingress) bool {
		return common.HasFinalizer(ing.ObjectMeta)
	}).AsList()
	allIngressKeys = common.ToIngressKeys(allIngresses)
	sort.Strings(allIngressKeys)
	if diff := cmp.Diff(expectedIngresses, allIngressKeys); diff != "" {
		t.Fatalf("Got diff for Ingresses after delete (-want +got):\n%s", diff)
	}
}

// TestGC asserts that GC workflow runs as expected during a controller sync.
func TestGC(t *testing.T) {
	flagSaver := test.NewFlagSaver()
	flagSaver.Save(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	defer flagSaver.Reset(test.FinalizerRemoveFlag, &flags.F.FinalizerRemove)
	flagSaver.Save(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	defer flagSaver.Reset(test.FinalizerAddFlag, &flags.F.FinalizerAdd)
	flagSaver.Save(test.EnableV2FrontendNamerFlag, &flags.F.EnableV2FrontendNamer)
	defer flagSaver.Reset(test.EnableV2FrontendNamerFlag, &flags.F.EnableV2FrontendNamer)
	flags.F.FinalizerRemove = true
	flags.F.FinalizerAdd = true
	flags.F.EnableV2FrontendNamer = true
	const namespace = "namespace"
	for _, tc := range []struct {
		desc              string
		v1IngressNames    []string
		v2IngressNames    []string
		expectedIngresses []string
	}{
		{
			desc:              "empty",
			v1IngressNames:    []string{},
			v2IngressNames:    []string{},
			expectedIngresses: []string{},
		},
		{
			desc:              "v1 ingresses only",
			v1IngressNames:    []string{"v1-ing1", "v1-ing2", "v1-ing3", "v1-ing4"},
			v2IngressNames:    []string{},
			expectedIngresses: []string{"namespace/v1-ing3", "namespace/v1-ing4"},
		},
		{
			desc:              "v2 ingresses only",
			v1IngressNames:    []string{},
			v2IngressNames:    []string{"v2-ing1", "v2-ing2", "v2-ing3", "v2-ing4"},
			expectedIngresses: []string{"namespace/v2-ing3", "namespace/v2-ing4"},
		},
		{
			desc:              "both ingresses",
			v1IngressNames:    []string{"v1-ing1", "v1-ing2", "v1-ing3", "v1-ing4"},
			v2IngressNames:    []string{"v2-ing1", "v2-ing2", "v2-ing3", "v2-ing4"},
			expectedIngresses: []string{"namespace/v1-ing3", "namespace/v1-ing4", "namespace/v2-ing3", "namespace/v2-ing4"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			// expected ingresses after creation.
			expectedIngressKeys := make([]string, 0)
			for _, ingName := range tc.v1IngressNames {
				expectedIngressKeys = append(expectedIngressKeys, fmt.Sprintf("%s/%s", namespace, ingName))
			}
			for _, ingName := range tc.v2IngressNames {
				expectedIngressKeys = append(expectedIngressKeys, fmt.Sprintf("%s/%s", namespace, ingName))
			}
			lbc := newLoadBalancerController()
			// Create ingresses and run sync on them.
			var v1Ingresses, v2Ingresses []*networkingv1.Ingress
			for _, ing := range tc.v1IngressNames {
				v1Ingresses = append(v1Ingresses, ensureIngress(t, lbc, namespace, ing, namer_util.V1NamingScheme))
			}
			for _, ing := range tc.v2IngressNames {
				v2Ingresses = append(v2Ingresses, ensureIngress(t, lbc, namespace, ing, namer_util.V2NamingScheme))
			}

			allIngressKeys := lbc.ctx.Ingresses().ListKeys()
			sort.Strings(allIngressKeys)
			if diff := cmp.Diff(expectedIngressKeys, allIngressKeys); diff != "" {
				t.Fatalf("Got diff for ingresses (-want +got):\n%s", diff)
			}

			// Delete half of the v1 ingresses.
			for i := 0; i < len(v1Ingresses)/2; i++ {
				// Add deletion stamp to indicate that ingress is deleted.
				timestamp := meta_v1.NewTime(time.Now())
				v1Ingresses[i].SetDeletionTimestamp(&timestamp)
				updateIngress(lbc, v1Ingresses[i])
				ingStoreKey := getKey(v1Ingresses[i], t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
				}
			}

			// Delete half of the v2 ingresses.
			for i := 0; i < len(v2Ingresses)/2; i++ {
				// Add deletion stamp to indicate that ingress is deleted.
				timestamp := meta_v1.NewTime(time.Now())
				v2Ingresses[i].SetDeletionTimestamp(&timestamp)
				updateIngress(lbc, v2Ingresses[i])
				ingStoreKey := getKey(v2Ingresses[i], t)
				if err := lbc.sync(ingStoreKey); err != nil {
					t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
				}
			}

			// Update ingress store with any potential changes made by controller.
			// This step is needed only because we use a mock controller setup.
			for _, ing := range v1Ingresses {
				lbc.ctx.IngressInformer.GetIndexer().Update(getUpdatedIngress(t, lbc, ing))
			}
			for _, ing := range v2Ingresses {
				lbc.ctx.IngressInformer.GetIndexer().Update(getUpdatedIngress(t, lbc, ing))
			}

			// Assert that controller returns same ingresses as expected ingresses.
			// Controller sync removes finalizers from all deletion candidates.
			// Filter ingresses with finalizer for un-deleted ingresses.
			allIngresses := operator.Ingresses(lbc.ctx.Ingresses().List()).Filter(func(ing *networkingv1.Ingress) bool {
				return common.HasFinalizer(ing.ObjectMeta)
			}).AsList()
			allIngressKeys = common.ToIngressKeys(allIngresses)
			sort.Strings(allIngressKeys)
			if diff := cmp.Diff(tc.expectedIngresses, allIngressKeys); diff != "" {
				t.Fatalf("Got diff for Ingresses after delete (-want +got):\n%s", diff)
			}
		})
	}
}

// ensureIngress creates an ingress and syncs it with ingress controller.
// This returns updated ingress after sync.
func ensureIngress(t *testing.T, lbc *LoadBalancerController, namespace, name string, scheme namer_util.Scheme) *networkingv1.Ingress {
	serviceName := fmt.Sprintf("service-for-%s", name)
	svc := test.NewService(types.NamespacedName{Name: serviceName, Namespace: namespace}, api_v1.ServiceSpec{
		Type:  api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{{Port: 80}},
	})
	addService(lbc, svc)

	defaultBackend := backend(serviceName, networkingv1.ServiceBackendPort{Number: 80})
	ing := test.NewIngress(types.NamespacedName{Name: name, Namespace: namespace},
		networkingv1.IngressSpec{
			DefaultBackend: &defaultBackend,
		})
	if scheme == namer_util.V1NamingScheme {
		ing.ObjectMeta.Finalizers = []string{common.FinalizerKey}
	} else if scheme == namer_util.V2NamingScheme {
		ing.ObjectMeta.Finalizers = []string{common.FinalizerKeyV2}
	}
	addIngress(lbc, ing)

	ingStoreKey := getKey(ing, t)
	if err := lbc.sync(ingStoreKey); err != nil {
		t.Fatalf("lbc.sync(%v) = %v, want nil", ingStoreKey, err)
	}

	return getUpdatedIngress(t, lbc, ing)
}

func getUpdatedIngress(t *testing.T, lbc *LoadBalancerController, ing *networkingv1.Ingress) *networkingv1.Ingress {
	updatedIng, err := lbc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Get(context2.TODO(), ing.Name, meta_v1.GetOptions{})
	if err != nil {
		t.Fatalf("lbc.ctx.KubeClient...Get(%v) = %v, want nil", getKey(ing, t), err)
	}
	return updatedIng
}
