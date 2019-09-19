/*
Copyright 2019 The Kubernetes Authors.

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

package readiness

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/context"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	clusterID = "cluster-uid"
)

// fakeLookUp implements LookUp interface
type fakeLookUp struct {
	readinessGateEnabled     bool
	readinessGateEnabledNegs []string
}

func (f *fakeLookUp) ReadinessGateEnabledNegs(namespace string, labels map[string]string) []string {
	return f.readinessGateEnabledNegs
}

// ReadinessGateEnabled returns true if the NEG requires readiness feedback
func (f *fakeLookUp) ReadinessGateEnabled(syncerKey negtypes.NegSyncerKey) bool {
	return f.readinessGateEnabled
}

func fakeContext() *context.ControllerContext {
	kubeClient := fake.NewSimpleClientset()
	namer := namer_util.NewNamer(clusterID, "")
	ctxConfig := context.ControllerContextConfig{
		Namespace:    apiv1.NamespaceAll,
		ResyncPeriod: 1 * time.Second,
	}
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	negtypes.MockNetworkEndpointAPIs(fakeGCE)
	context := context.NewControllerContext(kubeClient, nil, nil, nil, fakeGCE, namer, ctxConfig)
	return context
}

func newTestReadinessReflector(cc *context.ControllerContext) *readinessReflector {
	reflector := NewReadinessReflector(cc, &fakeLookUp{})
	ret := reflector.(*readinessReflector)
	return ret
}

func TestSyncPod(t *testing.T) {
	fakeContext := fakeContext()
	testReadinessReflector := newTestReadinessReflector(fakeContext)
	client := fakeContext.KubeClient
	podLister := testReadinessReflector.podLister
	testlookUp := testReadinessReflector.lookup.(*fakeLookUp)
	podName := "pod1"
	fakeClock := clock.NewFakeClock(time.Now())
	testReadinessReflector.clock = fakeClock
	now := metav1.NewTime(fakeClock.Now()).Rfc3339Copy()

	for _, tc := range []struct {
		desc         string
		mutateState  func()
		inputKey     string
		inputNeg     string
		expectExists bool
		expectPod    *v1.Pod
	}{
		{
			desc:        "empty input",
			mutateState: func() {},
		},
		{
			desc: "no need to update when pod does not have neg readiness gate",
			mutateState: func() {
				pod := generatePod(testNamespace, podName, false, true, true)
				podLister.Add(pod)
				client.CoreV1().Pods(testNamespace).Create(pod)
			},
			inputKey:     keyFunc(testNamespace, podName),
			inputNeg:     "",
			expectExists: true,
			expectPod:    generatePod(testNamespace, podName, false, true, true),
		},
		{
			desc: "no need to update 2 when pod already has neg condition status == true",
			mutateState: func() {
				pod := generatePod(testNamespace, podName, true, true, true)
				podLister.Update(pod)
				client.CoreV1().Pods(testNamespace).Update(pod)
			},
			inputKey:     keyFunc(testNamespace, podName),
			inputNeg:     "",
			expectExists: true,
			expectPod:    generatePod(testNamespace, podName, true, true, true),
		},
		{
			desc: "need to update pod but there is no Negs associated",
			mutateState: func() {
				pod := generatePod(testNamespace, podName, true, false, false)
				podLister.Update(pod)
				client.CoreV1().Pods(testNamespace).Update(pod)
			},
			inputKey:     keyFunc(testNamespace, podName),
			inputNeg:     "",
			expectExists: true,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      podName,
				},
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{ConditionType: shared.NegReadinessGate},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:    shared.NegReadinessGate,
							Status:  v1.ConditionTrue,
							Reason:  negReadyReason,
							Message: fmt.Sprintf("Pod does not belong to any NEG. Marking condition %q to True.", shared.NegReadinessGate),
						},
					},
				},
			},
		},
		{
			desc: "need to update pod: there is NEGs associated but pod is not healthy",
			mutateState: func() {
				pod := generatePod(testNamespace, podName, true, false, false)
				pod.CreationTimestamp = now
				podLister.Update(pod)
				client.CoreV1().Pods(testNamespace).Update(pod)
				testlookUp.readinessGateEnabledNegs = []string{"neg1", "neg2"}
			},
			inputKey:     keyFunc(testNamespace, podName),
			inputNeg:     "",
			expectExists: true,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         testNamespace,
					Name:              podName,
					CreationTimestamp: now,
				},
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{ConditionType: shared.NegReadinessGate},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:    shared.NegReadinessGate,
							Reason:  negNotReadyReason,
							Message: fmt.Sprintf("Waiting for pod to become healthy in at least one of the NEG(s): %v", []string{"neg1", "neg2"}),
						},
					},
				},
			},
		},
		{
			desc: "need to update pod: pod is healthy in a NEG",
			mutateState: func() {
				pod := generatePod(testNamespace, podName, true, false, false)
				podLister.Update(pod)
				client.CoreV1().Pods(testNamespace).Update(pod)
				testlookUp.readinessGateEnabledNegs = []string{"neg1", "neg2"}
			},
			inputKey:     keyFunc(testNamespace, podName),
			inputNeg:     "neg1",
			expectExists: true,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNamespace,
					Name:      podName,
				},
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{ConditionType: shared.NegReadinessGate},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:    shared.NegReadinessGate,
							Reason:  negReadyReason,
							Status:  v1.ConditionTrue,
							Message: fmt.Sprintf("Pod has become Healthy in NEG %q. Marking condition %q to True.", "neg1", shared.NegReadinessGate),
						},
					},
				},
			},
		},
		{
			desc: "timeout waiting for endpoint to become healthy in NEGs",
			mutateState: func() {
				pod := generatePod(testNamespace, podName, true, false, false)
				pod.CreationTimestamp = now
				podLister.Update(pod)
				client.CoreV1().Pods(testNamespace).Update(pod)
				testlookUp.readinessGateEnabledNegs = []string{"neg1", "neg2"}
				fakeClock.Step(unreadyTimeout)
			},
			inputKey:     keyFunc(testNamespace, podName),
			inputNeg:     "",
			expectExists: true,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:         testNamespace,
					Name:              podName,
					CreationTimestamp: now,
				},
				Spec: v1.PodSpec{
					ReadinessGates: []v1.PodReadinessGate{
						{ConditionType: shared.NegReadinessGate},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:    shared.NegReadinessGate,
							Reason:  negReadyTimedOutReason,
							Status:  v1.ConditionTrue,
							Message: fmt.Sprintf("Timeout waiting for pod to become healthy in at least one of the NEG(s): %v. Marking condition %q to True.", []string{"neg1", "neg2"}, shared.NegReadinessGate),
						},
					},
				},
			},
		},
	} {
		tc.mutateState()
		err := testReadinessReflector.syncPod(tc.inputKey, tc.inputNeg)
		if err != nil {
			t.Errorf("For test case %q, expect err to be nil, but got %v", tc.desc, err)
		}

		if tc.expectExists {
			pod, err := fakeContext.KubeClient.CoreV1().Pods(testNamespace).Get(tc.expectPod.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("For test case %q, expect err to be nil, but got %v", tc.desc, err)
			}
			// ignore creation timestamp for comparison
			pod.CreationTimestamp = tc.expectPod.CreationTimestamp
			if !reflect.DeepEqual(pod, tc.expectPod) {
				t.Errorf("For test case %q, expect pod to be %v, but got %v", tc.desc, tc.expectPod, pod)
			}
		}

	}
}
