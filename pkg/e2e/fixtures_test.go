/*
Copyright 2023 The Kubernetes Authors.
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

package e2e

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	"k8s.io/ingress-gce/pkg/negannotation"
	"k8s.io/utils/strings/slices"
)

const (
	testServiceName = "service-1"
	testEPSName     = "custom-endpointslice"
	replicas        = 2
)

func TestEnsureCustomEndpointSlice(t *testing.T) {
	endpointCount := 2 // endpointCount is the count of endpoints in the custom endpoint slice.

	s, err := newTestSandbox()
	if err != nil {
		t.Fatalf("Failed to create test sandbox, err: %v", err)
	}
	defer s.Destroy()

	// Set up echo service.
	annotation := negannotation.NegAnnotation{
		Ingress: false,
		ExposedPorts: map[int32]negannotation.NegAttributes{
			int32(443): {},
			int32(80):  {},
		},
	}
	svcAnnotations := map[string]string{negannotation.NEGAnnotationKey: annotation.String()}
	svc, err := EnsureEchoService(s, testServiceName, svcAnnotations, v1.ServiceTypeClusterIP, replicas)
	if err != nil {
		t.Fatalf("Failed to create echo service, err: %v", err)
	}

	// Create pods directly since the fake deployment does not create pods.
	// We also need to provide podIP in the pod config since podIP won't be
	// generating in fake Pod creation.
	var podIPs []string
	var pods []v1.Pod
	for i := 1; i <= endpointCount; i++ {
		podName := fmt.Sprintf("pod-%d", i)
		podIP := fmt.Sprintf("10.100.1.%d", i)
		pod, err := newTestPodWithIP(s, podName, testEPSName, podIP)
		if err != nil || pod == nil {
			t.Fatalf("Error when newTestPodWithIP(%s, %s)", podName, testEPSName)
		}
		podIPs = append(podIPs, podIP)
		pods = append(pods, *pod)
	}

	// Create a custom endpoint slice, and it should use IPs from testPods.
	eps, err := EnsureCustomEndpointSlice(s, svc, testEPSName, pods, func(endpointslice *discoveryv1.EndpointSlice) {})
	if err != nil {
		t.Fatalf("Failed to create custom endpoint slice, err: %v", err)
	}

	// Validate endpoint IPs are from testPod IPs.
	for _, endpoint := range eps.Endpoints {
		if !slices.Contains(podIPs, endpoint.Addresses[0]) {
			t.Fatalf("Endpoint IP not from pod, PodIPs: %v, endpoint IP: %v", podIPs, endpoint.Addresses[0])
		}
	}
}

// newTestSandbox creates a sandbox for unit testing with fake ClientSet and GCE.
func newTestSandbox() (*Sandbox, error) {
	randInt := rand.Int63()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	f := &Framework{
		Clientset: fake.NewSimpleClientset(),
		Cloud:     fakeGCE.Compute(),
	}
	s := &Sandbox{
		Namespace: fmt.Sprintf("test-sandbox-%x", randInt),
		f:         f,
		RandInt:   randInt,
	}

	// Create sandbox without validator.
	ns := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: s.Namespace,
		},
	}
	if _, err := s.f.Clientset.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{}); err != nil {
		return nil, err
	}
	return s, nil
}

// newTestPodWithIP creates a test Pod. A pod IP must be provided since in the
// fake ClientSet, podIP is not automatically generated during pod creation.
func newTestPodWithIP(s *Sandbox, podName, appName, podIP string) (*v1.Pod, error) {
	podSpec := v1.PodSpec{
		ReadinessGates: []v1.PodReadinessGate{{ConditionType: shared.NegReadinessGate}},
		NodeSelector:   map[string]string{"kubernetes.io/os": "linux"},
		Containers: []v1.Container{
			{
				Name:  "echoheaders",
				Image: echoheadersImage,
				Ports: []v1.ContainerPort{
					{ContainerPort: 8080, Name: "http-port"},
					{ContainerPort: 8443, Name: "https-port"},
				},
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: s.Namespace,
			Name:      podName,
			Labels: map[string]string{
				"app": appName,
			},
		},
		Status: v1.PodStatus{
			PodIP: podIP,
		},
		Spec: podSpec,
	}
	return s.f.Clientset.CoreV1().Pods(s.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
}
