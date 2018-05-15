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
	"fmt"
	"time"

	"github.com/golang/glog"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	apisbackendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig"
)

const (
	// Sleep interval to check the Established condition of CRD.
	checkCRDEstablishedInterval = time.Second
	// Timeout for checking the Established condition of CRD.
	checkCRDEstablishedTimeout = 60 * time.Second
)

func getCRDSpec() *apiextensionsv1beta1.CustomResourceDefinition {
	return &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "backendconfigs" + "." + apisbackendconfig.GroupName},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   apisbackendconfig.GroupName,
			Version: "v1beta1",
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Kind:     "BackendConfig",
				ListKind: "BackendConfigList",
				Plural:   "backendconfigs",
				Singular: "backendconfig",
			},
		},
	}
}

// EnsureCRD ensures the BackendConfig CRD resource for cluster.
func EnsureCRD(clientset crdclient.Interface) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
	crd, err := createOrUpdateCRD(clientset)
	if err != nil {
		return nil, err
	}

	// After CRD creation, it might take a few seconds for the RESTful API endpoint
	// to be created. Keeps watching the Established condition of BackendConfig
	// CRD to be true.
	if err := wait.PollImmediate(checkCRDEstablishedInterval, checkCRDEstablishedTimeout, func() (bool, error) {
		crd, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crd.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, c := range crd.Status.Conditions {
			if c.Type == apiextensionsv1beta1.Established && c.Status == apiextensionsv1beta1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("timed out waiting for BackendConfig CRD to become Established: %v", err)
	}

	glog.V(0).Infof("BackendConfig CRD is Established.")
	return crd, nil
}

func createOrUpdateCRD(clientset crdclient.Interface) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
	crd := getCRDSpec()
	existingCRD, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crd.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to verify the existence of BackendConfig CRD: %v", err)
	}

	// Update BackendConfig CRD if already present.
	if err == nil {
		glog.V(0).Infof("Updating existing BackendConfig CRD...")
		crd.ResourceVersion = existingCRD.ResourceVersion
		return clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Update(crd)
	}

	glog.V(0).Infof("Creating BackendConfig CRD...")
	return clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
}
