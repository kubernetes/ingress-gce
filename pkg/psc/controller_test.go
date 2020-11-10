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
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	alpha "google.golang.org/api/compute/v0.alpha"
	ga "google.golang.org/api/compute/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/ingress-gce/pkg/annotations"
	sav1alpha1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1alpha1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
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
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController()
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
			saCR := &sav1alpha1.ServiceAttachment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       testNamespace,
					Name:            saName,
					UID:             "service-attachment-uid",
					ResourceVersion: "resource-version",
					SelfLink:        saURL,
				},
				Spec: sav1alpha1.ServiceAttachmentSpec{
					ConnectionPreference: tc.connectionPreference,
					NATSubnets:           []string{"my-subnet"},
					ResourceRef:          tc.resourceRef,
				},
			}

			controller.svcAttachmentLister.Add(saCR)
			controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).Create(context2.TODO(), saCR, metav1.CreateOptions{})

			err := controller.processServiceAttachment(SvcAttachmentKeyFunc(testNamespace, saName))
			if tc.expectErr && err == nil {
				t.Errorf("expected an error when process service attachment")
			} else if !tc.expectErr && err != nil {
				t.Errorf("unexpected error processing Service Attachment: %s", err)
			} else if !tc.expectErr {
				updatedCR, err := controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).Get(context2.TODO(), saName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("unexpected error while querying for service attachment %s: %q", saName, err)
				}

				if updatedCR.ResourceVersion != saCR.ResourceVersion {
					t.Error("Resource versions should not change when Service Attachment CR is updated")
				}

				gceSAName := controller.saNamer.ServiceAttachment(testNamespace, saName, string(saCR.UID))
				sa, err := getServiceAttachment(fakeCloud, gceSAName)
				if err != nil {
					t.Errorf("%s", err)
				}

				desc := sautils.ServiceAttachmentDesc{URL: saURL}

				expectedSA := &alpha.ServiceAttachment{
					ConnectionPreference:   tc.connectionPreference,
					Description:            desc.String(),
					Name:                   gceSAName,
					NatSubnets:             []string{subnetURL},
					ProducerForwardingRule: rule.SelfLink,
					Region:                 fakeCloud.Region(),
					SelfLink:               sa.SelfLink,
				}

				if !reflect.DeepEqual(sa, expectedSA) {
					t.Errorf(" Expected service attachment resource to be \n%+v\n, but found \n%+v", expectedSA, sa)
				}

				if err = validateSAStatus(updatedCR.Status, sa); err != nil {
					t.Errorf("ServiceAttachment CR does not match expected: %s", err)
				}
			}
		})
	}
}

func TestServiceAttachmentUpdate(t *testing.T) {
	saName := "my-sa"
	svcName := "my-service"
	saUID := "serivce-attachment-uid"
	frIPAddr := "1.2.3.4"

	saCRAnnotation := testServiceAttachmentCR(saName, svcName, saUID, []string{"my-subnet"})
	saCRAnnotation.Annotations = map[string]string{"some-key": "some-value"}

	testcases := []struct {
		desc        string
		updatedSACR *sav1alpha1.ServiceAttachment
		expectErr   bool
	}{
		{
			desc:        "updated annotation",
			updatedSACR: saCRAnnotation,
			expectErr:   false,
		},
		{
			desc:        "updated subnet",
			updatedSACR: testServiceAttachmentCR(saName, svcName, saUID, []string{"diff-subnet"}),
			expectErr:   true,
		},
		{
			desc:        "updated service name",
			updatedSACR: testServiceAttachmentCR(saName, "my-second-service", saUID, []string{"my-subnet"}),
			expectErr:   true,
		},
		{
			desc:        "removed subnet",
			updatedSACR: testServiceAttachmentCR(saName, svcName, saUID, []string{}),
			expectErr:   true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			controller := newTestController()
			gceSAName := controller.saNamer.ServiceAttachment(testNamespace, saName, saUID)
			_, frName, err := createSvc(controller, svcName, "svc-uid", frIPAddr, annotations.TCPForwardingRuleKey)
			if err != nil {
				t.Errorf("%s", err)
			}
			if _, err = createForwardingRule(controller.cloud, frName, frIPAddr); err != nil {
				t.Errorf("%s", err)
			}

			if _, err := createNatSubnet(controller.cloud, "my-subnet"); err != nil {
				t.Errorf("%s", err)
			}

			saCR := testServiceAttachmentCR(saName, svcName, saUID, []string{"my-subnet"})
			err = controller.svcAttachmentLister.Add(saCR)
			if err != nil {
				t.Fatalf("Failed to add service attachment cr to store: %q", err)
			}
			_, err = controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).Create(context2.TODO(), saCR, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("Failed to create service attachment cr: %q", err)
			}

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

			err = controller.svcAttachmentLister.Add(tc.updatedSACR)
			if err != nil {
				t.Fatalf("Failed to add tc.updatedSACR to store: %q", err)
			}
			err = controller.processServiceAttachment(SvcAttachmentKeyFunc(testNamespace, saName))
			if tc.expectErr && err == nil {
				t.Error("Expected error while processing updated ServiceAttachment")
			} else if !tc.expectErr && err != nil {
				t.Errorf("Unexpected error while processing updated ServiceAttachment: %q", err)
			}

			updatedSA, err := getServiceAttachment(controller.cloud, gceSAName)
			if err != nil {
				t.Fatalf("Unexpected error when getting updatd GCE ServiceAttachment: %q", err)
			}

			if !reflect.DeepEqual(createdSA, updatedSA) {
				t.Errorf("GCE Service Attachment should not be updated. \nOriginal SA:\n %+v, \nUpdated SA:\n %+v", createdSA, updatedSA)
			}

			saCR, err = controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).Get(context2.TODO(), saName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get service attachment cr: %q", err)
			}
		})
	}
}

// newTestController returns a test psc controller
func newTestController() *Controller {
	kubeClient := fake.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	resourceNamer := namer.NewNamer(ClusterID, "")
	saClient := safake.NewSimpleClientset()

	ctxConfig := context.ControllerContextConfig{
		Namespace:             v1.NamespaceAll,
		ResyncPeriod:          1 * time.Minute,
		DefaultBackendSvcPort: test.DefaultBeSvcPort,
		HealthCheckPath:       "/",
	}

	ctx := context.NewControllerContext(nil, kubeClient, nil, nil, nil, nil, saClient, fakeGCE, resourceNamer, kubeSystemUID, ctxConfig)

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
func testServiceAttachmentCR(saName, svcName, svcUID string, subnets []string) *sav1alpha1.ServiceAttachment {
	return &sav1alpha1.ServiceAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      saName,
			UID:       types.UID(svcUID),
		},
		Spec: sav1alpha1.ServiceAttachmentSpec{
			ConnectionPreference: "ACCEPT_AUTOMATIC",
			NATSubnets:           subnets,
			ResourceRef: v1.TypedLocalObjectReference{
				Kind: "service",
				Name: svcName,
			},
		},
	}
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
func getServiceAttachment(cloud *gce.Cloud, saName string) (*alpha.ServiceAttachment, error) {
	saKey, err := composite.CreateKey(cloud, saName, meta.Regional)
	if err != nil {
		return nil, fmt.Errorf("errored creating a key for service attachment: %q", err)
	}
	sa, err := cloud.Compute().AlphaServiceAttachments().Get(context2.TODO(), saKey)
	if err != nil {
		return nil, fmt.Errorf("errored querying for service attachment: %q", err)
	}
	return sa, nil
}

// validateSAStatus validates that the status reports the same information as on the
// GCE service attachment resource
func validateSAStatus(status sav1alpha1.ServiceAttachmentStatus, sa *alpha.ServiceAttachment) error {
	if status.ServiceAttachmentURL != sa.SelfLink {
		return fmt.Errorf("ServiceAttachment.Status.ServiceAttachmentURL was %s, but should be %s", status.ServiceAttachmentURL, sa.SelfLink)
	}

	if status.ForwardingRuleURL != sa.ProducerForwardingRule {
		return fmt.Errorf("ServiceAttachment.Status.ForwardingRuleURL was %s, but should be %s", status.ForwardingRuleURL, sa.ProducerForwardingRule)
	}
	return nil
}
