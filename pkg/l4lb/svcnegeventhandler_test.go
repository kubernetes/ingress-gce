/*
Copyright 2025 The Kubernetes Authors.

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

package l4lb

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"k8s.io/ingress-gce/pkg/l4/annotations"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog/v2"
)

func TestAddSvcNEGEnqueuesService(t *testing.T) {
	subsettingILBService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testService",
			Namespace: "testNamespace",
			Annotations: map[string]string{
				annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
			},
			Finalizers: []string{
				common.ILBFinalizerV2,
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
		},
	}

	testCases := []struct {
		name           string
		svcNEG         *negv1beta1.ServiceNetworkEndpointGroup
		services       []*v1.Service
		expectEnqueues int
	}{
		{
			name: "Enqueue_service",
			svcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testService"},
					},
				},
			},
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 1,
		},
		{
			name: "Dont_enqueue_not_existent",
			svcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testOtherService"},
					},
				},
			},
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 0,
		},
		{
			name: "Dont_enqueue_owner_of_kind_other_than_service",
			svcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Ingress", Name: "testService"},
					},
				},
			},
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 0,
		},
		{
			name: "Dont_enqueue_not_handled_service",
			svcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testService"},
					},
				},
			},
			services: []*v1.Service{
				{ // legacy ILB
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testService",
						Namespace: "testNamespace",
						Annotations: map[string]string{
							annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
						},
						Finalizers: []string{
							common.LegacyILBFinalizer,
						},
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeLoadBalancer,
					},
				},
			},
			expectEnqueues: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kubeClient := fake.NewClientset()
			serviceInformer := informerv1.NewServiceInformer(kubeClient, metav1.NamespaceAll, 10*time.Minute, utils.NewNamespaceIndexer())

			queue := utils.NewPeriodicTaskQueueWithMultipleWorkers("l4netLB", "services", 1, func(s string) error {
				return nil
			}, klog.TODO())

			handler := &svcNEGEventHandler{
				logger:          klog.TODO(),
				ServiceInformer: serviceInformer,
				svcQueue:        queue,
				svcFilterFunc:   isSubsettingILBService,
			}
			for _, svc := range tc.services {
				serviceInformer.GetIndexer().Add(svc)
			}

			handler.OnAdd(tc.svcNEG, false)

			if queue.Len() != tc.expectEnqueues {
				t.Errorf("unexpected number of enqueues want=%d got=%d", tc.expectEnqueues, queue.Len())
			}
		})
	}
}

func TestUpdateSvcNEGEnqueuesService(t *testing.T) {
	subsettingILBService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testService",
			Namespace: "testNamespace",
			Annotations: map[string]string{
				annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
			},
			Finalizers: []string{
				common.ILBFinalizerV2,
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
		},
	}
	testSvcNEG := &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSvcNEG",
			Namespace: "testNamespace",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Service", Name: "testService"},
			},
		},
		Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
			NetworkEndpointGroups: []negv1beta1.NegObjectReference{
				{
					Id:                  "1",
					SelfLink:            "networkEndpointGroups/1",
					SubnetURL:           "subnet1",
					NetworkEndpointType: "GCE_VM_IP",
					State:               "ACTIVE",
				},
			},
		},
	}
	emptyTestSvcNEG := &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSvcNEG",
			Namespace: "testNamespace",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Service", Name: "testService"},
			},
		},
		Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
			NetworkEndpointGroups: []negv1beta1.NegObjectReference{},
		},
	}

	testCases := []struct {
		name           string
		svcNEG         *negv1beta1.ServiceNetworkEndpointGroup
		oldSvcNEG      *negv1beta1.ServiceNetworkEndpointGroup
		services       []*v1.Service
		expectEnqueues int
	}{
		{
			name:   "SvcNEG_unchanged_dont_enqueue",
			svcNEG: testSvcNEG,
			oldSvcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testService"},
					},
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []negv1beta1.NegObjectReference{
						{
							Id:                  "1",
							SelfLink:            "networkEndpointGroups/1",
							SubnetURL:           "subnet1",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "ACTIVE",
						},
					},
				},
			},
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 0,
		},
		{
			name:      "SvcNEG_changed_enqueue",
			svcNEG:    testSvcNEG,
			oldSvcNEG: emptyTestSvcNEG,
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 1,
		},
		{
			name: "SvcNEG_order_changes_but_is_the_same_dont_enqueue",
			svcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testService"},
					},
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []negv1beta1.NegObjectReference{
						{
							Id:                  "1",
							SelfLink:            "networkEndpointGroups/1",
							SubnetURL:           "subnet1",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "ACTIVE",
						},
						{
							Id:                  "2",
							SelfLink:            "networkEndpointGroups/2",
							SubnetURL:           "subnet2",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "ACTIVE",
						},
					},
				},
			},
			oldSvcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testService"},
					},
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []negv1beta1.NegObjectReference{
						{
							Id:                  "2",
							SelfLink:            "networkEndpointGroups/2",
							SubnetURL:           "subnet2",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "ACTIVE",
						},
						{
							Id:                  "1",
							SelfLink:            "networkEndpointGroups/1",
							SubnetURL:           "subnet1",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "ACTIVE",
						},
					},
				},
			},
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 0,
		},
		{
			name: "Dont_enqueue_not_existent_svc",
			svcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testOtherService"},
					},
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []negv1beta1.NegObjectReference{
						{
							Id:                  "1",
							SelfLink:            "networkEndpointGroups/1",
							SubnetURL:           "subnet1",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "ACTIVE",
						},
					},
				},
			},
			oldSvcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testOtherService"},
					},
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []negv1beta1.NegObjectReference{},
				},
			},
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 0,
		},
		{
			name:      "Dont_enqueue_not_handled_service",
			svcNEG:    testSvcNEG,
			oldSvcNEG: emptyTestSvcNEG,
			services: []*v1.Service{
				{ // legacy ILB
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testService",
						Namespace: "testNamespace",
						Annotations: map[string]string{
							annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
						},
						Finalizers: []string{
							common.LegacyILBFinalizer,
						},
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeLoadBalancer,
					},
				},
			},
			expectEnqueues: 0,
		},
		{
			name:   "Enqueue_on_NEG_status_change",
			svcNEG: testSvcNEG,
			oldSvcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testService"},
					},
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []negv1beta1.NegObjectReference{
						{
							Id:                  "1",
							SelfLink:            "networkEndpointGroups/1",
							SubnetURL:           "subnet1",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "INACTIVE",
						},
					},
				},
			},
			services: []*v1.Service{
				subsettingILBService,
			},
			expectEnqueues: 1,
		},
		// this case should not be happening really, since we don't expect the SvcNEG to have multiple owners,
		// but to make code more resilient, we include this case
		{
			name: "Enqueue multiple services",
			svcNEG: &negv1beta1.ServiceNetworkEndpointGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testSvcNEG",
					Namespace: "testNamespace",
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Service", Name: "testService"},
						{Kind: "Service", Name: "testOtherService"},
					},
				},
				Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
					NetworkEndpointGroups: []negv1beta1.NegObjectReference{
						{
							Id:                  "1",
							SelfLink:            "networkEndpointGroups/1",
							SubnetURL:           "subnet1",
							NetworkEndpointType: "GCE_VM_IP",
							State:               "ACTIVE",
						},
					},
				},
			},
			oldSvcNEG: emptyTestSvcNEG,
			services: []*v1.Service{
				subsettingILBService,
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "testOtherService",
						Namespace: "testNamespace",
						Annotations: map[string]string{
							annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
						},
						Finalizers: []string{
							common.ILBFinalizerV2,
						},
					},
					Spec: v1.ServiceSpec{
						Type: v1.ServiceTypeLoadBalancer,
					},
				},
			},
			expectEnqueues: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			kubeClient := fake.NewClientset()
			serviceInformer := informerv1.NewServiceInformer(kubeClient, metav1.NamespaceAll, 10*time.Minute, utils.NewNamespaceIndexer())

			queue := utils.NewPeriodicTaskQueueWithMultipleWorkers("l4netLB", "services", 1, func(s string) error {
				return nil
			}, klog.TODO())

			handler := &svcNEGEventHandler{
				logger:          klog.TODO(),
				ServiceInformer: serviceInformer,
				svcQueue:        queue,
				svcFilterFunc:   isSubsettingILBService,
			}
			for _, svc := range tc.services {
				serviceInformer.GetIndexer().Add(svc)
			}

			handler.OnUpdate(tc.oldSvcNEG, tc.svcNEG)

			if queue.Len() != tc.expectEnqueues {
				t.Errorf("unexpected number of enqueues want=%d got=%d", tc.expectEnqueues, queue.Len())
			}
		})
	}
}

func TestGetSvcOwnersOfSvcNEG(t *testing.T) {
	svcNEG := &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSvcNEG",
			Namespace: "testNamespace",
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Service", Name: "testService"},
				{Kind: "Ingress", Name: "testIngress"},
				{Kind: "Service", Name: "testService2"},
			},
		},
	}
	svcKeys := getSvcOwnersOfSvcNEG(svcNEG, klog.TODO())

	want := []string{
		"testNamespace/testService",
		"testNamespace/testService2",
	}
	if diff := cmp.Diff(want, svcKeys); diff != "" {
		t.Errorf("getSvcOwnersOfSvcNEG() invalid result, diff=%s (want-, got+)", diff)
	}

}

func TestIsSubsettingILBService(t *testing.T) {
	testCases := []struct {
		name       string
		service    *v1.Service
		wantResult bool
	}{
		{
			name: "Match_ILB_with_V2_finalizer",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testSubsetting",
					Annotations: map[string]string{
						annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
					},
					Finalizers: []string{
						common.ILBFinalizerV2,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			wantResult: true,
		},
		{
			name: "Dont_match_ILB_with_V1_finalizer",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testSubsetting",
					Annotations: map[string]string{
						annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
					},
					Finalizers: []string{
						common.LegacyILBFinalizer,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			wantResult: false,
		},
		{
			name: "Dont_match_NetLB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testSubsetting",
					Annotations: map[string]string{
						annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeExternal),
					},
					Finalizers: []string{
						common.ILBFinalizerV2,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			wantResult: false,
		},
		{
			name: "Dont_match_non_LB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testSubsetting",
					Annotations: map[string]string{
						annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
					},
					Finalizers: []string{
						common.ILBFinalizerV2,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeClusterIP,
				},
			},
			wantResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isSubsettingILBService(tc.service)
			if result != tc.wantResult {
				t.Errorf("isSubsettingILBService() unexpected result, want=%v, got=%v, svc=%+v", tc.wantResult, result, tc.service)
			}
		})
	}
}

func TestIsRBSNetLBServiceWithNEGs(t *testing.T) {
	testCases := []struct {
		name       string
		service    *v1.Service
		wantResult bool
	}{
		{
			name: "Match_NetLB_with_V2_finalizer",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Finalizers: []string{
						common.NetLBFinalizerV3,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			wantResult: true,
		},
		{
			name: "Dont_match_NetLB_with_V2_finalizer",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Finalizers: []string{
						common.NetLBFinalizerV2,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			wantResult: false,
		},
		{
			name: "Dont_match_ILB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Annotations: map[string]string{
						annotations.ServiceAnnotationLoadBalancerType: string(annotations.LBTypeInternal),
					},
					Finalizers: []string{
						common.NetLBFinalizerV2,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeLoadBalancer,
				},
			},
			wantResult: false,
		},
		{
			name: "Dont_match_non_LB",
			service: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test",
					Finalizers: []string{
						common.NetLBFinalizerV3,
					},
				},
				Spec: v1.ServiceSpec{
					Type: v1.ServiceTypeClusterIP,
				},
			},
			wantResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isRBSNetLBServiceWithNEGs(tc.service)
			if result != tc.wantResult {
				t.Errorf("isRBSNetLBServiceWithNEGs() unexpected result, want=%v, got=%v, svc=%+v", tc.wantResult, result, tc.service)
			}
		})
	}
}
