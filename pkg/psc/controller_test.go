/*
Copyright 2021 The Kubernetes Authors.

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
package psc

import (
	context2 "context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	ga "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/ingress-gce/pkg/annotations"
	sav1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	safake "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/namer"
	sautils "k8s.io/ingress-gce/pkg/utils/serviceattachment"
	"k8s.io/legacy-cloud-providers/gce"
	utilpointer "k8s.io/utils/pointer"
)

const (
	ClusterID     = "cluster-id"
	kubeSystemUID = "kube-system-uid"
	testNamespace = "test-namespace"
	ClusterName   = "test-cluster"
)

func TestServiceAttachmentCreation(t *testing.T) {
	saName := "my-sa"
	svcName := "my-service"
	validRef := v1.TypedLocalObjectReference{
		Kind: "service",
		Name: svcName,
	}
	frIPAddr := "1.2.3.4"

	testCases := []struct {
		desc                 string
		annotationKey        string
		legacySvc            bool
		svcExists            bool
		fwdRuleExists        bool
		connectionPreference string
		resourceRef          v1.TypedLocalObjectReference
		incorrectIPAddr      bool
		invalidSubnet        bool
		expectErr            bool
		proxyProtocol        bool
	}{
		{
			desc:                 "valid service attachment with tcp ILB",
			annotationKey:        annotations.TCPForwardingRuleKey,
			svcExists:            true,
			fwdRuleExists:        true,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			expectErr:            false,
		},
		{
			desc:                 "valid service attachment with udp ILB",
			annotationKey:        annotations.UDPForwardingRuleKey,
			svcExists:            true,
			fwdRuleExists:        true,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			expectErr:            false,
		},
		{
			desc:                 "legacy ILB service",
			legacySvc:            true,
			svcExists:            true,
			fwdRuleExists:        true,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			expectErr:            false,
		},
		{
			desc:                 "legacy ILB service, forwarding rule has wrong IP",
			legacySvc:            true,
			svcExists:            true,
			fwdRuleExists:        true,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			incorrectIPAddr:      true,
			expectErr:            true,
		},
		{
			desc:                 "forwarding rule has wrong IP",
			annotationKey:        annotations.TCPForwardingRuleKey,
			svcExists:            true,
			fwdRuleExists:        true,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			incorrectIPAddr:      true,
			expectErr:            true,
		},
		{
			desc:                 "service does not exist",
			svcExists:            false,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			expectErr:            true,
		},
		{
			desc:                 "forwarding rule does not exist",
			annotationKey:        annotations.TCPForwardingRuleKey,
			svcExists:            true,
			fwdRuleExists:        false,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			expectErr:            true,
		},
		{
			desc:                 "legacy ILB service, forwarding rule does not exist",
			legacySvc:            true,
			svcExists:            true,
			fwdRuleExists:        false,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			expectErr:            true,
		},
		{
			desc:                 "invalid resource reference",
			annotationKey:        annotations.TCPForwardingRuleKey,
			svcExists:            false,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef: v1.TypedLocalObjectReference{
				APIGroup: utilpointer.StringPtr("apiGroup"),
				Kind:     "not-service",
				Name:     svcName,
			},
			expectErr: true,
		},
		{
			desc:                 "valid resource reference with no api group",
			annotationKey:        annotations.TCPForwardingRuleKey,
			svcExists:            false,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef: v1.TypedLocalObjectReference{
				Kind: "service",
				Name: svcName,
			},
			expectErr: true,
		},
		{
			desc:                 "valid resource reference with kind=Service",
			annotationKey:        annotations.TCPForwardingRuleKey,
			svcExists:            false,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef: v1.TypedLocalObjectReference{
				Kind: "Service",
				Name: svcName,
			},
			expectErr: true,
		},
		{
			desc:                 "proxy protocol is true",
			annotationKey:        annotations.TCPForwardingRuleKey,
			svcExists:            true,
			fwdRuleExists:        true,
			connectionPreference: "ACCEPT_AUTOMATIC",
			resourceRef:          validRef,
			proxyProtocol:        true,
			expectErr:            false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController("ZONAL")
			fakeCloud := controller.cloud

			var frName string
			if tc.svcExists {
				ipAddr := frIPAddr
				if tc.incorrectIPAddr {
					ipAddr = "5.6.7.8"
				}

				var err error
				if tc.legacySvc {
					_, frName, err = createSvc(controller, svcName, "svc-uid", ipAddr, "")
					if err != nil {
						t.Errorf("%s", err)
					}
				} else {
					_, frName, err = createSvc(controller, svcName, "svc-uid", ipAddr, tc.annotationKey)
					if err != nil {
						t.Errorf("%s", err)
					}
				}
			}

			var rule *composite.ForwardingRule
			if tc.fwdRuleExists {
				var err error
				if rule, err = createForwardingRule(fakeCloud, frName, frIPAddr); err != nil {
					t.Errorf("%s", err)
				}
			}

			subnetURL := ""
			if !tc.invalidSubnet {
				if subnet, err := createNatSubnet(fakeCloud, "my-subnet"); err != nil {
					t.Errorf("%s", err)
				} else {
					subnetURL = subnet.SelfLink
				}
			}

			saURL := saName + "-url"
			saCR := &sav1.ServiceAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       testNamespace,
					Name:            saName,
					UID:             "service-attachment-uid",
					ResourceVersion: "resource-version",
					SelfLink:        saURL,
				},
				Spec: sav1.ServiceAttachmentSpec{
					ConnectionPreference: tc.connectionPreference,
					NATSubnets:           []string{"my-subnet"},
					ResourceRef:          tc.resourceRef,
					ProxyProtocol:        tc.proxyProtocol,
					ConsumerAllowList: []sav1.ConsumerProject{
						{
							ConnectionLimit: 100,
							Project:         "consumer-allow-project-1",
							ForceSendFields: []string{"field-1", "field-2", "field-3"},
							NullFields:      []string{"null-field-1"},
						},
						{
							ConnectionLimit: 80,
							Project:         "consumer-allow-project-2",
						},
						{
							Project: "consumer-allow-project-3",
						},
					},
					ConsumerRejectList: []string{
						"consumer-reject-project-1",
						"consumer-reject-project-2",
					},
				},
			}

			controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Create(context2.TODO(), saCR, metav1.CreateOptions{})
			syncServiceAttachmentLister(controller)

			err := controller.processServiceAttachment(SvcAttachmentKeyFunc(testNamespace, saName))
			if tc.expectErr && err == nil {
				t.Errorf("expected an error when process service attachment")
			} else if !tc.expectErr && err != nil {
				t.Errorf("unexpected error processing Service Attachment: %s", err)
			} else if !tc.expectErr {
				updatedCR, err := controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Get(context2.TODO(), saName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("unexpected error while querying for service attachment %s: %q", saName, err)
				}

				if err = verifyServiceAttachmentFinalizer(updatedCR); err != nil {
					t.Errorf("%s:%s", tc.desc, err)
				}

				if updatedCR.ResourceVersion != saCR.ResourceVersion {
					t.Error("Resource versions should not change when Service Attachment CR is updated")
				}

				gceSAName := controller.saNamer.ServiceAttachment(testNamespace, saName, string(saCR.UID))
				sa, err := getServiceAttachment(fakeCloud, gceSAName)
				if err != nil {
					t.Errorf("%s", err)
				}

				zone, err := fakeCloud.GetZone(context2.TODO())
				if err != nil {
					t.Errorf("failed to get zone %q", err)
				}
				desc := sautils.NewServiceAttachmentDesc(testNamespace, saName, ClusterName, zone.FailureDomain, false)

				expectedSA := &ga.ServiceAttachment{
					ConnectionPreference: tc.connectionPreference,
					Description:          desc.String(),
					Name:                 gceSAName,
					NatSubnets:           []string{subnetURL},
					TargetService:        rule.SelfLink,
					Region:               fakeCloud.Region(),
					SelfLink:             sa.SelfLink,
					EnableProxyProtocol:  tc.proxyProtocol,
					ConsumerAcceptLists: []*ga.ServiceAttachmentConsumerProjectLimit{
						{
							ConnectionLimit: 100,
							ProjectIdOrNum:  "consumer-allow-project-1",
							ForceSendFields: []string{"field-1", "field-2", "field-3"},
							NullFields:      []string{"null-field-1"},
						},
						{
							ConnectionLimit: 80,
							ProjectIdOrNum:  "consumer-allow-project-2",
						},
						{
							ProjectIdOrNum: "consumer-allow-project-3",
						},
					},
					ConsumerRejectLists: []string{
						"consumer-reject-project-1",
						"consumer-reject-project-2",
					},
				}

				if !reflect.DeepEqual(sa, expectedSA) {
					t.Errorf(" Expected service attachment resource to be \n%+v\n, but found \n%+v", expectedSA, sa)
				}

				if err = validateSAStatus(updatedCR.Status, sa, metav1.NewTime(time.Time{}), true); err != nil {
					t.Errorf("ServiceAttachment CR does not match expected: %s", err)
				}
			}
		})
	}
}

func TestServiceAttachmentConsumers(t *testing.T) {

	saName := "my-sa"
	svcName := "my-service"
	saUID := "service-attachment-uid"
	frIPAddr := "1.2.3.4"
	controller := newTestController("ZONAL")
	gceSAName := controller.saNamer.ServiceAttachment(testNamespace, saName, saUID)
	_, frName, err := createSvc(controller, svcName, "svc-uid", frIPAddr, annotations.TCPForwardingRuleKey)
	if err != nil {
		t.Errorf("%s", err)
	}
	rule, err := createForwardingRule(controller.cloud, frName, frIPAddr)
	if err != nil {
		t.Errorf("%s", err)
	}

	subnet, err := createNatSubnet(controller.cloud, "my-subnet")
	if err != nil {
		t.Errorf("%s", err)
	}

	saCR := testServiceAttachmentCR(saName, svcName, saUID, []string{"my-subnet"}, false, false)
	beforeTS := metav1.NewTime(time.Time{})
	saCR.Status.LastModifiedTimestamp = beforeTS
	_, err = controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Create(context2.TODO(), saCR, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service attachment cr: %q", err)
	}
	syncServiceAttachmentLister(controller)

	initialConsumerRules := []*ga.ServiceAttachmentConnectedEndpoint{
		{Endpoint: "consumer-fwd-rule-1", Status: "ACCEPTED"},
		{Endpoint: "consumer-fwd-rule-2", Status: "PENDING"},
	}

	updateConsumerRules := []*ga.ServiceAttachmentConnectedEndpoint{
		{Endpoint: "consumer-fwd-rule-1", Status: "ACCEPTED"},
		{Endpoint: "consumer-fwd-rule-2", Status: "PENDING"},
		{Endpoint: "consumer-fwd-rule-3", Status: "PENDING"},
	}

	desc := sautils.NewServiceAttachmentDesc(saCR.Namespace, saCR.Name, ClusterName, controller.cloud.Region(), true)
	expectedSA := &ga.ServiceAttachment{
		ConnectionPreference: saCR.Spec.ConnectionPreference,
		Description:          desc.String(),
		Name:                 gceSAName,
		NatSubnets:           []string{subnet.SelfLink},
		TargetService:        rule.SelfLink,
		Region:               controller.cloud.Region(),
		EnableProxyProtocol:  saCR.Spec.ProxyProtocol,
	}

	for _, consumerRules := range [][]*ga.ServiceAttachmentConnectedEndpoint{
		initialConsumerRules, updateConsumerRules} {
		expectedSA.ConnectedEndpoints = consumerRules
		err = insertServiceAttachment(controller.cloud, expectedSA)
		if err != nil {
			t.Errorf("errored adding consumer forwarding rules to gce service attachment: %q", err)
		}

		err = controller.processServiceAttachment(SvcAttachmentKeyFunc(testNamespace, saName))
		if err != nil {
			t.Errorf("unexpected error processing service attachment: %q", err)
		}

		updatedCR, err := controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Get(context2.TODO(), saName, metav1.GetOptions{})
		if err != nil {
			t.Errorf("unexpected error while querying for service attachment %s: %q", saName, err)
		}
		if err = validateSAStatus(updatedCR.Status, expectedSA, beforeTS, true); err != nil {
			t.Errorf("ServiceAttachment CR does not have correct consumers: %q", err)
		}

		// TODO(srepakula): Replace when mock allows updates to SA objects
		err = deleteServiceAttachment(controller.cloud, gceSAName)
		if err != nil {
			t.Errorf("errored deleting gce service attachment: %q", err)
		}
	}
}

func TestServiceAttachmentUpdate(t *testing.T) {
	saName := "my-sa"
	svcName := "my-service"
	otherServiceName := "my-other-service"
	saUID := "service-attachment-uid"
	frIPAddr := "1.2.3.4"

	subnet1 := "subnet-1"
	subnet2 := "subnet-2"
	subnet3 := "subnet-3"

	saCRWithAnnotation := testServiceAttachmentCR(saName, svcName, saUID, []string{subnet1, subnet2}, true, false)
	saCRWithAnnotation.Annotations = map[string]string{"some-key": "some-value"}

	testcases := []struct {
		desc            string
		updatedSACR     *sav1.ServiceAttachment
		expectSAUpdate  bool
		expectError     bool
		expectedSubnets []string
	}{
		{
			desc:            "update metadata annotation",
			updatedSACR:     saCRWithAnnotation,
			expectSAUpdate:  false,
			expectedSubnets: []string{subnet1, subnet2},
		},
		{
			desc:            "update forwarding rule",
			updatedSACR:     testServiceAttachmentCR(saName, otherServiceName, saUID, []string{subnet1, subnet2}, false, true),
			expectSAUpdate:  false,
			expectError:     true,
			expectedSubnets: []string{subnet1, subnet2},
		},
		{
			desc:            "update proxy protocol to false in the spec",
			updatedSACR:     testServiceAttachmentCR(saName, svcName, saUID, []string{subnet1, subnet2}, false, false),
			expectSAUpdate:  true,
			expectedSubnets: []string{subnet1, subnet2},
		},
		{ // though this case checks that the SA is updated, this would fail on the GCE update because subnets cannot be removed
			desc:            "update one of the subnets",
			updatedSACR:     testServiceAttachmentCR(saName, svcName, saUID, []string{subnet1, subnet3}, false, true),
			expectSAUpdate:  true,
			expectedSubnets: []string{subnet1, subnet3},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController("REGIONAL")
			gceSAName := controller.saNamer.ServiceAttachment(testNamespace, saName, saUID)
			_, frName, err := createSvc(controller, svcName, "svc-uid", frIPAddr, annotations.TCPForwardingRuleKey)
			if err != nil {
				t.Errorf("%s", err)
			}
			if _, err = createForwardingRule(controller.cloud, frName, frIPAddr); err != nil {
				t.Errorf("%s", err)
			}

			createdSubnets := map[string]string{}
			for _, subnetName := range []string{subnet1, subnet2, subnet3} {
				subnet, err := createNatSubnet(controller.cloud, subnetName)
				if err != nil {
					t.Errorf("%s", err)
				}
				createdSubnets[subnetName] = subnet.SelfLink
			}

			var expectedSubnetURLs []string
			for _, subnetName := range tc.expectedSubnets {
				expectedSubnetURLs = append(expectedSubnetURLs, createdSubnets[subnetName])
			}

			saCR := testServiceAttachmentCR(saName, svcName, saUID, []string{subnet1, subnet2}, false, false)
			_, err = controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Create(context2.TODO(), saCR, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create service attachment cr: %q", err)
			}
			syncServiceAttachmentLister(controller)

			if err := controller.processServiceAttachment(SvcAttachmentKeyFunc(testNamespace, saName)); err != nil {
				t.Fatalf("Unexpected error while processing ServiceAttachment: %q", err)
			}

			createdSA, err := getServiceAttachment(controller.cloud, gceSAName)
			if err != nil {
				t.Fatalf("Unexpected error when getting GCE ServiceAttachment: %q", err)
			}

			if saCR.Spec.ResourceRef.Name != tc.updatedSACR.Spec.ResourceRef.Name {
				if _, frName, err = createSvc(controller, tc.updatedSACR.Spec.ResourceRef.Name, "svc-uid", frIPAddr, annotations.TCPForwardingRuleKey); err != nil {
					t.Fatalf("%s", err)
				}
				if _, err = createForwardingRule(controller.cloud, frName, frIPAddr); err != nil {
					t.Fatalf("%s", err)
				}
			}

			processedCR, err := controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Get(context2.TODO(), saName, metav1.GetOptions{})
			syncServiceAttachmentLister(controller)
			if err != nil {
				t.Fatalf("Failed to get processed SA CR: %q", err)
			}

			beforeTS := metav1.NewTime(time.Time{})
			tc.updatedSACR.Status = processedCR.Status
			tc.updatedSACR.Status.LastModifiedTimestamp = beforeTS

			_, err = controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Update(context2.TODO(), tc.updatedSACR, metav1.UpdateOptions{})
			syncServiceAttachmentLister(controller)
			if err != nil {
				t.Fatalf("Failed to add tc.updatedSACR to store: %q", err)
			}
			err = controller.processServiceAttachment(SvcAttachmentKeyFunc(testNamespace, saName))
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error while processing updated ServiceAttachment: %q", err)
			} else if tc.expectError && err == nil {
				t.Errorf("Expected an error while processing updated ServiceAttachment")
			}

			var expectedSA *ga.ServiceAttachment
			if !tc.expectSAUpdate {
				expectedSA = createdSA
			} else {
				expectedSA = &ga.ServiceAttachment{
					ConnectionPreference: saCR.Spec.ConnectionPreference,
					Description:          createdSA.Description,
					Name:                 gceSAName,
					NatSubnets:           expectedSubnetURLs,
					TargetService:        createdSA.TargetService,
					Region:               controller.cloud.Region(),
					EnableProxyProtocol:  tc.updatedSACR.Spec.ProxyProtocol,
					SelfLink:             createdSA.SelfLink,
				}
			}
			updatedSA, err := getServiceAttachment(controller.cloud, gceSAName)
			if err != nil {
				t.Fatalf("Unexpected error when getting updated GCE ServiceAttachment: %q", err)
			}

			if !reflect.DeepEqual(updatedSA, expectedSA) {
				t.Errorf("Incorrect GCE Service Attachment \ngot SA:\n%+v,\nwant:\n%+v", updatedSA, expectedSA)
			}

			updatedCR, err := controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).Get(context2.TODO(), saName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get service attachment cr: %q", err)
			}

			// No changes in the GCE SA status so CR status should not be updated
			if err = validateSAStatus(updatedCR.Status, expectedSA, beforeTS, false); err != nil {
				t.Errorf("ServiceAttachment CR does not have correct status: %q", err)
			}
		})
	}
}

func TestNeedsUpdate(t *testing.T) {
	subnet1 := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-1"
	subnet2 := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-2"
	subnet3 := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-3"

	targetService1 := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/forwardingRules/fr-1"
	targetService2 := "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/forwardingRules/fr-2"

	originalSA := &ga.ServiceAttachment{
		ConnectionPreference: "ACCEPT_AUTOMATIC",
		Description:          "my-desc",
		Name:                 "my-sa",
		NatSubnets:           []string{subnet1, subnet2},
		TargetService:        targetService1,
		Region:               "region",
		EnableProxyProtocol:  true,
		SelfLink:             "self-link",
	}

	saDiffService := &ga.ServiceAttachment{}
	*saDiffService = *originalSA
	saDiffService.TargetService = targetService2

	saDiffSubnets := &ga.ServiceAttachment{}
	*saDiffSubnets = *originalSA
	saDiffSubnets.NatSubnets = []string{subnet1, subnet3}

	saAddSubnet := &ga.ServiceAttachment{}
	*saAddSubnet = *originalSA
	saAddSubnet.NatSubnets = []string{subnet1, subnet2, subnet3}

	saDiffSpec := &ga.ServiceAttachment{}
	*saDiffSpec = *originalSA
	saDiffSpec.EnableProxyProtocol = false

	saNoChange := &ga.ServiceAttachment{}
	*saNoChange = *originalSA

	testcases := []struct {
		desc         string
		newSA        *ga.ServiceAttachment
		expectError  bool
		expectUpdate bool
	}{
		{
			desc:         "change the target service/forwarding rule",
			newSA:        saDiffService,
			expectError:  true,
			expectUpdate: true,
		},
		{
			desc:         "change a subnet",
			newSA:        saDiffSubnets,
			expectError:  false,
			expectUpdate: true,
		},
		{
			desc:         "add a subnet",
			newSA:        saAddSubnet,
			expectError:  false,
			expectUpdate: true,
		},
		{
			desc:         "change spec",
			newSA:        saDiffSpec,
			expectError:  false,
			expectUpdate: true,
		},
		{
			desc:         "no change",
			newSA:        saNoChange,
			expectError:  false,
			expectUpdate: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			needsUpdate, err := needsUpdate(originalSA, tc.newSA)
			if tc.expectError && err == nil {
				t.Errorf("expected an error but got none")
			} else if !tc.expectError && err != nil {
				t.Errorf("unexpected error: %q", err)
			}

			if needsUpdate != tc.expectUpdate {
				t.Errorf("needsUpdate returned %t, expected %t", needsUpdate, tc.expectUpdate)
			}
		})
	}
}

func TestServiceAttachmentGarbageCollection(t *testing.T) {
	svcNamePrefix := "my-service"
	saUIDPrefix := "service-attachment-uid"
	frIPAddr := "1.2.3.4"

	testcases := []struct {
		desc           string
		deleteError    error
		getError       error
		invalidSAURL   bool
		saURL          string
		expectDeletion bool
	}{
		{
			desc:           "regular service attachment deletion",
			expectDeletion: true,
		},
		{
			desc:           "service attachment not found error during gc",
			getError:       test.FakeGoogleAPINotFoundErr(),
			expectDeletion: true,
		},
		{
			desc:           "service attachment not found error during gc",
			getError:       &googleapi.Error{Code: http.StatusBadRequest},
			expectDeletion: true,
		},
		{
			desc:           "service attachment not found error during gc",
			getError:       fmt.Errorf("deletion error"),
			expectDeletion: false,
		},
		{
			desc:           "service attachment cr has an empty url",
			invalidSAURL:   true,
			saURL:          "",
			expectDeletion: true,
		},
		{
			desc:           "service attachment cr has a malformed url",
			invalidSAURL:   true,
			saURL:          "malformed-url",
			expectDeletion: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {

			controller := newTestController("ZONAL")

			if _, err := createNatSubnet(controller.cloud, "my-subnet"); err != nil {
				t.Errorf("failed to create subnet: %s", err)
			}

			// create a serviceAttachment that should not be deleted as part of GC
			saToKeep := testServiceAttachmentCR("sa-to-keep", svcNamePrefix+"-keep", saUIDPrefix+"-keep", []string{"my-subnet"}, true, false)
			saToBeDeleted := testServiceAttachmentCR("sa-to-be-deleted", svcNamePrefix+"-deleted", saUIDPrefix+"-deleted", []string{"my-subnet"}, true, false)
			for _, sa := range []*sav1.ServiceAttachment{saToKeep, saToBeDeleted} {
				svcName := sa.Spec.ResourceRef.Name
				svc, frName, err := createSvc(controller, svcName, string(sa.UID), frIPAddr, annotations.TCPForwardingRuleKey)
				if err != nil {
					t.Errorf("%s", err)
				}
				controller.serviceLister.Add(svc)

				if _, err = createForwardingRule(controller.cloud, frName, frIPAddr); err != nil {
					t.Errorf("%s", err)
				}

				_, err = controller.saClient.NetworkingV1().ServiceAttachments(sa.Namespace).Create(context2.TODO(), sa, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("failed to add service attachment to client: %q", err)
				}

				syncServiceAttachmentLister(controller)

				if err = controller.processServiceAttachment(SvcAttachmentKeyFunc(sa.Namespace, sa.Name)); err != nil {
					t.Fatalf("failed to process service attachment: %q", err)
				}
			}

			deletionTS := metav1.Now()
			saCR, err := controller.saClient.NetworkingV1().ServiceAttachments(saToBeDeleted.Namespace).Get(context2.TODO(), saToBeDeleted.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get service attachment cr: %q", err)
			}

			saCR.DeletionTimestamp = &deletionTS
			if tc.invalidSAURL {
				saCR.Status.ServiceAttachmentURL = tc.saURL
			}
			_, err = controller.saClient.NetworkingV1().ServiceAttachments(saToBeDeleted.Namespace).Update(context2.TODO(), saCR, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("failed to update service attachment to client: %q", err)
			}

			// sync the controller cache to have current set of serviceAttachments
			syncServiceAttachmentLister(controller)

			if tc.getError != nil || tc.deleteError != nil {

				fakeGCE := controller.cloud.Compute().(*cloud.MockGCE)
				mockSA := fakeGCE.ServiceAttachments().(*cloud.MockServiceAttachments)

				gceSAName := controller.saNamer.ServiceAttachment(saToBeDeleted.Namespace, saToBeDeleted.Name, string(saToBeDeleted.UID))
				saKey, _ := composite.CreateKey(controller.cloud, gceSAName, meta.Regional)

				if tc.getError != nil {
					mockSA.GetError[*saKey] = tc.getError
				}

				if tc.deleteError != nil {
					mockSA.DeleteError[*saKey] = tc.deleteError
				}
			}

			controller.garbageCollectServiceAttachments()

			if tc.expectDeletion {
				gceSAName := controller.saNamer.ServiceAttachment(saToBeDeleted.Namespace, saToBeDeleted.Name, string(saToBeDeleted.UID))
				if err := verifyGCEServiceAttachmentDeletion(controller, saToBeDeleted); err != nil {
					t.Errorf("Expected gce sa %s to be deleted : %s", gceSAName, err)
				}

				if err := verifyServiceAttachmentCRDeletion(controller, saToBeDeleted); err != nil {
					t.Errorf("Expected sa %s to be deleted : %s", saToBeDeleted.Name, err)
				}
			} else {
				currSA, err := controller.saClient.NetworkingV1().ServiceAttachments(saToBeDeleted.Namespace).Get(context2.TODO(), saToBeDeleted.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("failed to query for service attachment %s from client: %q", currSA.Name, err)
				}

				if err := verifyServiceAttachmentFinalizer(currSA); err != nil {
					t.Errorf("service attachment %s finalizer should not be removed after gc: %q", currSA.Name, err)
				}
			}

			// verify saToKeep was not deleted
			currSA, err := controller.saClient.NetworkingV1().ServiceAttachments(saToKeep.Namespace).Get(context2.TODO(), saToKeep.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to query for service attachment %s from client: %q", saToKeep.Name, err)
			}

			if err = verifyServiceAttachmentFinalizer(currSA); err != nil {
				t.Errorf("service attachment %s finalizer should not be removed after gc: %q", saToKeep.Name, err)
			}

			gceSAName := controller.saNamer.ServiceAttachment(saToKeep.Namespace, saToKeep.Name, string(saToKeep.UID))
			gceSA, err := getServiceAttachment(controller.cloud, gceSAName)
			if err != nil {
				t.Errorf("Unexpected error when getting updated GCE ServiceAttachment: %q", err)
			}

			if gceSA == nil {
				t.Errorf("service attachment %s should not have been gc'd", saToKeep.Name)
			}
		})
	}
}

func TestShouldProcess(t *testing.T) {
	now := metav1.Now()
	originalSA := testServiceAttachmentCR("sa", "my-service", "service-attachment-uid", []string{"my-subnet"}, true, false)

	deletedSA := originalSA.DeepCopy()
	deletedSA.SetDeletionTimestamp(&now)

	metadataSA := originalSA.DeepCopy()
	metadataSA.Labels = map[string]string{"key": "value"}

	metadataWithStatusSA := metadataSA.DeepCopy()
	metadataWithStatusSA.Status.LastModifiedTimestamp = metav1.Now()

	specSA := originalSA.DeepCopy()
	specSA.Spec.ConnectionPreference = "some-connection-pref"

	statusSA := originalSA.DeepCopy()
	statusSA.Status.LastModifiedTimestamp = metav1.Now()

	testcases := []struct {
		desc          string
		newSA         *sav1.ServiceAttachment
		shouldProcess bool
	}{
		{
			desc:          "cr has been deleted",
			newSA:         deletedSA,
			shouldProcess: false,
		},
		{
			desc:          "metadata has been updated and status has not changed",
			newSA:         metadataSA,
			shouldProcess: true,
		},
		{
			desc:          "metadata and status has been updated",
			newSA:         metadataWithStatusSA,
			shouldProcess: false,
		},
		{
			desc:          "No changes were made like in a periodic sync",
			newSA:         originalSA,
			shouldProcess: true,
		},
		{
			desc:          "Spec has changed",
			newSA:         specSA,
			shouldProcess: true,
		},
		{
			desc:          "only status has changed",
			newSA:         statusSA,
			shouldProcess: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			processSA := shouldProcess(originalSA, tc.newSA)
			if processSA != tc.shouldProcess {
				t.Errorf("Expected shouldProcess to return %t, but got %t", tc.shouldProcess, processSA)
			}
		})
	}
}

func TestFilterError(t *testing.T) {
	unclassifiedError := errors.New("unclassified error")
	badGatewayErr := &googleapi.Error{Code: http.StatusBadGateway}
	wrappedBadGatewayErr := fmt.Errorf("wrap 1: %w", fmt.Errorf("wrap 2: %w", badGatewayErr))
	testcases := []struct {
		desc          string
		err           error
		expectedError error
	}{
		{
			desc:          "google api status not found error",
			err:           test.FakeGoogleAPINotFoundErr(),
			expectedError: nil,
		},
		{
			desc:          "google api bad request error",
			err:           &googleapi.Error{Code: http.StatusBadRequest},
			expectedError: nil,
		},
		{
			desc:          "google api bad gateway error",
			err:           badGatewayErr,
			expectedError: badGatewayErr,
		},
		{
			desc:          "service not found",
			err:           ServiceNotFoundError,
			expectedError: nil,
		},
		{
			desc:          "mismatched ilb ip",
			err:           MismatchedILBIPError,
			expectedError: nil,
		},
		{
			desc:          "wrapped google api status not found error",
			err:           fmt.Errorf("wrap 1: %w", fmt.Errorf("wrap 2: %w", test.FakeGoogleAPINotFoundErr())),
			expectedError: nil,
		},
		{
			desc:          "wrapped google api bad request error",
			err:           fmt.Errorf("wrap 1: %w", fmt.Errorf("wrap 2: %w", &googleapi.Error{Code: http.StatusBadRequest})),
			expectedError: nil,
		},
		{
			desc:          "wrapped google api bad gateway error",
			err:           wrappedBadGatewayErr,
			expectedError: wrappedBadGatewayErr,
		},
		{
			desc:          "wrapped service not found",
			err:           fmt.Errorf("wrap 1: %w", fmt.Errorf("wrap 2: %w", ServiceNotFoundError)),
			expectedError: nil,
		},
		{
			desc:          "wrapped mismatched ilb ip",
			err:           fmt.Errorf("wrap 1: %w", fmt.Errorf("wrap 2: %w", MismatchedILBIPError)),
			expectedError: nil,
		},
		{
			desc:          "unclassified error",
			err:           unclassifiedError,
			expectedError: unclassifiedError,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			resultErr := filterError(tc.err)
			if resultErr != tc.expectedError {
				t.Errorf("Expected filterError(%+v) to return %+v but got %+v", tc.err, tc.expectedError, resultErr)
			}
		})
	}
}

func TestGetSubnetURLs(t *testing.T) {
	testcases := []struct {
		desc           string
		subnet         string
		createSubnet   bool
		expectErr      bool
		expectedSubnet string
	}{
		{
			desc:           "subnet is in the host project",
			subnet:         "my-subnet",
			createSubnet:   true,
			expectErr:      false,
			expectedSubnet: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/my-subnet",
		},
		{
			desc:           "subnet is the resource url path",
			subnet:         "projects/test-project/regions/us-central1/subnetworks/subnet-1",
			createSubnet:   false,
			expectErr:      false,
			expectedSubnet: "projects/test-project/regions/us-central1/subnetworks/subnet-1",
		},
		{
			desc:           "subnet is a fully qualified resource url",
			subnet:         "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-1",
			createSubnet:   false,
			expectErr:      false,
			expectedSubnet: "https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/subnetworks/subnet-1",
		},
		{
			desc:         "subnet does not exist in host project",
			subnet:       "my-subnet",
			createSubnet: false,
			expectErr:    true,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController("ZONAL")

			if tc.createSubnet {
				_, err := createNatSubnet(controller.cloud, tc.subnet)
				if err != nil {
					t.Errorf("failed to create nat subnet: %s", err)
				}
			}

			subnets, err := controller.getSubnetURLs([]string{tc.subnet})
			if err == nil && tc.expectErr {
				t.Fatalf("getSubnetURLs() returned no error, but expected an error")
			} else if err != nil && !tc.expectErr {
				t.Fatalf("getSubnetURLs() returned unexpected error: %s", err)
			}

			if tc.expectedSubnet != "" && !tc.expectErr {
				if len(subnets) != 1 {
					t.Fatalf("got %d subnets, should be 1", len(subnets))
				}

				if subnets[0] != tc.expectedSubnet {
					t.Fatalf("getSubnetURLs() returned %s, but wanted %s", subnets[0], tc.expectedSubnet)
				}
			}
		})
	}
}

// newTestController returns a test psc controller
func newTestController(clusterType string) *Controller {
	kubeClient := fake.NewSimpleClientset()
	gceClient := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	fakeGCE := gceClient.Compute().(*cloud.MockGCE)
	mockSA := fakeGCE.ServiceAttachments().(*cloud.MockServiceAttachments)

	// Mock has a noop for the Patch. Add the patch hook so GCE SA gets updated when mock patch is called
	mockSA.PatchHook = func(ctx context2.Context, key *meta.Key, sa *ga.ServiceAttachment, fake *cloud.MockServiceAttachments) error {
		if !key.Valid() {
			return fmt.Errorf("PatchHook: invalid GCE key (%+v)", key)
		}

		sa.Name = key.Name
		projectID := fake.ProjectRouter.ProjectID(ctx, "v1", "serviceAttachments")
		sa.SelfLink = cloud.SelfLink(meta.VersionGA, projectID, "serviceAttachments", key)

		originalObj, ok := fake.Objects[*key]
		if ok {
			originalSA := originalObj.Obj.(*ga.ServiceAttachment)
			if originalSA.Fingerprint != sa.Fingerprint {
				return fmt.Errorf("Service Attachment resource fingerprint does not match")
			}
		}

		fake.Objects[*key] = &cloud.MockServiceAttachmentsObj{Obj: sa}
		return nil
	}

	resourceNamer := namer.NewNamer(ClusterID, "")
	saClient := safake.NewSimpleClientset()

	ctxConfig := context.ControllerContextConfig{
		Namespace:             v1.NamespaceAll,
		ResyncPeriod:          1 * time.Minute,
		DefaultBackendSvcPort: test.DefaultBeSvcPort,
		HealthCheckPath:       "/",
	}

	flags.F.GKEClusterName = ClusterName
	flags.F.GKEClusterType = clusterType
	ctx := context.NewControllerContext(nil, kubeClient, nil, nil, nil, nil, saClient, gceClient, resourceNamer, kubeSystemUID, ctxConfig)

	return NewController(ctx)
}

// createSvc creates a test K8s Service resource and adds it to the controller's svcAttachmentLister. If forwardingRuleKey is empty, no annotations will be added to the service.
func createSvc(controller *Controller, svcName, svcUID, ipAddr, forwardingRuleKey string) (*v1.Service, string, error) {
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      svcName,
			UID:       types.UID(svcUID),
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{
					{
						IP: ipAddr,
					},
				},
			},
		},
	}
	frName := cloudprovider.DefaultLoadBalancerName(svc)
	if forwardingRuleKey != "" {
		frName = svcName + "-fr"
		svc.Annotations = map[string]string{
			forwardingRuleKey: frName,
		}
	}
	return svc, frName, controller.serviceLister.Add(svc)
}

// testServiceAttachmentCR creates a test ServiceAttachment CR with the provided name, uid and subnets
func testServiceAttachmentCR(saName, svcName, svcUID string, subnets []string, withFinalizer, proxyProtocol bool) *sav1.ServiceAttachment {
	cr := &sav1.ServiceAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      saName,
			UID:       types.UID(svcUID),
		},
		Spec: sav1.ServiceAttachmentSpec{
			ConnectionPreference: "ACCEPT_AUTOMATIC",
			NATSubnets:           subnets,
			ResourceRef: v1.TypedLocalObjectReference{
				Kind: "service",
				Name: svcName,
			},
			ProxyProtocol: proxyProtocol,
		},
	}

	if withFinalizer {
		cr.Finalizers = []string{ServiceAttachmentFinalizerKey}
	}
	return cr
}

// createForwardingRule will create a forwarding rule with the provided name and ip address
func createForwardingRule(c *gce.Cloud, frName, ipAddr string) (*composite.ForwardingRule, error) {
	key, err := composite.CreateKey(c, frName, meta.Regional)
	if err != nil {
		return nil, fmt.Errorf("Unexpected error when creating key: %q", err)
	}
	// Create a ForwardingRule that matches
	fwdRule := &composite.ForwardingRule{
		Name:                frName,
		LoadBalancingScheme: string(cloud.SchemeInternal),
		IPAddress:           ipAddr,
	}
	if err = composite.CreateForwardingRule(c, key, fwdRule); err != nil {
		return nil, fmt.Errorf("Failed to create fake forwarding rule %s:  %q", frName, err)
	}

	rule, err := composite.GetForwardingRule(c, key, meta.VersionGA)
	if err != nil {
		return rule, fmt.Errorf("Failed to get forwarding rule: %q", err)
	}
	return rule, nil
}

// createNatSubnet will create a subnet with the provided name
func createNatSubnet(c *gce.Cloud, natSubnet string) (*ga.Subnetwork, error) {
	key, err := composite.CreateKey(c, natSubnet, meta.Regional)
	if err != nil {
		return nil, fmt.Errorf("Unexpected error when creating key: %q", err)
	}
	// Create a ForwardingRule that matches
	subnet := &ga.Subnetwork{
		Name: natSubnet,
	}
	if err = c.Compute().Subnetworks().Insert(context2.TODO(), key, subnet); err != nil {
		return nil, fmt.Errorf("Failed to create fake subnet %s:  %q", natSubnet, err)
	}

	subnet, err = c.Compute().Subnetworks().Get(context2.TODO(), key)
	if err != nil {
		return subnet, fmt.Errorf("Failed to get forwarding rule: %q", err)
	}
	return subnet, nil
}

// getServiceAttachment queries for the Service Attachment resource in GCE
func getServiceAttachment(cloud *gce.Cloud, saName string) (*ga.ServiceAttachment, error) {
	saKey, err := composite.CreateKey(cloud, saName, meta.Regional)
	if err != nil {
		return nil, fmt.Errorf("errored creating a key for service attachment: %q", err)
	}
	sa, err := cloud.Compute().ServiceAttachments().Get(context2.TODO(), saKey)
	if err != nil {
		return nil, fmt.Errorf("errored querying for service attachment: %q", err)
	}
	return sa, nil
}

// insertServiceAttachment inserts the given Service Attachment resource in GCE
func insertServiceAttachment(cloud *gce.Cloud, sa *ga.ServiceAttachment) error {
	saKey, err := composite.CreateKey(cloud, sa.Name, meta.Regional)
	if err != nil {
		return fmt.Errorf("errored creating a key for service attachment: %q", err)
	}
	err = cloud.Compute().ServiceAttachments().Insert(context2.TODO(), saKey, sa)
	if err != nil {
		return fmt.Errorf("errored inserting gce service attachment: %q", err)
	}
	return nil
}

// deleteServiceAttachment deletes the ServiceAttachment in GCE
func deleteServiceAttachment(cloud *gce.Cloud, name string) error {
	saKey, err := composite.CreateKey(cloud, name, meta.Regional)
	if err != nil {
		return fmt.Errorf("errored creating a key for service attachment: %q", err)
	}
	err = cloud.Compute().ServiceAttachments().Delete(context2.TODO(), saKey)
	if err != nil {
		return fmt.Errorf("errored deleting gce service attachment: %q", err)
	}
	return nil
}

// validateSAStatus validates that the status reports the same information as on the
// GCE service attachment resource
func validateSAStatus(status sav1.ServiceAttachmentStatus, sa *ga.ServiceAttachment, beforeTS metav1.Time, expectStatusUpdate bool) error {
	if status.ServiceAttachmentURL != sa.SelfLink {
		return fmt.Errorf("ServiceAttachment.Status.ServiceAttachmentURL was %s, but should be %s", status.ServiceAttachmentURL, sa.SelfLink)
	}

	if status.ForwardingRuleURL != sa.TargetService {
		return fmt.Errorf("ServiceAttachment.Status.ForwardingRuleURL was %s, but should be %s", status.ForwardingRuleURL, sa.TargetService)
	}

	if len(sa.ConnectedEndpoints) != len(status.ConsumerForwardingRules) {
		return fmt.Errorf("ServiceAttachment.Status.ConsumerForwardingRules has %d rules, expected %d", len(status.ConsumerForwardingRules), len(sa.ConnectedEndpoints))
	}
	for _, expectedConsumer := range sa.ConnectedEndpoints {
		foundConsumer := false
		for _, consumer := range status.ConsumerForwardingRules {
			if expectedConsumer.Endpoint == consumer.ForwardingRuleURL &&
				expectedConsumer.Status == consumer.Status {
				foundConsumer = true
			}
		}
		if !foundConsumer {
			return fmt.Errorf("ServiceAttachment.Status.ConsumerForwardingRules did not have %+v", expectedConsumer)
		}
	}

	if expectStatusUpdate && !beforeTS.Before(&status.LastModifiedTimestamp) {
		return fmt.Errorf("ServiceAttachment CR Status should update timestamp after status update. Before: %s, Status: %s",
			beforeTS.UTC().String(), status.LastModifiedTimestamp.UTC().String())
	} else if !expectStatusUpdate && !beforeTS.Equal(&status.LastModifiedTimestamp) {
		return fmt.Errorf("ServiceAttachment CR Status should not update timestamp when status update is needed.")
	}
	return nil
}

// verifyServiceAttachmentFinalizer verifies that the provided ServiceAttachment CR
// has the ServiceAttachmentFinalizerKey, otherwise it will return an error
func verifyServiceAttachmentFinalizer(cr *sav1.ServiceAttachment) error {
	finalizers := cr.GetFinalizers()
	if len(finalizers) != 1 {
		return fmt.Errorf("Expected service attachment to have one finalizer, has %d", len(finalizers))
	} else {
		if finalizers[0] != ServiceAttachmentFinalizerKey {
			return fmt.Errorf("Expected service attachment to have finalizer %s, but found %s", ServiceAttachmentFinalizerKey, finalizers[0])
		}
	}
	return nil
}

// syncServiceAttachmentLister will add all the current ServiceAttachment CRs in the Kubernetes
// client to the controller's svcAttachmentLister
func syncServiceAttachmentLister(controller *Controller) error {
	crs, err := controller.saClient.NetworkingV1().ServiceAttachments(testNamespace).List(context2.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, cr := range crs.Items {
		saCR := cr
		if err = controller.svcAttachmentLister.Add(&saCR); err != nil {
			return err
		}
	}
	return nil
}

// verifyServiceAttachmentCRDeletion will verify that the provided ServiceAttachment CR
// does not have the service attachment finalizer and that the deletion timestamp has been
// set
func verifyServiceAttachmentCRDeletion(controller *Controller, sa *sav1.ServiceAttachment) error {
	currCR, err := controller.saClient.NetworkingV1().ServiceAttachments(sa.Namespace).Get(context2.TODO(), sa.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to query for service attachment from client: %q", err)
	}

	if currCR.DeletionTimestamp.IsZero() {
		return fmt.Errorf("deletion timestamp is not set on %s", sa.Name)
	}

	if err = verifyServiceAttachmentFinalizer(currCR); err == nil {
		return fmt.Errorf("service attachment %s finalizer should be removed after gc", sa.Name)
	}
	return nil
}

// verifyGCEServiceAttachmentDeletion verifies that the provided CR's corresponding GCE
// Service Attachment resource has been deleted
func verifyGCEServiceAttachmentDeletion(controller *Controller, sa *sav1.ServiceAttachment) error {
	gceSAName := controller.saNamer.ServiceAttachment(sa.Namespace, sa.Name, string(sa.UID))
	gceSA, err := getServiceAttachment(controller.cloud, gceSAName)
	if err == nil {
		return fmt.Errorf("Expected error not found when getting GCE ServiceAttachment for SA CR %s", sa.Name)
	}

	if gceSA != nil {
		return fmt.Errorf("Service attachment: %q should have been deleted", gceSAName)
	}
	return nil
}
