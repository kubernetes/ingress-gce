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
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	alpha "google.golang.org/api/compute/v0.alpha"
	ga "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"k8s.io/ingress-gce/pkg/utils"
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

			controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).Create(context2.TODO(), saCR, metav1.CreateOptions{})
			syncServiceAttachmentLister(controller)

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

	saCRAnnotation := testServiceAttachmentCR(saName, svcName, saUID, []string{"my-subnet"}, true)
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
			updatedSACR: testServiceAttachmentCR(saName, svcName, saUID, []string{"diff-subnet"}, true),
			expectErr:   true,
		},
		{
			desc:        "updated service name",
			updatedSACR: testServiceAttachmentCR(saName, "my-second-service", saUID, []string{"my-subnet"}, true),
			expectErr:   true,
		},
		{
			desc:        "removed subnet",
			updatedSACR: testServiceAttachmentCR(saName, svcName, saUID, []string{}, true),
			expectErr:   true,
		},
		{
			desc:        "removed finalizer",
			updatedSACR: testServiceAttachmentCR(saName, svcName, saUID, []string{"my-subnet"}, false),
			expectErr:   false,
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

			saCR := testServiceAttachmentCR(saName, svcName, saUID, []string{"my-subnet"}, false)
			_, err = controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).Create(context2.TODO(), saCR, metav1.CreateOptions{})
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

			_, err = controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).Update(context2.TODO(), tc.updatedSACR, metav1.UpdateOptions{})
			syncServiceAttachmentLister(controller)
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

func TestServiceAttachmentGarbageCollection(t *testing.T) {
	svcNamePrefix := "my-service"
	saUIDPrefix := "serivce-attachment-uid"
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
			getError:       utils.FakeGoogleAPINotFoundErr(),
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

			controller := newTestController()

			if _, err := createNatSubnet(controller.cloud, "my-subnet"); err != nil {
				t.Errorf("failed to create subnet: %s", err)
			}

			// create a serviceAttachment that should not be deleted as part of GC
			saToKeep := testServiceAttachmentCR("sa-to-keep", svcNamePrefix+"-keep", saUIDPrefix+"-keep", []string{"my-subnet"}, true)
			saToBeDeleted := testServiceAttachmentCR("sa-to-be-deleted", svcNamePrefix+"-deleted", saUIDPrefix+"-deleted", []string{"my-subnet"}, true)
			for _, sa := range []*sav1alpha1.ServiceAttachment{saToKeep, saToBeDeleted} {
				svcName := sa.Spec.ResourceRef.Name
				svc, frName, err := createSvc(controller, svcName, string(sa.UID), frIPAddr, annotations.TCPForwardingRuleKey)
				if err != nil {
					t.Errorf("%s", err)
				}
				controller.serviceLister.Add(svc)

				if _, err = createForwardingRule(controller.cloud, frName, frIPAddr); err != nil {
					t.Errorf("%s", err)
				}

				_, err = controller.saClient.NetworkingV1alpha1().ServiceAttachments(sa.Namespace).Create(context2.TODO(), sa, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("failed to add service attachment to client: %q", err)
				}

				syncServiceAttachmentLister(controller)

				if err = controller.processServiceAttachment(SvcAttachmentKeyFunc(sa.Namespace, sa.Name)); err != nil {
					t.Fatalf("failed to process service attachment: %q", err)
				}
			}

			deletionTS := metav1.Now()
			saCR, err := controller.saClient.NetworkingV1alpha1().ServiceAttachments(saToBeDeleted.Namespace).Get(context2.TODO(), saToBeDeleted.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get service attachment cr: %q", err)
			}

			saCR.DeletionTimestamp = &deletionTS
			if tc.invalidSAURL {
				saCR.Status.ServiceAttachmentURL = tc.saURL
			}
			_, err = controller.saClient.NetworkingV1alpha1().ServiceAttachments(saToBeDeleted.Namespace).Update(context2.TODO(), saCR, metav1.UpdateOptions{})
			if err != nil {
				t.Fatalf("failed to update service attachment to client: %q", err)
			}

			// sync the controller cache to have have current set of serviceAttachments
			syncServiceAttachmentLister(controller)

			if tc.getError != nil || tc.deleteError != nil {

				fakeGCE := controller.cloud.Compute().(*cloud.MockGCE)
				mockSA := fakeGCE.AlphaServiceAttachments().(*cloud.MockAlphaServiceAttachments)

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
				currSA, err := controller.saClient.NetworkingV1alpha1().ServiceAttachments(saToBeDeleted.Namespace).Get(context2.TODO(), saToBeDeleted.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("failed to query for service attachment %s from client: %q", currSA.Name, err)
				}

				if err := verifyServiceAttachmentFinalizer(currSA); err != nil {
					t.Errorf("service attachment %s finalizer should not be removed after gc: %q", currSA.Name, err)
				}
			}

			// verify saToKeep was not deleted
			currSA, err := controller.saClient.NetworkingV1alpha1().ServiceAttachments(saToKeep.Namespace).Get(context2.TODO(), saToKeep.Name, metav1.GetOptions{})
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
func testServiceAttachmentCR(saName, svcName, svcUID string, subnets []string, withFinalizer bool) *sav1alpha1.ServiceAttachment {
	cr := &sav1alpha1.ServiceAttachment{
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

// verifyServiceAttachmentFinalizer verfies that the provided ServiceAttachment CR
// has the ServiceAttachmentFinalizerKey, otherwise it will return an error
func verifyServiceAttachmentFinalizer(cr *sav1alpha1.ServiceAttachment) error {
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
	crs, err := controller.saClient.NetworkingV1alpha1().ServiceAttachments(testNamespace).List(context2.TODO(), metav1.ListOptions{})
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

// verifyServiceAttachmentCRDeletion will verify that the provicded ServiceAttachment CR
// does not have the service attachment finalizer and that the deletion timestamp has been
// set
func verifyServiceAttachmentCRDeletion(controller *Controller, sa *sav1alpha1.ServiceAttachment) error {
	currCR, err := controller.saClient.NetworkingV1alpha1().ServiceAttachments(sa.Namespace).Get(context2.TODO(), sa.Name, metav1.GetOptions{})
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
// Service Attachmen resource has been deleted
func verifyGCEServiceAttachmentDeletion(controller *Controller, sa *sav1alpha1.ServiceAttachment) error {
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
