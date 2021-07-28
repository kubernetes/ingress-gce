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

package common

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/utils/slice"
)

func TestPatchIngressObjectMetadata(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		ing         *v1.Ingress
		newMetaFunc func(*v1.Ingress) *v1.Ingress
	}{
		{
			desc: "add annotation",
			ing:  newTestIngress("ns1", "add-annotation-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				ret.Annotations["test-annotation-key3"] = "test-value3"
				return ret
			},
		},
		{
			desc: "delete annotation",
			ing:  newTestIngress("ns2", "delete-annotation-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				delete(ret.Annotations, testAnnotationKey)
				return ret
			},
		},
		{
			desc: "delete all annotations",
			ing:  newTestIngress("ns3", "delete-all-annotations-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				ret.Annotations = nil
				return ret
			},
		},
		{
			desc: "add finalizer",
			ing:  newTestIngress("ns4", "add-finalizer-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				ret.Finalizers = append(ret.Finalizers, "new-test-ingress-finalizer")
				return ret
			},
		},
		{
			desc: "delete finalizer",
			ing:  newTestIngress("ns5", "delete-finalizer-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				ret.Finalizers = slice.RemoveString(ret.Finalizers, testFinalizer, nil)
				return ret
			},
		},
		{
			desc: "delete annotation and finalizer",
			ing:  newTestIngress("ns6", "delete-annotation-and-finalizer-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				ret.Annotations = nil
				ret.Finalizers = nil
				return ret
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ingKey := fmt.Sprintf("%s/%s", tc.ing.Namespace, tc.ing.Name)
			ingClient := fake.NewSimpleClientset().NetworkingV1().Ingresses(tc.ing.Namespace)
			if _, err := ingClient.Create(context.TODO(), tc.ing, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Create(%s) = %v, want nil", ingKey, err)
			}
			// Add an annotation to the ingress resource so that the resource version
			// is different from the one that will be used to compute patch bytes.
			updatedIng := tc.ing.DeepCopy()
			updatedIng.Annotations["readonly-annotation-key"] = "readonly-value"
			if _, err := ingClient.Update(context.TODO(), updatedIng, metav1.UpdateOptions{}); err != nil {
				t.Fatalf("Create(%s) = %v, want nil", ingKey, err)
			}
			gotIng, err := PatchIngressObjectMetadata(ingClient, tc.ing, tc.newMetaFunc(tc.ing).ObjectMeta)
			if err != nil {
				t.Fatalf("PatchIngressObjectMetadata(%s) = %v, want nil", ingKey, err)
			}

			// Verify that the read only annotation is not overwritten.
			expectIng := tc.newMetaFunc(updatedIng)
			if diff := cmp.Diff(expectIng, gotIng); diff != "" {
				t.Errorf("Got mismatch for Ingress patch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchIngressStatus(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		ing         *v1.Ingress
		newMetaFunc func(*v1.Ingress) *v1.Ingress
	}{
		{
			desc: "update status",
			ing:  newTestIngress("ns1", "update-status-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				ret.Status = v1.IngressStatus{
					LoadBalancer: apiv1.LoadBalancerStatus{
						Ingress: []apiv1.LoadBalancerIngress{
							{IP: "10.0.0.1"},
						},
					},
				}
				return ret
			},
		},
		{
			desc: "delete status",
			ing:  newTestIngress("ns2", "delete-status-ing"),
			newMetaFunc: func(ing *v1.Ingress) *v1.Ingress {
				ret := ing.DeepCopy()
				ret.Status = v1.IngressStatus{}
				return ret
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			ingKey := fmt.Sprintf("%s/%s", tc.ing.Namespace, tc.ing.Name)
			ingClient := fake.NewSimpleClientset().NetworkingV1().Ingresses(tc.ing.Namespace)
			if _, err := ingClient.Create(context.TODO(), tc.ing, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Create(%s) = %v, want nil", ingKey, err)
			}
			expectIng := tc.newMetaFunc(tc.ing)
			gotIng, err := PatchIngressStatus(ingClient, tc.ing, expectIng.Status)
			if err != nil {
				t.Fatalf("PatchIngressStatus(%s) = %v, want nil", ingKey, err)
			}

			if diff := cmp.Diff(expectIng, gotIng); diff != "" {
				t.Errorf("Got mismatch for Ingress patch (-want +got):\n%s", diff)
			}
		})
	}
}

const (
	testAnnotationKey = "test-annotations-key1"
	testFinalizer     = "test-finalizer"
)

func newTestIngress(namespace, name string) *v1.Ingress {
	return &v1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				testAnnotationKey:       "test-value1",
				"test-annotations-key2": "test-value2",
			},
			Finalizers: []string{testFinalizer},
		},
		Spec: v1.IngressSpec{
			DefaultBackend: &v1.IngressBackend{
				Service: &v1.IngressServiceBackend{
					Name: "test-svc",
					Port: v1.ServiceBackendPort{
						Number: 8080,
					},
				},
			},
		},
		Status: v1.IngressStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					{IP: "127.0.0.1"},
				},
			},
		},
	}
}
