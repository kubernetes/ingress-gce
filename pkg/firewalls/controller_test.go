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
	"testing"
	"time"

	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	test "k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/context"
)

// newFirewallController creates a firewall controller.
func newFirewallController() *FirewallController {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	ctxConfig := context.ControllerContextConfig{
		Namespace:               api_v1.NamespaceAll,
		ResyncPeriod:            1 * time.Minute,
		DefaultBackendSvcPortID: test.DefaultBeSvcPort.ID,
	}

	ctx := context.NewControllerContext(kubeClient, backendConfigClient, fakeGCE, namer, ctxConfig)
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

	fwc.ctx.KubeClient.CoreV1().Services(defaultSvc.Namespace).Create(defaultSvc)
	fwc.ctx.ServiceInformer.GetIndexer().Add(defaultSvc)

	ing := test.NewIngress(types.NamespacedName{Name: "my-ingress", Namespace: "default"}, extensions.IngressSpec{})
	fwc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Create(ing)
	fwc.ctx.IngressInformer.GetIndexer().Add(ing)

	key, _ := utils.KeyFunc(queueKey)
	if err := fwc.sync(key); err != nil {
		t.Fatalf("fwc.sync() = %v, want nil", err)
	}

	// Verify a firewall rule was created.
	_, err := fwc.ctx.Cloud.GetFirewall(ruleName)
	if err != nil {
		t.Fatalf("cloud.GetFirewall(%v) = _, %v, want _, nil", ruleName, err)
	}

	fwc.ctx.KubeClient.ExtensionsV1beta1().Ingresses(ing.Namespace).Delete(ing.Name, &meta_v1.DeleteOptions{})
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
