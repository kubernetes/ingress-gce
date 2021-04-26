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

package main

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
)

func TestNEG(t *testing.T) {
	t.Parallel()
	const (
		numForwardingRules = 1
		serviceName1       = "neg-service1"
		serviceName2       = "neg-service2"
		ingressName        = "neg-ingress1"
		replicas           = int32(2)
	)
	port80 := networkingv1.ServiceBackendPort{Number: 80}

	type serviceAttr struct {
		annotations annotations.NegAnnotation
		svcType     v1.ServiceType
	}

	for _, tc := range []struct {
		desc             string
		ingress          *networkingv1.Ingress
		services         map[string]serviceAttr
		expectNegBackend bool
		expectIgBackend  bool
	}{
		{
			desc:    "Create a ingress with 2 NEG services of different types",
			ingress: fuzz.NewIngressBuilder("", ingressName, "").DefaultBackend(serviceName1, port80).AddPath("foo.com", "/foo", serviceName2, port80).Build(),
			services: map[string]serviceAttr{
				serviceName1: {
					annotations: annotations.NegAnnotation{Ingress: true},
					svcType:     v1.ServiceTypeClusterIP,
				},
				serviceName2: {
					annotations: annotations.NegAnnotation{Ingress: true},
					svcType:     v1.ServiceTypeNodePort,
				},
			},
			expectNegBackend: true,
			expectIgBackend:  false,
		},
		{
			desc:    "Create a ingress with 1 NEG service and 1 non-NEG service with default backend",
			ingress: fuzz.NewIngressBuilder("", ingressName, "").AddPath("foo.com", "/foo", serviceName1, port80).AddPath("bar.com", "/bar", serviceName2, port80).Build(),
			services: map[string]serviceAttr{
				serviceName1: {
					annotations: annotations.NegAnnotation{Ingress: false},
					svcType:     v1.ServiceTypeNodePort,
				},
				serviceName2: {
					annotations: annotations.NegAnnotation{Ingress: true},
					svcType:     v1.ServiceTypeClusterIP,
				},
			},
			expectNegBackend: true,
			expectIgBackend:  true,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()
			ctx := context.Background()

			for name, attr := range tc.services {
				_, err := e2e.EnsureEchoService(s, name, map[string]string{
					annotations.NEGAnnotationKey: attr.annotations.String()}, attr.svcType, replicas)
				if err != nil {
					t.Fatalf("error ensuring echo service: %v", err)
				}
				t.Logf("Echo service ensured (%s/%s)", s.Namespace, name)
			}
			ing := tc.ingress
			ing.Namespace = s.Namespace
			ing, err := e2e.EnsureIngress(s, ing)
			if err != nil {
				t.Fatalf("error ensuring Ingress spec: %v", err)
			}
			t.Logf("Ingress ensured (%s/%s)", s.Namespace, ing.Name)

			ing, err = e2e.WaitForIngress(s, ing, nil, nil)
			if err != nil {
				t.Fatalf("error waiting for Ingress to stabilize: %v", err)
			}
			t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

			// Perform whitebox testing.
			gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
			if err != nil {
				t.Fatalf("e2e.WhiteboxTest(%s/%s, ...) = %v, want nil", ing.Namespace, ing.Name, err)
			}

			// TODO(mixia): The below checks should be merged into PerformWhiteboxTests().
			if (len(gclb.NetworkEndpointGroup) > 0) != tc.expectNegBackend {
				t.Errorf("Error: expectNegBackend = %v, %d negs found for gclb %v", tc.expectNegBackend, len(gclb.NetworkEndpointGroup), gclb)
			}

			if (len(gclb.InstanceGroup) > 0) != tc.expectIgBackend {
				t.Errorf("Error: expectNegBackend = %v, %d negs found for gclb %v", tc.expectNegBackend, len(gclb.NetworkEndpointGroup), gclb)
			}

			if err := e2e.WaitForIngressDeletion(ctx, gclb, s, ing, &fuzz.GCLBDeleteOptions{}); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		})
	}
}

func TestNEGTransition(t *testing.T) {
	t.Parallel()

	port80 := networkingv1.ServiceBackendPort{Number: 80}

	ctx := context.Background()

	Framework.RunWithSandbox("NEG State Transition Tests", t, func(t *testing.T, s *e2e.Sandbox) {

		ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
			DefaultBackend("service-1", port80).
			Build()

		var previousGCLBState *fuzz.GCLB

		for _, tc := range []struct {
			desc        string
			annotations *annotations.NegAnnotation
			// negGC is true if a NEG should be garbage collected after applying the annotations
			negGC bool
		}{
			{
				desc:        "Using ingress only",
				annotations: &annotations.NegAnnotation{Ingress: true},
				negGC:       false,
			},
			{
				desc:        "Disable NEG for ingress",
				annotations: &annotations.NegAnnotation{Ingress: false},
				negGC:       true,
			},
			{
				desc:        "Re-enable NEG for ingress",
				annotations: &annotations.NegAnnotation{Ingress: true},
				negGC:       false,
			},
			{
				desc:        "No annotations",
				annotations: nil,
				negGC:       true,
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {
				svcAnnotations := map[string]string{}
				if tc.annotations != nil {
					svcAnnotations[annotations.NEGAnnotationKey] = tc.annotations.String()
				}
				// First create the echo service, we will be adapting it throughout the basic tests
				_, err := e2e.EnsureEchoService(s, "service-1", svcAnnotations, v1.ServiceTypeNodePort, 1)

				if err != nil {
					t.Fatalf("error ensuring echo service: %v", err)
				}
				t.Logf("Echo service ensured (%s/%s)", s.Namespace, "service-1")

				ing.Namespace = s.Namespace
				// Create the ingress
				ing, err = e2e.EnsureIngress(s, ing)
				if err != nil {
					t.Fatalf("error ensuring Ingress spec: %v", err)
				}
				t.Logf("Ingress ensured (%s/%s)", s.Namespace, ing.Name)

				ing, err = e2e.WaitForIngress(s, ing, nil, nil)
				if err != nil {
					t.Fatalf("error waiting for Ingress to stabilize: %v", err)
				}
				t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)

				// Perform whitebox testing.
				gclb, err := e2e.WhiteboxTest(ing, nil, Framework.Cloud, "", s)
				if err != nil {
					t.Fatalf("e2e.WhiteboxTest(%s/%s, ...)", ing.Namespace, ing.Name)
				}

				if tc.negGC {
					if len(gclb.NetworkEndpointGroup) != 0 {
						t.Errorf("NegGC = true, expected 0 negs for gclb %v, got %d", gclb, len(gclb.NetworkEndpointGroup))
					}
					if err = e2e.WaitForNEGDeletion(ctx, s.ValidatorEnv.Cloud(), previousGCLBState, nil); err != nil {
						t.Errorf("Error waiting for NEGDeletion: %v", err)
					}
				} else {
					if len(gclb.NetworkEndpointGroup) < 1 {
						t.Errorf("Error, no NEGS associated with gclb %v, expected at least one", gclb)
					}
				}
				previousGCLBState = gclb
			})
		}

		if ing != nil && previousGCLBState != nil {
			if err := e2e.WaitForIngressDeletion(ctx, previousGCLBState, s, ing, &fuzz.GCLBDeleteOptions{}); err != nil {
				t.Errorf("e2e.WaitForIngressDeletion(..., %q, nil) = %v, want nil", ing.Name, err)
			}
		}
	})
}

func TestNEGSyncEndpoints(t *testing.T) {
	t.Parallel()

	port80 := networkingv1.ServiceBackendPort{Number: 80}
	svcName := "service-1"

	for _, tc := range []struct {
		desc                     string
		annotations              annotations.NegAnnotation
		expectServicePort        sets.String
		expectHealthyServicePort sets.String
		checkBackendReachability bool
	}{
		{
			desc:                     "Ingress NEG only",
			annotations:              annotations.NegAnnotation{Ingress: true},
			expectServicePort:        sets.NewString("80"),
			expectHealthyServicePort: sets.NewString("80"),
			checkBackendReachability: true,
		},
		{
			desc: "Both standalone NEGs and Ingress NEG enabled",
			annotations: annotations.NegAnnotation{
				Ingress: true,
				ExposedPorts: map[int32]annotations.NegAttributes{
					int32(443): {},
				},
			},
			expectServicePort:        sets.NewString("80", "443"),
			expectHealthyServicePort: sets.NewString("80"),
			checkBackendReachability: true,
		},
		{
			desc: "Standalone NEGs only",
			annotations: annotations.NegAnnotation{
				Ingress: false,
				ExposedPorts: map[int32]annotations.NegAttributes{
					int32(443): {},
					int32(80):  {},
				},
			},
			expectServicePort:        sets.NewString("80", "443"),
			expectHealthyServicePort: sets.NewString(),
			checkBackendReachability: false,
		},
	} {
		tc := tc // Capture tc as we are running this in parallel.
		Framework.RunWithSandbox(tc.desc, t, func(t *testing.T, s *e2e.Sandbox) {
			t.Parallel()
			ctx := context.Background()

			svcAnnotations := map[string]string{annotations.NEGAnnotationKey: tc.annotations.String()}
			_, err := e2e.EnsureEchoService(s, svcName, svcAnnotations, v1.ServiceTypeClusterIP, 0)

			if err != nil {
				t.Fatalf("error ensuring echo service: %v", err)
			}
			t.Logf("Echo service ensured (%s/%s)", s.Namespace, "service-1")

			scaleAndValidate := func(replicas int32) {
				t.Logf("Scaling echo deployment to %v replicas", replicas)
				// The deployment is created with pod anti affinity rules trying to spread the pods across zones.
				// GCLB only creates the underlying infrastructure in each zone when there is at least one backend.
				// Since this test tries to validate by sending traffic, it is essential that the LB backends are fully
				// instantiated in all zones so that the new endpoints can show up faster before test timeout occur.
				// If the LB backend need to be freshly setup when a new pod is scheduled to the zone, this may lead to
				// test timeout as it takes more time for the pod to respond to traffic
				// However, the anti affinity rule may not fully solve this problem in the case where there
				// is no capacity left in all nodes in a zone. Hence, it may still cause all pods to be scheduled into
				// other zones. A pod started later may get scheduled to a zone when capacity freed up.
				if err := e2e.EnsureEchoDeployment(s, svcName, replicas, e2e.SpreadPodAcrossZones); err != nil {
					t.Fatalf("error ensuring echo deployment: %v", err)
				}

				if err := e2e.WaitForEchoDeploymentStable(s, svcName); err != nil {
					t.Fatalf("Echo deployment failed to become stable: %v", err)
				}

				// validate via sending traffic
				if tc.checkBackendReachability {
					// only ensure ingress if we check reachability
					ing := fuzz.NewIngressBuilder(s.Namespace, "ingress-1", "").
						DefaultBackend(svcName, port80).
						Build()
					ing, err = e2e.EnsureIngress(s, ing)
					if err != nil {
						t.Fatalf("error ensuring Ingress spec: %v", err)
					}
					t.Logf("Ingress ensured (%s/%s)", s.Namespace, ing.Name)

					ing, err = e2e.WaitForIngress(s, ing, nil, nil)
					if err != nil {
						t.Fatalf("error waiting for Ingress to stabilize: %v", err)
					}
					t.Logf("GCLB resources created (%s/%s)", s.Namespace, ing.Name)
					vip := ing.Status.LoadBalancer.Ingress[0].IP
					t.Logf("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
					if err = e2e.WaitForDistinctHosts(ctx, vip, int(replicas), true); err != nil {
						t.Errorf("error waiting for Ingress to response from %v backends: %v", replicas, err)
					}
				}

				// validate neg status
				negStatus, err := e2e.WaitForNegStatus(s, svcName, tc.expectServicePort.List(), false)
				if err != nil {
					t.Fatalf("error waiting for NEG status to update: %v", err)
				}

				// validate neg configurations
				for port, negName := range negStatus.NetworkEndpointGroups {
					if tc.expectHealthyServicePort.Has(port) {
						e2e.WaitForNegs(ctx, Framework.Cloud, negName, negStatus.Zones, true, int(replicas))
					} else if tc.expectServicePort.Has(port) {
						e2e.WaitForNegs(ctx, Framework.Cloud, negName, negStatus.Zones, false, int(replicas))
					} else {
						t.Errorf("Unexpected port %v and NEG %q in NEG Status %v", port, negName, negStatus)
					}
				}
			}

			// This test rescales test backend and validate if NEG controller is able to handle it correctly.
			// Following validation is performed:
			// 1. validate if expected number of network endpoint is in NEGs
			// 2. validate if the newtork endpoint is healthy
			// 3. validate by sending traffic to LB VIP and check if expected number of backends can be reached.
			// First scale up the pods to 5 replicas to try to cover all zones where the cluster spans.
			scaleAndValidate(5)
			scaleAndValidate(3)
			scaleAndValidate(1)
			scaleAndValidate(4)
			scaleAndValidate(2)
		})
	}
}

func TestReadinessReflector(t *testing.T) {
	t.Parallel()
	Framework.RunWithSandbox("Readiness reflector should handle pods that are not behind NEG but with NEG readiness gate", t, func(t *testing.T, s *e2e.Sandbox) {
		name := "deployment1"
		// create deployment with NEG readiness gate
		if err := e2e.EnsureEchoDeployment(s, name, 3, func(deployment *apps.Deployment) {
			deployment.Spec.Template.Spec.ReadinessGates = []v1.PodReadinessGate{{ConditionType: shared.NegReadinessGate}}
		}); err != nil {
			t.Errorf("Failed to ensure echo deployment: %v", err)
		}

		if err := e2e.WaitForEchoDeploymentStable(s, name); err != nil {
			t.Errorf("Echo deployment failed to become stable: %v", err)
		}
	})
}

func TestNegCRDTransitions(t *testing.T) {
	t.Parallel()
	port80 := networkingv1.ServiceBackendPort{Number: 80}
	port443 := networkingv1.ServiceBackendPort{Number: 443}
	serviceName := "neg-service"
	ctx := context.Background()

	Framework.RunWithSandbox("NEGs with custom names", t, func(t *testing.T, s *e2e.Sandbox) {
		var previousNegStatus annotations.NegStatus
		expectedNEGName := fmt.Sprintf("test-neg-name-%x", s.RandInt)

		for _, tc := range []struct {
			desc               string
			annotations        annotations.NegAnnotation
			replicas           int32
			expectedNegAttrs   map[string]string
			expectedGCNegPorts []string
		}{
			{desc: "one NEG with custom name, one neg with generated name",
				annotations: annotations.NegAnnotation{
					Ingress: false,
					ExposedPorts: map[int32]annotations.NegAttributes{
						port80.Number:  annotations.NegAttributes{Name: expectedNEGName},
						port443.Number: annotations.NegAttributes{},
					}},
				replicas:         2,
				expectedNegAttrs: map[string]string{strconv.Itoa(int(port80.Number)): expectedNEGName, strconv.Itoa(int(port443.Number)): ""},
			},
			{desc: "remove custom name",
				annotations: annotations.NegAnnotation{
					Ingress: false,
					ExposedPorts: map[int32]annotations.NegAttributes{
						port80.Number:  annotations.NegAttributes{},
						port443.Number: annotations.NegAttributes{},
					}},
				replicas:           2,
				expectedNegAttrs:   map[string]string{strconv.Itoa(int(port80.Number)): "", strconv.Itoa(int(port443.Number)): ""},
				expectedGCNegPorts: []string{strconv.Itoa(int(port80.Number))},
			},
			{desc: "add custom name",
				annotations: annotations.NegAnnotation{
					Ingress: false,
					ExposedPorts: map[int32]annotations.NegAttributes{
						port80.Number:  annotations.NegAttributes{},
						port443.Number: annotations.NegAttributes{Name: expectedNEGName},
					}},
				replicas:           2,
				expectedNegAttrs:   map[string]string{strconv.Itoa(int(port80.Number)): "", strconv.Itoa(int(port443.Number)): expectedNEGName},
				expectedGCNegPorts: []string{strconv.Itoa(int(port443.Number))},
			},
			{desc: "no NEGs",
				annotations: annotations.NegAnnotation{
					Ingress:      false,
					ExposedPorts: map[int32]annotations.NegAttributes{}},
				replicas:           2,
				expectedGCNegPorts: []string{strconv.Itoa(int(port80.Number)), strconv.Itoa(int(port443.Number))},
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {
				_, err := e2e.EnsureEchoService(s, serviceName, map[string]string{
					annotations.NEGAnnotationKey: tc.annotations.String()}, v1.ServiceTypeClusterIP, tc.replicas)
				if err != nil {
					t.Fatalf("error ensuring echo service: %v", err)
				}
				t.Logf("Echo service ensured (%s/%s)", s.Namespace, serviceName)

				if len(tc.expectedGCNegPorts) > 0 {
					for _, port := range tc.expectedGCNegPorts {
						if err = e2e.WaitForStandaloneNegDeletion(ctx, s.ValidatorEnv.Cloud(), s, port, previousNegStatus); err != nil {
							t.Errorf("Error waiting for NEGDeletion: %v", err)
						}
					}
				}

				negStatus, err := e2e.WaitForNegCRs(s, serviceName, tc.expectedNegAttrs)
				if err != nil {
					t.Fatalf("Error: e2e.WaitForNegCRs(%s,%+v) = %s, want nil", serviceName, tc.expectedNegAttrs, err)
				}

				for port, negName := range negStatus.NetworkEndpointGroups {
					err := e2e.WaitForNegs(ctx, Framework.Cloud, negName, negStatus.Zones, false, int(tc.replicas))
					if err != nil {
						t.Fatalf("Error: e2e.WaitForNegs service %s/%s neg port/name %s/%s", serviceName, s.Namespace, port, negName)
					}
				}
				previousNegStatus = negStatus
			})
		}
	})
}

func TestNegCRDErrorEvents(t *testing.T) {
	t.Parallel()
	port80 := networkingv1.ServiceBackendPort{Number: 80}
	svc1 := "svc1"
	svc2 := "svc2"
	replicas := int32(2)
	ctx := context.Background()

	Framework.RunWithSandbox("two services, same neg name", t, func(t *testing.T, s *e2e.Sandbox) {
		expectedNEGName := fmt.Sprintf("test-neg-name-%x", s.RandInt)
		annotation := annotations.NegAnnotation{
			Ingress: true,
			ExposedPorts: map[int32]annotations.NegAttributes{
				port80.Number: annotations.NegAttributes{Name: expectedNEGName},
			},
		}

		_, err := e2e.EnsureEchoService(s, svc1, map[string]string{
			annotations.NEGAnnotationKey: annotation.String()}, v1.ServiceTypeClusterIP, replicas)
		if err != nil {
			t.Fatalf("error ensuring echo service: %v", err)
		}

		// Ingress true with a custom name should cause an event
		if err = e2e.WaitForSvcNegErrorEvents(s, svc1, []string{"custom neg name cannot be used with ingress enabled"}); err != nil {
			t.Errorf("error waiting for error events: %s", err)
		}

		// Ensure service with ingress true and wait for neg to be created
		annotation.Ingress = false
		_, err = e2e.EnsureEchoService(s, svc1, map[string]string{
			annotations.NEGAnnotationKey: annotation.String()}, v1.ServiceTypeClusterIP, replicas)
		if err != nil {
			t.Fatalf("error ensuring echo service: %v", err)
		}
		t.Logf("Echo service ensured (%s/%s)", s.Namespace, svc1)

		expectedNegAttrs := map[string]string{strconv.Itoa(int(port80.Number)): expectedNEGName}
		negStatus, err := e2e.WaitForNegCRs(s, svc1, expectedNegAttrs)
		if err != nil {
			t.Fatalf("Error: e2e.WaitForNegCRs(%s,%+v) = %s, want nil", svc1, expectedNegAttrs, err)
		}

		for port, negName := range negStatus.NetworkEndpointGroups {
			err := e2e.WaitForNegs(ctx, Framework.Cloud, negName, negStatus.Zones, false, int(replicas))
			if err != nil {
				t.Fatalf("Error: e2e.WaitForNegs service %s/%s neg port/name %s/%s", svc1, s.Namespace, port, negName)
			}
		}

		// Ensure a second service requesting the same neg name
		_, err = e2e.EnsureEchoService(s, svc2, map[string]string{
			annotations.NEGAnnotationKey: annotation.String()}, v1.ServiceTypeClusterIP, replicas)
		if err != nil {
			t.Fatalf("error ensuring echo service: %v", err)
		}

		// Requesting the same neg name should cause an error event on the second service
		if err = e2e.WaitForSvcNegErrorEvents(s, svc2, []string{"Neg already exists", "Please remove previous neg before creating this configuration"}); err != nil {
			t.Errorf("error waiting for error events: %s", err)
		}

		// GC existing negs
		_, err = e2e.EnsureEchoService(s, svc1, map[string]string{}, v1.ServiceTypeClusterIP, replicas)
		if err != nil {
			t.Fatalf("error ensuring echo service: %v", err)
		}

		_, err = e2e.EnsureEchoService(s, svc2, map[string]string{}, v1.ServiceTypeClusterIP, replicas)
		if err != nil {
			t.Fatalf("error ensuring echo service: %v", err)
		}

		e2e.WaitForStandaloneNegDeletion(ctx, Framework.Cloud, s, strconv.Itoa(int(port80.Number)), negStatus)
	})
}

func TestNegDisruptive(t *testing.T) {
	t.Parallel()
	port80 := networkingv1.ServiceBackendPort{Number: 80}
	replicas := int32(2)
	serviceName := "disruptive-neg-service"
	// gcSvcName is the name of the service used to determine if GC has finished
	gcSvcName := "gc-service"
	ctx := context.Background()

	annotation := annotations.NegAnnotation{
		ExposedPorts: map[int32]annotations.NegAttributes{
			port80.Number: annotations.NegAttributes{},
		},
	}

	ensureGCService := func(s *e2e.Sandbox) annotations.NegStatus {
		// use gc-service as a way to track if GC has completed or not
		_, err := e2e.EnsureEchoService(s, "gc-service", map[string]string{
			annotations.NEGAnnotationKey: annotation.String()}, v1.ServiceTypeClusterIP, replicas)
		if err != nil {
			t.Fatalf("error ensuring gc service: %v", err)
		}
		t.Logf("GC service ensured (%s/%s)", s.Namespace, gcSvcName)

		expectedNegAttrs := map[string]string{strconv.Itoa(int(port80.Number)): ""}
		negStatus, err := e2e.WaitForNegCRs(s, gcSvcName, expectedNegAttrs)
		if err != nil {
			t.Fatalf("Error: e2e.WaitForNegCRs(%s,%+v) = %s, want nil", gcSvcName, expectedNegAttrs, err)
		}
		for port, negName := range negStatus.NetworkEndpointGroups {
			err := e2e.WaitForNegs(ctx, Framework.Cloud, negName, negStatus.Zones, false, int(replicas))
			if err != nil {
				t.Fatalf("Error: e2e.WaitForNegs service %s/%s neg port/name %s/%s", gcSvcName, s.Namespace, port, negName)
			}
		}
		return negStatus
	}

	waitForGCSvcDeletion := func(s *e2e.Sandbox, negStatus annotations.NegStatus) {
		if err := e2e.DeleteEchoService(s, gcSvcName); err != nil {
			t.Fatalf("Error: e2e.DeleteEchoService %s: %q", gcSvcName, err)
		}
		t.Logf("GC service deleted (%s/%s)", s.Namespace, gcSvcName)

		if err := e2e.WaitForStandaloneNegDeletion(ctx, s.ValidatorEnv.Cloud(), s, strconv.Itoa(int(port80.Number)), negStatus); err != nil {
			t.Fatalf("Error waiting for NEGDeletion: %v", err)
		}
	}

	Framework.RunWithSandbox("Disruptive Service Recreations", t, func(t *testing.T, s *e2e.Sandbox) {

		testcases := []struct {
			desc string
			// waitForGC waits until the NEG controller finished GC. It will wait at the beginning of the testcase
			// before processing any service changes
			waitForGC bool
			// waitForNeg will occur at the end of the testcase after any deletion and creation is processed
			waitForNeg bool
			// waitForNegGC waits to see that the NEG has been deleted
			waitForNegGC bool
			// deleteService will delete the service will run be run before createService
			deleteService bool
			// createService will create the service will run be run after deleteService
			createService bool
			// checkForErrorEvents checks to see that no processing error events have occurred
			checkErrorEvents bool
		}{
			{
				desc:          "create service and wait for neg creations",
				createService: true,
				waitForNeg:    true,
			},
			{
				desc:          "delete service and recreate first time",
				deleteService: true,
				createService: true,
			},
			{
				desc:          "delete service and recreate second time",
				deleteService: true,
				createService: true,
			},
			{
				desc:          "delete service and recreate third time",
				deleteService: true,
				createService: true,
			},
			{
				desc:       "check that neg still exists",
				waitForNeg: true,
			},
			//waitForGC is to ensure that GC doesn't occur after deletion and before the recreation
			{
				desc:          "waitForGC, delete service and recreate fourth time",
				waitForGC:     true,
				deleteService: true,
				createService: true,
			},
			{
				desc:          "delete service and recreate fifth time",
				deleteService: true,
				createService: true,
			},
			{
				desc:          "delete service and recreate sixth time",
				deleteService: true,
				createService: true,
			},
			{
				desc:             "check neg still exists and no processing error events",
				waitForNeg:       true,
				checkErrorEvents: true,
			},
			{
				desc:          "delete service and recreate seventh time",
				deleteService: true,
				createService: true,
			},
			{
				desc:          "delete service and recreate eight time",
				deleteService: true,
				createService: true,
			},
			{
				desc:       "check that neg still exists",
				waitForNeg: true,
			},
			{
				desc:          "delete service",
				deleteService: true,
			},
			{
				desc:         "waitForGC to properly delete",
				waitForNegGC: true,
			},
		}

		var previousNegStatus annotations.NegStatus
		for _, tc := range testcases {
			t.Log(tc.desc)
			if tc.waitForGC {
				negStatus := ensureGCService(s)
				waitForGCSvcDeletion(s, negStatus)
			}

			if tc.deleteService {
				if err := e2e.DeleteEchoService(s, serviceName); err != nil {
					t.Fatalf("Error: e2e.DeleteEchoService %s: %q", serviceName, err)
				}
				t.Logf("Echo service deleted (%s/%s)", s.Namespace, serviceName)
			}

			if tc.createService {
				_, err := e2e.EnsureEchoService(s, serviceName, map[string]string{
					annotations.NEGAnnotationKey: annotation.String()}, v1.ServiceTypeClusterIP, replicas)
				if err != nil {
					t.Fatalf("error ensuring echo service: %v", err)
				}
				t.Logf("Echo service ensured (%s/%s)", s.Namespace, serviceName)
			}

			if tc.waitForNeg {
				expectedNegAttrs := map[string]string{strconv.Itoa(int(port80.Number)): ""}
				negStatus, err := e2e.WaitForNegCRs(s, serviceName, expectedNegAttrs)
				if err != nil {
					t.Fatalf("Error: e2e.WaitForNegCRs(%s,%+v) = %s, want nil", serviceName, expectedNegAttrs, err)
				}

				for port, negName := range negStatus.NetworkEndpointGroups {
					err := e2e.WaitForNegs(ctx, Framework.Cloud, negName, negStatus.Zones, false, int(replicas))
					if err != nil {
						t.Fatalf("Error: e2e.WaitForNegs service %s/%s neg port/name %s/%s", serviceName, s.Namespace, port, negName)
					}
				}
				previousNegStatus = negStatus
			}

			if tc.checkErrorEvents {
				// check for "error processing service" events that do not include "is shutting down" or "not found"
				// Error events that are due to syncers shutting down or service not found (due to out of date cache)
				// are temporary and can be ignored
				foundEvents, err := e2e.CheckSvcEvents(s, serviceName, v1.EventTypeWarning, "error processing service", "is shutting down", "not found")
				if err != nil {
					t.Fatalf("errored quering for service events: %q", err)
				}
				if foundEvents {
					t.Fatalf("found error events when none were expected")
				}
			}

			if tc.waitForNegGC {
				if err := e2e.WaitForStandaloneNegDeletion(ctx, s.ValidatorEnv.Cloud(), s, strconv.Itoa(int(port80.Number)), previousNegStatus); err != nil {
					t.Fatalf("Error waiting for NEGDeletion: %v", err)
				}
			}
		}
	})
}
