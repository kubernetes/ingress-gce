/*
Copyright 2026 The Kubernetes Authors.

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

package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

func TestStandaloneNEGLBSync(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"

	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)

	testCases := []struct {
		desc               string
		svc                *v1.Service
		frs                map[string]*composite.ForwardingRule
		expectIPs          []string
		expectEventReasons []string
		expectError        bool
	}{
		{
			desc: "Multiple forwarding rules, one missing, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc3",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName + ",missing-fr",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:          []string{frIP},
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
		},
		{
			desc: "Multiple forwarding rules, all missing, error",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc4",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "missing-fr1,missing-fr2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs:         map[string]*composite.ForwardingRule{},
			expectIPs:   nil,
			expectError: true,
		},
		{
			desc: "Multiple forwarding rules, multiple backend services, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc5",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName + ",fr2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
				"fr2": {
					Name:                "fr2",
					IPAddress:           "10.0.0.101",
					BackendService:      bsURL + "-2",
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:   []string{frIP, "10.0.0.101"},
			expectError: false,
		},
		{
			desc: "Multiple forwarding rules, same backend service, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc6",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName + ",fr2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
				"fr2": {
					Name:                "fr2",
					IPAddress:           "10.0.0.101",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:   []string{frIP, "10.0.0.101"},
			expectError: false,
		},
		{
			desc: "Global forwarding rule sync success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-global",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/global/forwardingRules/global-fr", project),
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"global-fr": {
					Name:                "global-fr",
					IPAddress:           "10.0.0.200",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Global,
					Version:             meta.VersionGA,
				},
			},
			expectIPs: []string{"10.0.0.200"},
		},
		{
			desc: "Failure when rule has INTERNAL scheme",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-internal-scheme",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "INTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
		},
		{
			desc: "Failure when rule protocol is ESP",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-esp-protocol",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "ESP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
		},
		{
			desc: "Success when rule protocol is L3_DEFAULT",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-l3-default",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "L3_DEFAULT",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs: []string{frIP},
		},
		{
			desc: "Missing forwarding rule annotation entirely",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-missing-annotation",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
		},
		{
			desc: "Forwarding rule annotation is empty",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-empty-annotation",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
		},
		{
			desc: "Forwarding rule annotation parses to nothing",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-parses-to-nothing",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: ",  , ",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
		},
		{
			desc: "Forwarding rule annotation has invalid URL",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-invalid-url",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "invalid/url/format",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
		},
		{
			desc: "Clear status ingress IP when Service type is not LoadBalancer",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-not-lb",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeClusterIP,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectError: false,
		},
		{
			desc: "Clear status ingress IP when CustomForwardingRuleKey annotation is missing",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-missing-annotation",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
		},
		{
			desc: "Clear status ingress IP when forwarding rule protocol is ESP (unusable)",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-esp",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "ESP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
		},
		{
			desc: "Clear status ingress IP when CustomForwardingRuleKey annotation parses to nothing",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-parses-to-nothing",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: ",  , ",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
		},
		{
			desc: "Clear status ingress IP when CustomForwardingRuleKey annotation has an invalid URL",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-invalid-url",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "invalid/url/format",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset()
			fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
			namer := namer.NewNamer("cluster-uid", "firewall-name", klog.TODO())

			// Populate fakeGCE client with forwarding rules defined in each test case
			for name, fr := range tc.frs {
				key, err := composite.CreateKey(fakeGCE, name, fr.Scope)
				if err != nil {
					t.Fatalf("Failed to create key for forwarding rule %s: %v", name, err)
				}
				err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to create forwarding rule %s: %v", name, err)
				}
			}

			stopCh := make(chan struct{})
			defer close(stopCh)

			ctxConfig := ingctx.ControllerContextConfig{Namespace: v1.NamespaceAll}
			c, err := ingctx.NewControllerContext(kubeClient, nil, nil, nil, nil, nil, nil, nil, nil, kubeClient, fakeGCE, namer, "", ctxConfig, klog.TODO())
			if err != nil {
				t.Fatalf("Failed to create controller context: %v", err)
			}

			lc := NewStandaloneNEGLBController(c, stopCh, klog.TODO())

			// Add service to informer
			c.ServiceInformer.GetIndexer().Add(tc.svc)
			// Also add to fake kube client so updateStatus works
			kubeClient.CoreV1().Services(tc.svc.Namespace).Create(context.TODO(), tc.svc, metav1.CreateOptions{})

			key := tc.svc.Namespace + "/" + tc.svc.Name
			err = lc.sync(key)
			if (err != nil) != tc.expectError {
				t.Errorf("sync() error = %v, expectError %v", err, tc.expectError)
			}

			updatedSvc, _ := kubeClient.CoreV1().Services(tc.svc.Namespace).Get(context.TODO(), tc.svc.Name, metav1.GetOptions{})

			if len(updatedSvc.Status.LoadBalancer.Ingress) != len(tc.expectIPs) {
				t.Errorf("Expected %d ingress IPs, got %d", len(tc.expectIPs), len(updatedSvc.Status.LoadBalancer.Ingress))
			} else {
				for i, ip := range tc.expectIPs {
					if updatedSvc.Status.LoadBalancer.Ingress[i].IP != ip {
						t.Errorf("Expected IP %s, got %v", ip, updatedSvc.Status.LoadBalancer.Ingress[i].IP)
					}
				}
			}

			if len(tc.expectEventReasons) > 0 {
				for _, expectedReason := range tc.expectEventReasons {
					expectedReason := expectedReason
					err := wait.PollUntilContextTimeout(context.Background(), 10*time.Millisecond, 5*time.Second, true,
						func(ctx context.Context) (bool, error) {
							events, err := kubeClient.CoreV1().Events(tc.svc.Namespace).List(ctx, metav1.ListOptions{})
							if err != nil {
								return false, err
							}
							for _, e := range events.Items {
								if e.Reason == expectedReason {
									return true, nil
								}
							}
							return false, nil
						})
					if err != nil {
						t.Errorf("Expected %s event, but none found within timeout", expectedReason)
					}
				}
			}
		})
	}
}

func TestValidateForwardingRule(t *testing.T) {
	testCases := []struct {
		desc           string
		fr             *composite.ForwardingRule
		frName         string
		expectError    bool
		expectErrorMsg string
	}{
		{
			desc: "Valid TCP rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "TCP",
			},
			frName:      "valid-tcp",
			expectError: false,
		},
		{
			desc: "Valid UDP rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "UDP",
			},
			frName:      "valid-udp",
			expectError: false,
		},
		{
			desc: "Valid L3_DEFAULT rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "L3_DEFAULT",
			},
			frName:      "valid-l3-default",
			expectError: false,
		},
		{
			desc: "Internal scheme not supported",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "INTERNAL",
				IPProtocol:          "TCP",
			},
			frName:         "internal-scheme",
			expectError:    true,
			expectErrorMsg: "forwarding rule internal-scheme has unsupported load balancing scheme: INTERNAL, supported schemes are: EXTERNAL, EXTERNAL_PASSTHROUGH",
		},
		{
			desc: "Valid EXTERNAL_PASSTHROUGH rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
			},
			frName:      "valid-ext-passthrough",
			expectError: false,
		},
		{
			desc: "Unsupported scheme",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "INTERNAL_SELF_MANAGED",
				IPProtocol:          "TCP",
			},
			frName:         "invalid-scheme",
			expectError:    true,
			expectErrorMsg: "forwarding rule invalid-scheme has unsupported load balancing scheme: INTERNAL_SELF_MANAGED, supported schemes are: EXTERNAL, EXTERNAL_PASSTHROUGH",
		},
		{
			desc: "Unsupported protocol",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "ESP",
			},
			frName:         "invalid-protocol",
			expectError:    true,
			expectErrorMsg: "forwarding rule invalid-protocol has unsupported protocol: ESP, supported protocols are: TCP, UDP, L3_DEFAULT",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			err := validateForwardingRule(tc.fr, tc.frName)
			if (err != nil) != tc.expectError {
				t.Errorf("validateForwardingRule() error = %v, expectError = %v", err, tc.expectError)
			}
			if err != nil && tc.expectErrorMsg != "" && err.Error() != tc.expectErrorMsg {
				t.Errorf("validateForwardingRule() error message = %q, expected = %q", err.Error(), tc.expectErrorMsg)
			}
		})
	}
}

func setupControllerContext(t *testing.T) (*fake.Clientset, *gce.Cloud, *StandaloneNEGLBController, chan struct{}) {
	kubeClient := fake.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
	namer := namer.NewNamer("cluster-uid", "firewall-name", klog.TODO())

	stopCh := make(chan struct{})

	ctxConfig := ingctx.ControllerContextConfig{Namespace: v1.NamespaceAll}
	c, err := ingctx.NewControllerContext(kubeClient, nil, nil, nil, nil, nil, nil, nil, nil, kubeClient, fakeGCE, namer, "", ctxConfig, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create controller context: %v", err)
	}

	lc := NewStandaloneNEGLBController(c, stopCh, klog.TODO())
	return kubeClient, fakeGCE, lc, stopCh
}

func TestStandaloneNEGLBControllerMetrics_Success(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"
	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a valid forwarding rule
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}
	fr := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           frIP,
		BackendService:      bsURL,
		LoadBalancingScheme: "EXTERNAL",
		IPProtocol:          "TCP",
		Scope:               meta.Regional,
		Version:             meta.VersionGA,
	}
	err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create forwarding rule: %v", err)
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	// Add service to informer and fake kube client
	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	err = lc.sync(svcKey)
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}

	state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}
	if state.Status != l4metrics.StatusSuccess {
		t.Errorf("Expected status %s, got %s", l4metrics.StatusSuccess, state.Status)
	}
	if !state.LBSchemeExternal {
		t.Errorf("Expected LBSchemeExternal to be true")
	}
}

func TestStandaloneNEGLBControllerMetrics_UserError(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"
	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a forwarding rule with invalid scheme
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}
	fr := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           frIP,
		BackendService:      bsURL,
		LoadBalancingScheme: "INTERNAL_SELF_MANAGED", // Invalid
		IPProtocol:          "TCP",
		Scope:               meta.Regional,
		Version:             meta.VersionGA,
	}
	err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create forwarding rule: %v", err)
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	err = lc.sync(svcKey)
	if err == nil {
		t.Fatalf("Expected sync to fail")
	}

	state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}
	if state.Status != l4metrics.StatusUserError {
		t.Errorf("Expected status %s, got %s", l4metrics.StatusUserError, state.Status)
	}
}

func TestStandaloneNEGLBControllerMetrics_SystemError(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a valid forwarding rule key
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}

	// Inject GCE error
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	if mockGCE.MockForwardingRules.GetError == nil {
		mockGCE.MockForwardingRules.GetError = make(map[meta.Key]error)
	}
	mockGCE.MockForwardingRules.GetError[*key] = fmt.Errorf("internal GCE error")
	defer delete(mockGCE.MockForwardingRules.GetError, *key)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	err = lc.sync(svcKey)
	if err == nil {
		t.Fatalf("Expected sync to fail due to GCE error")
	}

	state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}
	if state.Status != l4metrics.StatusError {
		t.Errorf("Expected status %s, got %s", l4metrics.StatusError, state.Status)
	}
	if state.FirstSyncErrorTime == nil {
		t.Errorf("Expected FirstSyncErrorTime to be set for system error")
	}
}

func TestStandaloneNEGLBControllerMetrics_Deletion(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"
	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a valid forwarding rule
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}
	fr := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           frIP,
		BackendService:      bsURL,
		LoadBalancingScheme: "EXTERNAL",
		IPProtocol:          "TCP",
		Scope:               meta.Regional,
		Version:             meta.VersionGA,
	}
	err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create forwarding rule: %v", err)
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	// Add service to informer and fake kube client
	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	// Initial sync to establish state in metrics
	err = lc.sync(svcKey)
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}
	_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}

	// Act - Deletion
	err = kubeClient.CoreV1().Services(svc.Namespace).Delete(context.TODO(), svc.Name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}
	lc.ctx.ServiceInformer.GetIndexer().Delete(svc)

	err = lc.sync(svcKey)
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}

	// Assert
	_, ok = lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if ok {
		t.Errorf("Expected service %s to be removed from metrics map", svcKey)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Add(t *testing.T) {
	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: new(annotations.StandalonePassthroughNegLoadBalancerClass),
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Act
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Assert - Wait for metrics to be updated (should be UserError because no annotation)
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok && state.Status == l4metrics.StatusUserError, nil
	})
	if err != nil {
		t.Errorf("Failed to verify AddFunc via metrics: %v", err)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Update(t *testing.T) {
	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: new(annotations.StandalonePassthroughNegLoadBalancerClass),
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Setup initial state (service exists and is enqueued/processed once)
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	// Wait for initial metrics to be created
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok, nil
	})
	if err != nil {
		t.Fatalf("Failed to establish initial metrics state: %v", err)
	}

	// Act - Modify to not match shouldProcess (remove load balancer class)
	currentSvc, err := kubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get service: %v", err)
	}
	currentSvc.Spec.LoadBalancerClass = nil
	_, err = kubeClient.CoreV1().Services(svc.Namespace).Update(context.TODO(), currentSvc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update service: %v", err)
	}

	// Assert - Wait for metrics to be deleted
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return !ok, nil
	})
	if err != nil {
		t.Errorf("Failed to verify UpdateFunc (non-matching) via metrics: %v", err)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Delete(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass

	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Setup initial state
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	// Wait for initial metrics to be created
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok, nil
	})
	if err != nil {
		t.Fatalf("Failed to establish initial metrics state: %v", err)
	}

	// Act - Delete service
	err = kubeClient.CoreV1().Services(svc.Namespace).Delete(context.TODO(), svc.Name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}

	// Assert - Wait for metrics to be deleted
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return !ok, nil
	})
	if err != nil {
		t.Errorf("Failed to verify DeleteFunc via metrics: %v", err)
	}
}
