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

package firewalls

import (
	context2 "context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	v1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	test "k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/legacy-cloud-providers/gce"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/context"
)

// newFirewallController creates a firewall controller.
func newFirewallController() *FirewallController {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	ctxConfig := context.ControllerContextConfig{
		Namespace:             api_v1.NamespaceAll,
		ResyncPeriod:          1 * time.Minute,
		DefaultBackendSvcPort: test.DefaultBeSvcPort,
	}

	ctx := context.NewControllerContext(nil, kubeClient, backendConfigClient, nil, nil, nil, nil, fakeGCE, defaultNamer, "" /*kubeSystemUID*/, ctxConfig)
	fwc := NewFirewallController(ctx, []string{"30000-32767"})
	fwc.hasSynced = func() bool { return true }

	return fwc
}

// TestFirewallCreateDelete asserts that `sync` will ensure the L7 firewall with
// the correct ports. It also asserts that when no ingresses exist, that the
// firewall rule is deleted.
func TestFirewallCreateDelete(t *testing.T) {
	fwc := newFirewallController()

	// Create the default-backend service.
	defaultSvc := test.NewService(test.DefaultBeSvcPort.ID.Service, api_v1.ServiceSpec{
		Type: api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{
			{
				Name:     "http",
				Port:     80,
				NodePort: 30000,
			},
		},
	})

	fwc.ctx.KubeClient.CoreV1().Services(defaultSvc.Namespace).Create(context2.TODO(), defaultSvc, meta_v1.CreateOptions{})
	fwc.ctx.ServiceInformer.GetIndexer().Add(defaultSvc)

	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, networkingv1.IngressSpec{})
	fwc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Create(context2.TODO(), ing, meta_v1.CreateOptions{})
	fwc.ctx.IngressInformer.GetIndexer().Add(ing)

	key, _ := common.KeyFunc(queueKey)
	if err := fwc.sync(key); err != nil {
		t.Fatalf("fwc.sync() = %v, want nil", err)
	}

	// Verify a firewall rule was created.
	_, err := fwc.ctx.Cloud.GetFirewall(ruleName)
	if err != nil {
		t.Fatalf("cloud.GetFirewall(%v) = _, %v, want _, nil", ruleName, err)
	}

	fwc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Delete(context2.TODO(), ing.Name, meta_v1.DeleteOptions{})
	fwc.ctx.IngressInformer.GetIndexer().Delete(ing)

	if err := fwc.sync(key); err != nil {
		t.Fatalf("fwc.sync() = %v, want nil", err)
	}

	// Verify the firewall rule was deleted.
	_, err = fwc.ctx.Cloud.GetFirewall(ruleName)
	if !utils.IsNotFoundError(err) {
		t.Fatalf("cloud.GetFirewall(%v) = _, %v, want _, 404 error", ruleName, err)
	}
}

// TestFirewallEmptyPortList assets that a firewall rule will be deleted
// if the port list is empty. An empty port list should be impossible, except
// when it is a bug
func TestFirewallEmptyPortList(t *testing.T) {
	fwc := newFirewallController()

	// Create the default-backend service.
	testSvc := test.NewService(types.NamespacedName{Namespace: "default", Name: "service-name"}, api_v1.ServiceSpec{
		Type: api_v1.ServiceTypeNodePort,
		Ports: []api_v1.ServicePort{
			{
				Name:     "http",
				Port:     80,
				NodePort: 30000,
			},
		},
	})

	fwc.ctx.KubeClient.CoreV1().Services(testSvc.Namespace).Create(context2.TODO(), testSvc, meta_v1.CreateOptions{})
	fwc.ctx.ServiceInformer.GetIndexer().Add(testSvc)

	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, networkingv1.IngressSpec{
		Rules: []networkingv1.IngressRule{
			{
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: "/",
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "service-name",
										Port: networkingv1.ServiceBackendPort{
											Number: int32(80),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	fwc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Create(context2.TODO(), ing, meta_v1.CreateOptions{})
	fwc.ctx.IngressInformer.GetIndexer().Add(ing)

	key, _ := common.KeyFunc(queueKey)
	if err := fwc.sync(key); err != nil {
		t.Fatalf("fwc.sync() = %v, want nil", err)
	}

	// Verify a firewall rule was created.
	_, err := fwc.ctx.Cloud.GetFirewall(ruleName)
	if err != nil {
		t.Fatalf("cloud.GetFirewall(%v) = _, %v, want _, nil", ruleName, err)
	}

	// delete the ingress rules
	ing.Spec.Rules = []networkingv1.IngressRule{}
	fwc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Update(context2.TODO(), ing, meta_v1.UpdateOptions{})
	fwc.ctx.IngressInformer.GetIndexer().Update(ing)

	if err := fwc.sync(key); err != nil {
		t.Fatalf("fwc.sync() = %v, want nil", err)
	}

	// Verify the firewall rule was deleted.
	_, err = fwc.ctx.Cloud.GetFirewall(ruleName)
	if !utils.IsNotFoundError(err) {
		t.Fatalf("cloud.GetFirewall(%v) = _, %v, want _, 404 error", ruleName, err)
	}
}

// TestFirewallNoneExistingService asserts that a an empty firewall rule 
// is not created whe the port list is empty
func TestFirewallNoneExistingService(t *testing.T) {
	fwc := newFirewallController()

	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, networkingv1.IngressSpec{
		Rules: []networkingv1.IngressRule{
			{
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{
							{
								Path: "/",
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "service-name",
										Port: networkingv1.ServiceBackendPort{
											Number: int32(80),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	fwc.ctx.KubeClient.NetworkingV1().Ingresses(ing.Namespace).Create(context2.TODO(), ing, meta_v1.CreateOptions{})
	fwc.ctx.IngressInformer.GetIndexer().Add(ing)

	key, _ := common.KeyFunc(queueKey)
	if err := fwc.sync(key); err != nil {
		t.Fatalf("fwc.sync() = %v, want nil", err)
	}

	// Verify the firewall rule was not created.
	_, err := fwc.ctx.Cloud.GetFirewall(ruleName)
	if !utils.IsNotFoundError(err) {
		t.Fatalf("cloud.GetFirewall(%v) = _, %v, want _, 404 error", ruleName, err)
	}
}

func TestGetCustomHealthCheckPorts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		desc     string
		svcPorts []utils.ServicePort
		expect   []string
	}{
		{
			desc:     "One service port with custom port",
			svcPorts: []utils.ServicePort{utils.ServicePort{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: utils.NewInt64Pointer(8000)}}}}},
			expect:   []string{"8000"},
		},
		{
			desc: "Two service ports with custom port",
			svcPorts: []utils.ServicePort{utils.ServicePort{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: utils.NewInt64Pointer(8000)}}}},
				utils.ServicePort{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: utils.NewInt64Pointer(9000)}}}}},
			expect: []string{"8000", "9000"},
		},
		{
			desc:   "No service ports",
			expect: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fwc := newFirewallController()
			result := fwc.getCustomHealthCheckPorts(tc.svcPorts)
			if diff := cmp.Diff(tc.expect, result); diff != "" {
				t.Errorf("unexpected diff of ports (-want +got): \n%s", diff)
			}
		})
	}
}
