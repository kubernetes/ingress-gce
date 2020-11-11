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
	"testing"

	"github.com/google/go-cmp/cmp"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdclientfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kube-openapi/pkg/common"
)

var (
	crdMeta = &CRDMeta{
		groupName: "test.group.com",
		versions: []*Version{
			NewVersion("v1alpha1", "pkg/apis/test/v1alpha1.Test", testGetOpenAPIDefinitions),
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
		desc     string
		initFunc func(clientset crdclient.Interface, namespaceScoped bool) error
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
	}

	for _, namespacedScoped := range []bool{true, false} {
		expectedCRD := crd(crdMeta, namespacedScoped)
		for _, tc := range testCases {
			t.Run(tc.desc, func(t *testing.T) {
				fakeCRDClient := crdclientfake.NewSimpleClientset()
				fakeCRDHandler := NewCRDHandler(fakeCRDClient)
				if tc.initFunc != nil {
					if err := tc.initFunc(fakeCRDClient, namespacedScoped); err != nil {
						t.Errorf("Unexpected error in initFunc(): %v", err)
					}
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

				// Nuke CRD status before comparing.
				crd.Status = apiextensionsv1.CustomResourceDefinitionStatus{}
				if diff := cmp.Diff(expectedCRD, crd); diff != "" {
					t.Errorf("Unexpected CRD returned (-want +got):\n%s", diff)
				}
			})
		}
	}
}
