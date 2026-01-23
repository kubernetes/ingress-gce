/*
Copyright 2024 The Kubernetes Authors.

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

package operator

import (
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/cloud-provider-gcp/providers/gce"

	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/context"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/test"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	clusterUID = "kube-system-uid"

	testNamespace   = "test-namespace"
	testServiceName = "test-service"
	testIngressName = "test-ingress"
	testServicePort = int32(80)
)

func TestReferencesSvcNeg(t *testing.T) {
	t.Parallel()

	kubeClient := fake.NewSimpleClientset()
	gceClient := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
	namer := namer_util.NewNamer(clusterUID, "", klog.TODO())
	ctxConfig := context.ControllerContextConfig{
		Namespace:                     apiv1.NamespaceAll,
		ResyncPeriod:                  1 * time.Minute,
		DefaultBackendSvcPort:         test.DefaultBeSvcPort,
		HealthCheckPath:               "/",
		EnableIngressRegionalExternal: true,
	}
	ctx, err := context.NewControllerContext(kubeClient, nil, nil, nil, nil, nil, nil, nil, nil, kubeClient /*kube client to be used for events*/, gceClient, namer, "" /*kubeSystemUID*/, ctxConfig, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to initialize controller context: %v", err)
	}

	if err := addTestService(ctx); err != nil {
		t.Fatalf("Failed to add test service: %v", err)
	}
	testNegName := ctx.ClusterNamer.NEG(testNamespace, testServiceName, testServicePort)

	if err := addTestIngress(ctx); err != nil {
		t.Fatalf("Failed to add test ingress: %v", err)
	}
	ingOperator := Ingresses(ctx.Ingresses().List())

	testCases := []struct {
		desc                 string
		svcNegLabel          map[string]string
		expectedIngressCount int
	}{
		{
			desc: "Corresponds to a matching service",
			svcNegLabel: map[string]string{
				negtypes.NegCRServiceNameKey: testServiceName,
			},
			expectedIngressCount: 1,
		},
		{
			desc: "Corresponds to no service",
			svcNegLabel: map[string]string{
				negtypes.NegCRServiceNameKey: "random-service",
			},
			expectedIngressCount: 0,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			testServiceNeg := &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testNegName,
					Namespace: testNamespace,
					Labels:    tc.svcNegLabel,
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{},
			}

			gotIngresses := ingOperator.ReferencesSvcNeg(testServiceNeg, ctx.Services()).AsList()
			if len(gotIngresses) != tc.expectedIngressCount {
				t.Errorf("Got %d matching ingress, expected %d", len(gotIngresses), tc.expectedIngressCount)
			}
		})
	}

}

func addTestService(ctx *context.ControllerContext) error {
	testService := test.NewService(
		types.NamespacedName{
			Name:      testServiceName,
			Namespace: testNamespace,
		},
		apiv1.ServiceSpec{
			Type:  apiv1.ServiceTypeNodePort,
			Ports: []apiv1.ServicePort{{Port: testServicePort}},
		},
	)
	return ctx.ServiceInformer.GetIndexer().Add(testService)
}

func addTestIngress(ctx *context.ControllerContext) error {
	testIngressPath := v1.HTTPIngressPath{
		Path: "/",
		Backend: v1.IngressBackend{
			Service: &v1.IngressServiceBackend{
				Name: testServiceName,
				Port: v1.ServiceBackendPort{
					Number: testServicePort,
				},
			},
		},
	}
	testIngressSpec := v1.IngressSpec{
		Rules: []v1.IngressRule{{
			IngressRuleValue: v1.IngressRuleValue{
				HTTP: &v1.HTTPIngressRuleValue{
					Paths: []v1.HTTPIngressPath{testIngressPath},
				},
			},
		}},
	}
	testIngress := test.NewIngress(
		types.NamespacedName{
			Name:      testIngressName,
			Namespace: testNamespace,
		},
		testIngressSpec,
	)
	return ctx.IngressInformer.GetIndexer().Add(testIngress)
}
