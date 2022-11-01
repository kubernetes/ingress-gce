package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	istioV1alpha3 "istio.io/api/networking/v1alpha3"
	apiv1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/pkg/e2e"
)

const (
	asmConfigNamespace          = "kube-system"
	asmConfigName               = "ingress-controller-asm-cm-config"
	negControllerRestartTimeout = 5 * time.Minute
)

// TestASMConfig tests the ASM enable/disable, it can't run parallel with other tests.
func TestASMConfig(t *testing.T) {
	Framework.RunWithSandbox("TestASMConfig", t, func(t *testing.T, s *e2e.Sandbox) {
		if !s.IstioEnabled() {
			t.Fatalf("This test must run against a cluster with Istio on.")
		}
		for _, tc := range []struct {
			desc                string
			configMap           map[string]string
			wantASMReady        bool
			wantConfigMapEvents []string
		}{
			{
				desc:                "Invalid ConfigMap value equals to disable",
				configMap:           map[string]string{"enable-asm": "INVALID"},
				wantASMReady:        false,
				wantConfigMapEvents: []string{"The map provided a unvalid value for field: enable-asm, value: INVALID"},
			},
			{
				desc:                "Invalid ConfigMap filed equals to disable",
				configMap:           map[string]string{"enable-unknow-field": "INVALID1"},
				wantASMReady:        false,
				wantConfigMapEvents: []string{"The map contains a unknown key-value pair: enable-unknow-field:INVALID1"},
			},
			{
				desc:         "Set enable-asm to true should restart the controller",
				configMap:    map[string]string{"enable-asm": "true"},
				wantASMReady: true,
				wantConfigMapEvents: []string{"ConfigMapConfigController: Get a update on the ConfigMapConfig, Restarting Ingress controller",
					"NEG controller is running in ASM Mode with Istio API: v1alpha3"},
			},
			{
				desc:         "Invalid ConfigMap value equals to disable",
				configMap:    map[string]string{"enable-asm": "INVALID2"},
				wantASMReady: false,
				wantConfigMapEvents: []string{"The map provided a unvalid value for field: enable-asm, value: INVALID2",
					"ConfigMapConfigController: Get a update on the ConfigMapConfig, Restarting Ingress controller"},
			},
		} {
			t.Run(tc.desc, func(t *testing.T) {
				var err error
				if err = e2e.EnsureConfigMap(s, asmConfigNamespace, asmConfigName, tc.configMap); err != nil {
					t.Errorf("Failed to ensure ConfigMap, error: %s", err)
				}

				if waitErr := wait.Poll(5*time.Second, 10*time.Minute, func() (bool, error) {
					cmData, err := e2e.GetConfigMap(s, asmConfigNamespace, asmConfigName)
					if err != nil {
						return false, err
					}
					if val, ok := cmData["asm-ready"]; ok {
						return val == strconv.FormatBool(tc.wantASMReady), nil
					}
					return false, nil

				}); waitErr != nil {
					if apierrors.IsTimeout(waitErr) {
						t.Fatalf("Failed to validate asm-ready = %t. Error time out, last seen error: %v", tc.wantASMReady, err)
					} else {
						t.Fatalf("Failed to validate asm-ready = %t. Error: %v", tc.wantASMReady, waitErr)
					}
				}

				if err := e2e.WaitConfigMapEvents(s, asmConfigNamespace, asmConfigName, tc.wantConfigMapEvents, negControllerRestartTimeout); err != nil {
					t.Fatalf("Failed to get events: %v; Error %e", strings.Join(tc.wantConfigMapEvents, ";"), err)
				}
			})
		}
		e2e.DeleteConfigMap(s, asmConfigNamespace, asmConfigName)
	})
}

// TestASMServiceAndDestinationRule tests the service/destinationrule, it can't run parallel with other tests.
func TestASMServiceAndDestinationRule(t *testing.T) {
	_ = istioV1alpha3.DestinationRule{}
	_ = apiv1.ComponentStatus{}

	// This test case will need two namespaces, one will in asm-skip-namespaces.
	Framework.RunWithSandbox("TestASMServiceAndDestinationRule", t, func(t *testing.T, sSkip *e2e.Sandbox) {
		Framework.RunWithSandbox("TestASMServiceAndDestinationRule", t, func(t *testing.T, s *e2e.Sandbox) {
			if !s.IstioEnabled() {
				t.Fatalf("This test must run against a cluster with Istio on.")
			}
			// Enable ASM mode
			ctx := context.Background()

			asmConfig := map[string]string{"enable-asm": "true",
				"asm-skip-namespaces": fmt.Sprintf("kube-system,istio-system,%s", sSkip.Namespace)}
			if err := e2e.EnsureConfigMap(s, asmConfigNamespace, asmConfigName,
				asmConfig); err != nil {
				t.Error(err)
			}

			var porterPort int32
			porterPort = 80
			svcName := "service"
			svcSkipName := "service-skip"

			// Create deployments used by the Service and DestinationRules.
			// The deployment will contain two label pairs: {"app": "porter", "version": "*"}
			// Different versions will be used as DestinationRule: subset
			for _, deployment := range []struct {
				deploymentName string
				replicas       int32
				version        string
			}{
				{deploymentName: "deployment-v1", replicas: 1, version: "v1"},
				{deploymentName: "deployment-v2", replicas: 2, version: "v2"},
				{deploymentName: "deployment-v3", replicas: 3, version: "v3"},
			} {
				if err := e2e.CreatePorterDeployment(s, deployment.deploymentName, deployment.replicas, deployment.version); err != nil {
					t.Errorf("Failed to create deployment, Error: %s", err)
				}
			}

			// Create and validate DestinationRules level NEGs, NEG controller shouldn't create DestinationRules level NEGs for those services in the skip namespace.
			for _, svc := range []struct {
				desc            string
				svcName         string
				inSkipNamespace bool
			}{
				{desc: "NEG Controller should create NEGs for all ports for a service by default", svcName: svcName, inSkipNamespace: false},
				{desc: "NEG Controller shouldn't create NEGs for all ports for a service if it's in a skip namespace", svcName: svcSkipName, inSkipNamespace: true},
			} {
				t.Logf("Running test case: %s", svc.desc)
				sandbox := s
				noPresentTest := false
				if svc.inSkipNamespace {
					sandbox = sSkip
					noPresentTest = true
				}
				if err := e2e.CreatePorterService(sandbox, svc.svcName); err != nil {
					t.Errorf("Failed to create service, Error: %s", err)
				}

				// Test the Service Annotations
				negStatus, err := e2e.WaitForNegStatus(sandbox, svc.svcName, []string{strconv.Itoa(int(porterPort))}, noPresentTest)
				if err != nil {
					t.Errorf("Failed to wait for Service NEGAnnotation, error: %s", err)
				}
				if svc.inSkipNamespace {
					if negStatus != nil {
						t.Errorf("Service: %s/%s is in the ASM skip namespace, shouldn't have NEG Status. ASM Config: %v, NEGStatus got: %v",
							sandbox.Namespace, svc.svcName, asmConfig, negStatus)
					}
				} else {
					if negName, ok := negStatus.NetworkEndpointGroups[strconv.Itoa(int(porterPort))]; ok {
						if err := e2e.WaitForNegs(ctx, Framework.Cloud, negName, negStatus.Zones, false, 6); err != nil {
							t.Errorf("Failed to wait Negs, error: %s", err)
						}
					} else {
						t.Fatalf("Service annotation doesn't contain the desired NEG status, want: %d, have: %v", porterPort, negStatus.NetworkEndpointGroups)
					}
				}
			}

			for _, tc := range []struct {
				desc                   string
				destinationRuleName    string
				subsetEndpointCountMap map[string]int
				crossNamespace         bool // If crossNamespace set, the DestinationRule will use full Host(Service) name to refer a service in a different namespace.
			}{
				{desc: "NEG controller should create NEGs for destinationrule", destinationRuleName: "porter-destinationrule", subsetEndpointCountMap: map[string]int{"v1": 1, "v2": 2, "v3": 3}, crossNamespace: false},
				{desc: "NEG controller should update NEGs for destinationrule", destinationRuleName: "porter-destinationrule", subsetEndpointCountMap: map[string]int{"v1": 1, "v2": 2}, crossNamespace: false},
				{desc: "NEG controller should create NEGs for cross namespace destinationrule", destinationRuleName: "porter-destinationrule-1", subsetEndpointCountMap: map[string]int{"v1": 1}, crossNamespace: true},
			} {
				t.Run(tc.desc, func(t *testing.T) {
					sandbox := s
					drHost := svcName
					// crossNamespace will test DestinationRules that referring a service located in a different namespace
					if tc.crossNamespace {
						sandbox = sSkip
						drHost = fmt.Sprintf("%s.%s.svc.cluster.local", svcName, s.Namespace)
					}

					versions := []string{}
					for k := range tc.subsetEndpointCountMap {
						versions = append(versions, k)
					}
					if err := e2e.EnsurePorterDestinationRule(sandbox, tc.destinationRuleName, drHost, versions); err != nil {
						t.Errorf("Failed to create destinationRule, error: %s", err)
					}

					// One DestinationRule should have count(NEGs) = count(subset)* count(port)
					dsNEGStatus, err := e2e.WaitDestinationRuleAnnotation(sandbox, sandbox.Namespace, tc.destinationRuleName, len(tc.subsetEndpointCountMap)*1, 5*time.Minute)
					if err != nil {
						t.Errorf("Failed to validate the NEG count. Error: %s", err)
					}

					zones := dsNEGStatus.Zones
					for subsetVersion, endpointCount := range tc.subsetEndpointCountMap {
						negNames, ok := dsNEGStatus.NetworkEndpointGroups[subsetVersion]
						if !ok {
							t.Fatalf("DestinationRule annotation doesn't contain the desired NEG status, want: %s, have: %v", subsetVersion, dsNEGStatus.NetworkEndpointGroups)
						}
						negName, ok := negNames[strconv.Itoa(int(porterPort))]
						if !ok {
							t.Fatalf("DestinationRule annotation doesn't contain the desired NEG status, want: %d, have: %v", porterPort, negNames)
						}
						if err := e2e.WaitForNegs(ctx, Framework.Cloud, negName, zones, false, endpointCount); err != nil {
							t.Errorf("Failed to wait Negs, error: %s", err)
						}
					}

				})
			}

			negStatus, err := e2e.WaitForNegStatus(s, svcName, []string{strconv.Itoa(int(porterPort))}, false)
			if err != nil {
				t.Fatalf("Error: e2e.WaitForNegStatus %s: %q", svcName, err)
			}

			if err := e2e.DeleteService(s, svcName); err != nil {
				t.Fatalf("Error: e2e.DeleteService %s: %q", svcName, err)
			}
			t.Logf("GC service deleted (%s/%s)", s.Namespace, svcName)

			if err := e2e.DeleteService(sSkip, svcSkipName); err != nil {
				t.Fatalf("Error: e2e.DeleteService %s: %q", svcSkipName, err)
			}
			t.Logf("GC service deleted (%s/%s)", sSkip.Namespace, svcSkipName)

			if err := e2e.WaitForStandaloneNegDeletion(ctx, s.ValidatorEnv.Cloud(), s, strconv.Itoa(int(porterPort)), *negStatus); err != nil {
				t.Fatalf("Error waiting for NEGDeletion: %v", err)
			}
		})
	})
}

func TestNoIstioASM(t *testing.T) {

	Framework.RunWithSandbox("TestASMConfigOnNoIstioCluster", t, func(t *testing.T, s *e2e.Sandbox) {
		if s.IstioEnabled() {
			t.Fatalf("This test must run against a cluster with Istio off.")
		}

		cm := map[string]string{"enable-asm": "true"}
		wantConfigMapEvents := []string{"ConfigMapConfigController: Get a update on the ConfigMapConfig, Restarting Ingress controller",
			"Cannot find DestinationRule CRD, disabling ASM Mode, please check Istio setup."}

		if err := e2e.EnsureConfigMap(s, asmConfigNamespace, asmConfigName, cm); err != nil {
			t.Error(err)
		}
		defer e2e.DeleteConfigMap(s, asmConfigNamespace, asmConfigName)
		if err := e2e.WaitConfigMapEvents(s, asmConfigNamespace, asmConfigName, wantConfigMapEvents, negControllerRestartTimeout); err != nil {
			t.Fatalf("Failed to get events: %v; Error %e", wantConfigMapEvents, err)
		}

		if err := wait.Poll(5*time.Second, 1*time.Minute, func() (bool, error) {
			cmData, err := e2e.GetConfigMap(s, asmConfigNamespace, asmConfigName)
			if err != nil {
				return false, err
			}
			if val, ok := cmData["asm-ready"]; ok && val == "false" {
				return true, nil
			}
			return false, fmt.Errorf("ConfigMap: %s/%s, asm-ready is not false. Value: %v", asmConfigNamespace, asmConfigName, cmData)

		}); err != nil {
			t.Fatalf("Failed to validate asm-ready = false. Error: %s", err)
		}

	})
}
