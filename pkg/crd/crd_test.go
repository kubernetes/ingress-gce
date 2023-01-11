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

package crd

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdclientfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/kube-openapi/pkg/common"
)

var (
	crdMeta = &CRDMeta{
		groupName: "test.group.com",
		versions: []*Version{
			NewVersion("v1alpha1", "pkg/apis/test/v1alpha1.Test", testGetOpenAPIDefinitions, false),
		},
		kind:     "Test",
		listKind: "TestList",
		singular: "test",
		plural:   "tests",
	}
	trueValue = true
)

func testGetOpenAPIDefinitions(common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return map[string]common.OpenAPIDefinition{}
}

func TestCreateOrUpdateCRD(t *testing.T) {
	testCases := []struct {
		desc             string
		initFunc         func(clientset crdclient.Interface, namespaceScoped bool) error
		retryOnForbidden bool
	}{
		{
			desc: "Create CRD when not exist",
		},
		{
			desc: "Update CRD when exist with wrongname",
			initFunc: func(clientset crdclient.Interface, namespaceScoped bool) error {
				crd := crd(crdMeta, namespaceScoped)
				crd.Spec.Names.Kind = "wrongname"
				crd.Spec.Names.ListKind = "wrongnameList"
				if _, err := clientset.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
					return err
				}
				return nil
			},
		},
		{
			desc: "Update CRD when pruning is not enabled",
			initFunc: func(clientset crdclient.Interface, namespaceScoped bool) error {
				crd := crd(crdMeta, namespaceScoped)
				crd.Spec.PreserveUnknownFields = trueValue
				if _, err := clientset.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
					return err
				}
				return nil
			},
		},
		{
			desc:             "Create CRD when not exist after receiving a forbidden error the first time",
			retryOnForbidden: true,
		},
		{
			desc: "Update CRD when exist with wrongname after receiving a forbidden error the first time",
			initFunc: func(clientset crdclient.Interface, namespaceScoped bool) error {
				crd := crd(crdMeta, namespaceScoped)
				crd.Spec.Names.Kind = "wrongname"
				crd.Spec.Names.ListKind = "wrongnameList"
				if _, err := clientset.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
					return err
				}
				return nil
			},
			retryOnForbidden: true,
		},
		{
			desc: "Update CRD when pruning is not enabled after receiving a forbidden error the first time",
			initFunc: func(clientset crdclient.Interface, namespaceScoped bool) error {
				crd := crd(crdMeta, namespaceScoped)
				crd.Spec.PreserveUnknownFields = trueValue
				if _, err := clientset.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
					return err
				}
				return nil
			},
			retryOnForbidden: true,
		},
	}

	for _, namespacedScoped := range []bool{true, false} {
		expectedCRD := crd(crdMeta, namespacedScoped)
		for _, tc := range testCases {
			t.Run(tc.desc+fmt.Sprintf(" namespaced scoped %v", namespacedScoped), func(t *testing.T) {
				fakeCRDClient := crdclientfake.NewSimpleClientset()
				fakeCRDHandler := NewCRDHandler(fakeCRDClient)
				if tc.initFunc != nil {
					if err := tc.initFunc(fakeCRDClient, namespacedScoped); err != nil {
						t.Errorf("Unexpected error in initFunc(): %v", err)
					}
				}
				// test createOrUpdateCRD retries on http forbidden errors
				numRetries := 3
				if tc.retryOnForbidden {
					counter := 0
					fakeCRDClient.PrependReactor("get", "customresourcedefinitions", k8stesting.ReactionFunc(func(a k8stesting.Action) (bool, runtime.Object, error) {
						counter++
						if counter < numRetries {
							return true, &apiextensionsv1.CustomResourceDefinition{}, apierrors.NewForbidden(a.GetResource().GroupResource(), a.(k8stesting.GetAction).GetName(), fmt.Errorf(`User "system:controller:glbc" cannot get resource`))
						}

						// delegate to the next reactor in the chain
						return false, &apiextensionsv1.CustomResourceDefinition{}, nil
					}))
				}
				crd, err := fakeCRDHandler.createOrUpdateCRD(crdMeta, namespacedScoped)
				if err != nil {
					t.Errorf("Unexpected error in createOrUpdateCRD(): %v", err)
				}

				if len(crd.Spec.Versions) == 0 {
					t.Fatal("Unexpected CRD returned, no versions exist")
				}

				for i, v := range crd.Spec.Versions {
					if i == 0 && !v.Storage {
						t.Errorf("Unexpected version returned, storage set to false for %s API", v.Name)
					}
					if i != 0 && v.Storage {
						t.Errorf("Unexpected version returned, storage set to true for %s API", v.Name)
					}
					if v.Schema == nil {
						t.Errorf("Unexpected version returned, validation schema not specified for %s API", v.Name)
					}
				}

				// pruning is always expected to be enabled(PreserveUnknownFields=false).
				if crd.Spec.PreserveUnknownFields {
					t.Errorf("Unexpected CRD returned, PreserveUnknownFields set to true: %v", crd)
				}

				if namespacedScoped && crd.Spec.Scope != apiextensionsv1.NamespaceScoped {
					t.Errorf("CRD should be namespaced scoped but found %s", crd.Spec.Scope)
				} else if !namespacedScoped && crd.Spec.Scope != apiextensionsv1.ClusterScoped {
					t.Errorf("CRD should be cluster scoped but found %s", crd.Spec.Scope)
				}

				getActions := 0
				for _, a := range fakeCRDClient.Actions() {
					if a.GetVerb() == "get" && a.GetResource().Resource == "customresourcedefinitions" {
						getActions++
					}
				}
				expectedGetActions := 1
				if tc.retryOnForbidden {
					expectedGetActions = numRetries
				}

				if getActions != expectedGetActions {
					t.Errorf("Unexpected number of Get requests, got %d expected %d", getActions, expectedGetActions)
				}

				// Nuke CRD status before comparing.
				crd.Status = apiextensionsv1.CustomResourceDefinitionStatus{}
				if diff := cmp.Diff(expectedCRD, crd); diff != "" {
					t.Errorf("Unexpected CRD returned (-want +got):\n%s", diff)
				}
			})
		}
	}
}
