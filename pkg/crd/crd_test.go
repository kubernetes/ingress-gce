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
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdclientfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	crdMeta = &CRDMeta{
		groupName: "test.group.com",
		version:   "v1alpha1",
		kind:      "Test",
		listKind:  "TestList",
		singular:  "test",
		plural:    "tests",
	}
	trueValue = true
)

func TestCreateOrUpdateCRD(t *testing.T) {
	expectedCRD := crd(crdMeta)
	testCases := []struct {
		desc     string
		initFunc func(clientset crdclient.Interface) error
	}{
		{
			desc: "Create CRD when not exist",
		},
		{
			desc: "Update CRD when exist with wrongname",
			initFunc: func(clientset crdclient.Interface) error {
				crd := crd(crdMeta)
				crd.Spec.Names.Kind = "wrongname"
				crd.Spec.Names.ListKind = "wrongnameList"
				if _, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
					return err
				}
				return nil
			},
		},
		{
			desc: "Update CRD when pruning is not enabled",
			initFunc: func(clientset crdclient.Interface) error {
				crd := crd(crdMeta)
				if crd.Spec.Validation != nil {
					crd.Spec.PreserveUnknownFields = &trueValue
				}
				if _, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{}); err != nil {
					return err
				}
				return nil
			},
		},
	}

	for _, tc := range testCases {
		fakeCRDClient := crdclientfake.NewSimpleClientset()
		fakeCRDHandler := NewCRDHandler(fakeCRDClient)
		if tc.initFunc != nil {
			if err := tc.initFunc(fakeCRDClient); err != nil {
				t.Errorf("%s: Unexpected error in initFunc(): %v", tc.desc, err)
			}
		}

		crd, err := fakeCRDHandler.createOrUpdateCRD(crdMeta)
		if err != nil {
			t.Errorf("%s: Unexpected error in createOrUpdateCRD(): %v", tc.desc, err)
		}

		if crd.Spec.Validation != nil {
			if crd.Spec.PreserveUnknownFields == nil {
				t.Fatalf("%s: Unexpected CRD returned, PreserveUnknownFields not set: %v", tc.desc, crd)
			}
			if *crd.Spec.PreserveUnknownFields {
				t.Errorf("%s: Unexpected CRD returned, PreserveUnknownFields set to true: %v", tc.desc, crd)
			}
		}

		// Nuke CRD status before comparing.
		crd.Status = apiextensionsv1beta1.CustomResourceDefinitionStatus{}
		if diff := cmp.Diff(expectedCRD, crd); diff != "" {
			t.Errorf("%s: Unexpected CRD returned (-want +got):\n%s", tc.desc, diff)
		}

	}
}
