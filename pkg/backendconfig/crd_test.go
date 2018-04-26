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

package backendconfig

import (
	"reflect"
	"testing"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdclientfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
)

func TestCreateOrUpdateCRD(t *testing.T) {
	expectedCRD := getCRDSpec()
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
				crd := getCRDSpec()
				crd.Spec.Names.Kind = "wrongname"
				crd.Spec.Names.ListKind = "wrongnameList"
				if _, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd); err != nil {
					return err
				}
				return nil
			},
		},
	}

	for _, tc := range testCases {
		crdClient := crdclientfake.NewSimpleClientset()
		if tc.initFunc != nil {
			if err := tc.initFunc(crdClient); err != nil {
				t.Errorf("%s: Unexpected error in initFunc(): %v", tc.desc, err)
			}
		}

		crd, err := createOrUpdateCRD(crdClient)
		if err != nil {
			t.Errorf("%s: Unexpected error in createOrUpdateCRD(): %v", tc.desc, err)
		}

		// Nuke CRD status before comparing.
		crd.Status = apiextensionsv1beta1.CustomResourceDefinitionStatus{}
		if !reflect.DeepEqual(crd, expectedCRD) {
			t.Errorf("%s: Unexpected CRD returned: got %v, want %v", tc.desc, crd, expectedCRD)
		}

	}
}
