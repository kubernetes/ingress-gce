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

package patch

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/kubernetes/pkg/util/slice"
)

func TestStrategicMergePatchBytes(t *testing.T) {
	// Patch an Ingress w/ a finalizer
	ing := &v1.Ingress{}
	updated := &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"foo"},
		},
	}
	b, err := StrategicMergePatchBytes(ing, updated, v1.Ingress{})
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"metadata":{"finalizers":["foo"]}}`
	if string(b) != expected {
		t.Errorf("StrategicMergePatchBytes(%+v, %+v) = %s ; want %s", ing, updated, string(b), expected)
	}

	// Patch an Ingress with the finalizer removed
	ing = updated
	updated = &v1.Ingress{}
	b, err = StrategicMergePatchBytes(ing, updated, v1.Ingress{})
	if err != nil {
		t.Fatal(err)
	}
	expected = `{"metadata":{"finalizers":null}}`
	if string(b) != expected {
		t.Errorf("StrategicMergePatchBytes(%+v, %+v) = %s ; want %s", ing, updated, string(b), expected)
	}
}

func TestJSONMergePatchBytes(t *testing.T) {
	// Patch an Ingress w/ a finalizer
	ing := &v1.Ingress{}
	updated := &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"foo"},
		},
	}
	b, err := MergePatchBytes(ing, updated)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"metadata":{"finalizers":["foo"]}}`
	if string(b) != expected {
		t.Errorf("MergePatchBytes(%+v, %+v) = %s ; want %s", ing, updated, string(b), expected)
	}

	// Patch an Ingress with an additional finalizer
	ing = updated
	updated = &v1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{"foo", "bar"},
		},
	}
	b, err = MergePatchBytes(ing, updated)
	if err != nil {
		t.Fatal(err)
	}
	expected = `{"metadata":{"finalizers":["foo","bar"]}}`
	if string(b) != expected {
		t.Errorf("MergePatchBytes(%+v, %+v) = %s ; want %s", ing, updated, string(b), expected)
	}
	// Patch an Ingress with the finalizer removed
	ing = updated
	updated = &v1.Ingress{}
	b, err = MergePatchBytes(ing, updated)
	if err != nil {
		t.Fatal(err)
	}
	expected = `{"metadata":{"finalizers":null}}`
	if string(b) != expected {
		t.Errorf("MergePatchBytes(%+v, %+v) = %s ; want %s", ing, updated, string(b), expected)
	}
}

func TestPatchServiceObjectMetadata(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		svc         *apiv1.Service
		newMetaFunc func(*apiv1.Service) *apiv1.Service
	}{
		{
			desc: "add annotation",
			svc:  newTestService("ns1", "add-annotation-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Annotations["test-annotation-key3"] = "test-value3"
				return ret
			},
		},
		{
			desc: "delete annotation",
			svc:  newTestService("ns2", "delete-annotation-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				delete(ret.Annotations, testAnnotationKey)
				return ret
			},
		},
		{
			desc: "delete all annotations",
			svc:  newTestService("ns3", "delete-all-annotations-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Annotations = nil
				return ret
			},
		},
		{
			desc: "add finalizer",
			svc:  newTestService("ns4", "add-finalizer-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Finalizers = append(ret.Finalizers, "new-test-finalizer")
				return ret
			},
		},
		{
			desc: "delete finalizer",
			svc:  newTestService("ns5", "delete-finalizer-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Finalizers = slice.RemoveString(ret.Finalizers, testFinalizer, nil)
				return ret
			},
		},
		{
			desc: "delete all finalizers",
			svc:  newTestService("ns6", "delete-all-finalizers-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Finalizers = nil
				return ret
			},
		},
		{
			desc: "delete both annotation and finalizer",
			svc:  newTestService("ns7", "delete-annotation-and-finalizer-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Finalizers = slice.RemoveString(ret.Finalizers, testFinalizer, nil)
				delete(ret.Annotations, testAnnotationKey)
				return ret
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			svcKey := fmt.Sprintf("%s/%s", tc.svc.Namespace, tc.svc.Name)
			coreClient := fake.NewSimpleClientset().CoreV1()
			if _, err := coreClient.Services(tc.svc.Namespace).Create(context.TODO(), tc.svc, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Create(%s) = %v, want nil", svcKey, err)
			}
			expectSvc := tc.newMetaFunc(tc.svc)
			err := PatchServiceObjectMetadata(coreClient, tc.svc, expectSvc.ObjectMeta)
			if err != nil {
				t.Fatalf("PatchServiceObjectMetadata(%s) = %v, want nil", svcKey, err)
			}

			gotSvc, err := coreClient.Services(tc.svc.Namespace).Get(context.TODO(), tc.svc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Get(%s) = %v, want nil", svcKey, err)
			}
			if diff := cmp.Diff(expectSvc, gotSvc); diff != "" {
				t.Errorf("Got mismatch for Service (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPatchServiceLoadBalancerStatus(t *testing.T) {
	for _, tc := range []struct {
		desc        string
		svc         *apiv1.Service
		newMetaFunc func(*apiv1.Service) *apiv1.Service
	}{
		{
			desc: "update status",
			svc:  newTestService("ns1", "update-status-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Status = apiv1.ServiceStatus{
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
			svc:  newTestService("ns2", "delete-status-svc"),
			newMetaFunc: func(svc *apiv1.Service) *apiv1.Service {
				ret := svc.DeepCopy()
				ret.Status = apiv1.ServiceStatus{}
				return ret
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			svcKey := fmt.Sprintf("%s/%s", tc.svc.Namespace, tc.svc.Name)
			coreClient := fake.NewSimpleClientset().CoreV1()
			if _, err := coreClient.Services(tc.svc.Namespace).Create(context.TODO(), tc.svc, metav1.CreateOptions{}); err != nil {
				t.Fatalf("Create(%s) = %v, want nil", svcKey, err)
			}
			expectSvc := tc.newMetaFunc(tc.svc)
			err := PatchServiceLoadBalancerStatus(coreClient, tc.svc, expectSvc.Status.LoadBalancer)
			if err != nil {
				t.Fatalf("PatchServiceLoadBalancerStatus(%s) = %v, want nil", svcKey, err)
			}

			gotSvc, err := coreClient.Services(tc.svc.Namespace).Get(context.TODO(), tc.svc.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Get(%s) = %v, want nil", svcKey, err)
			}
			if diff := cmp.Diff(expectSvc, gotSvc); diff != "" {
				t.Errorf("Got mismatch for Service (-want +got):\n%s", diff)
			}
		})
	}
}

const (
	testAnnotationKey = "test-annotations-key1"
	testFinalizer     = "test-finalizer"
)

func newTestService(namespace, name string) *apiv1.Service {
	return &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				testAnnotationKey:       "test-value1",
				"test-annotations-key2": "test-value2",
			},
			Finalizers: []string{testFinalizer},
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{Name: "http-80"},
			},
		},
		Status: apiv1.ServiceStatus{
			LoadBalancer: apiv1.LoadBalancerStatus{
				Ingress: []apiv1.LoadBalancerIngress{
					{IP: "127.0.0.1"},
				},
			},
		},
	}
}
