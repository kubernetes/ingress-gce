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
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"k8s.io/klog/v2"

	firewallclient "github.com/GoogleCloudPlatform/gke-networking-api/client/gcpfirewall/clientset/versioned/fake"
	"github.com/google/go-cmp/cmp"
	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/cloud-provider-gcp/providers/gce"
	v1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/flags"
	test "k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/utils/ptr"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/context"
)

// newFirewallController creates a firewall controller.
func newFirewallController() (*FirewallController, error) {
	kubeClient := fake.NewSimpleClientset()
	backendConfigClient := backendconfigclient.NewSimpleClientset()
	firewallClient := firewallclient.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())

	ctxConfig := context.ControllerContextConfig{
		Namespace:             api_v1.NamespaceAll,
		ResyncPeriod:          1 * time.Minute,
		DefaultBackendSvcPort: test.DefaultBeSvcPort,
	}
	ctx, err := context.NewControllerContext(kubeClient, backendConfigClient, nil, firewallClient, nil, nil, nil, nil, nil, kubeClient /*kube client to be used for events*/, fakeGCE, defaultNamer, "" /*kubeSystemUID*/, ctxConfig, klog.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize controller context: %v", err)
	}
	fwc, err := NewFirewallController(ctx, []string{"30000-32767"}, false, false, true, make(chan struct{}), klog.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize firewall controller: %v", err)
	}
	fwc.hasSynced = func() bool { return true }

	return fwc, nil
}

// TestFirewallCreateDelete asserts that `sync` will ensure the L7 firewall with
// the correct ports. It also asserts that when no ingresses exist, that the
// firewall rule is deleted.
func TestFirewallCreateDelete(t *testing.T) {
	fwc, err := newFirewallController()
	if err != nil {
		t.Fatalf("failed to initialize firewall controller: %v", err)
	}

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
	_, err = fwc.ctx.Cloud.GetFirewall(ruleName)
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

func TestGetCustomHealthCheckPorts(t *testing.T) {
	// No t.Parallel().
	oldTHC := flags.F.EnableTransparentHealthChecks
	oldTHCPort := flags.F.THCPort
	defer func() {
		flags.F.EnableTransparentHealthChecks = oldTHC
		flags.F.THCPort = oldTHCPort
	}()
	const thcPort = 5678
	flags.F.THCPort = thcPort

	testCases := []struct {
		desc      string
		svcPorts  []utils.ServicePort
		enableTHC bool
		expect    []string
	}{
		{
			desc:     "One service port with custom port",
			svcPorts: []utils.ServicePort{{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: ptr.To(int64(8000))}}}}},
			expect:   []string{"8000"},
		},
		{
			desc: "Two service ports with custom port",
			svcPorts: []utils.ServicePort{{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: ptr.To(int64(8000))}}}},
				{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: ptr.To(int64(9000))}}}}},
			expect: []string{"8000", "9000"},
		},
		{
			desc: "Two service ports with custom port THC enabled",
			svcPorts: []utils.ServicePort{{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: ptr.To(int64(8000))}}}},
				{BackendConfig: &v1.BackendConfig{Spec: v1.BackendConfigSpec{HealthCheck: &v1.HealthCheckConfig{Port: ptr.To(int64(9000))}}}}},
			enableTHC: true,
			expect:    []string{"8000", "9000", strconv.FormatInt(thcPort, 10)},
		},
		{
			desc:   "No service ports",
			expect: nil,
		},
		{
			desc:      "No service ports THC enabled",
			enableTHC: true,
			expect:    []string{strconv.FormatInt(thcPort, 10)},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			flags.F.EnableTransparentHealthChecks = tc.enableTHC
			fwc, err := newFirewallController()
			if err != nil {
				t.Fatalf("failed to initialize firewall controller: %v", err)
			}
			result := fwc.getCustomHealthCheckPorts(tc.svcPorts)
			if diff := cmp.Diff(tc.expect, result); diff != "" {
				t.Errorf("unexpected diff of ports (-want +got): \n%s", diff)
			}
		})
	}
}

func TestLBSourceRanges(t *testing.T) {
	t.Parallel()
	defaultSourceRanges := gce.L7LoadBalancerSrcRanges()

	testCases := []struct {
		desc           string
		overrideRanges string
		want           []string
	}{
		{
			desc:           "Empty overrideRanges",
			overrideRanges: "",
			want:           defaultSourceRanges,
		},
		{
			desc:           "Single override range",
			overrideRanges: "10.0.0.0/8",
			want:           []string{"10.0.0.0/8"},
		},
		{
			desc:           "Multiple override ranges",
			overrideRanges: "10.0.0.0/8,192.168.0.0/16",
			want:           []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
		{
			desc:           "Multiple override ranges with spaces",
			overrideRanges: " 10.0.0.0/8 , 192.168.0.0/16 ",
			want:           []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
		{
			desc:           "IPv6 override range",
			overrideRanges: "2001:db8::/32",
			want:           []string{"2001:db8::/32"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			got, err := lbSourceRanges(klog.TODO(), tc.overrideRanges)
			if err != nil {
				t.Fatalf("lbSourceRanges(%q) returned error: %v", tc.overrideRanges, err)
			}
			sort.Strings(got)
			sort.Strings(tc.want)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("lbSourceRanges(%q) returned diff (-want +got):\\n%s", tc.overrideRanges, diff)
			}
		})
	}
}
