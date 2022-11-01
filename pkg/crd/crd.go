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
	"time"

	"k8s.io/klog/v2"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// Sleep interval to check the Established condition of CRD.
	checkCRDEstablishedInterval = time.Second
	// Timeout for checking the Established condition of CRD.
	checkCRDEstablishedTimeout = 60 * time.Second
)

// CRDHandler takes care of ensuring CRD's for a cluster.
type CRDHandler struct {
	client crdclient.Interface
}

// NewCRDHandler returns a new CRDHandler.
func NewCRDHandler(client crdclient.Interface) *CRDHandler {
	return &CRDHandler{client}
}

// EnsureCRD ensures a CRD in a cluster given the CRD's metadata.
func (h *CRDHandler) EnsureCRD(meta *CRDMeta, namespacedScoped bool) (*apiextensionsv1.CustomResourceDefinition, error) {
	crd, err := h.createOrUpdateCRD(meta, namespacedScoped)
	if err != nil {
		return nil, err
	}

	// After CRD creation, it might take a few seconds for the RESTful API endpoint
	// to be created. Keeps watching the Established condition of BackendConfig
	// CRD to be true.
	if err := wait.PollImmediate(checkCRDEstablishedInterval, checkCRDEstablishedTimeout, func() (bool, error) {
		crd, err = h.client.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crd.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, c := range crd.Status.Conditions {
			if c.Type == apiextensionsv1.Established && c.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("timed out waiting for %v CRD to become Established: %v", meta.kind, err)
	}

	klog.V(0).Infof("%v CRD is Established.", meta.kind)
	return crd, nil
}

func (h *CRDHandler) createOrUpdateCRD(meta *CRDMeta, namespacedScoped bool) (*apiextensionsv1.CustomResourceDefinition, error) {
	crd := crd(meta, namespacedScoped)
	existingCRD, err := h.client.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crd.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to verify the existence of %v CRD: %v", meta.kind, err)
	}

	// Update CRD if already present.
	if err == nil {
		klog.V(0).Infof("Updating existing %v CRD...", meta.kind)
		crd.ResourceVersion = existingCRD.ResourceVersion
		return h.client.ApiextensionsV1().CustomResourceDefinitions().Update(context.TODO(), crd, metav1.UpdateOptions{})
	}

	klog.V(0).Infof("Creating %v CRD...", meta.kind)
	return h.client.ApiextensionsV1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{})
}

func crd(meta *CRDMeta, namespacedScoped bool) *apiextensionsv1.CustomResourceDefinition {
	scope := apiextensionsv1.NamespaceScoped
	if !namespacedScoped {
		scope = apiextensionsv1.ClusterScoped
	}
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: meta.plural + "." + meta.groupName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: meta.groupName,
			Scope: scope,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Kind:       meta.kind,
				ListKind:   meta.listKind,
				Plural:     meta.plural,
				Singular:   meta.singular,
				ShortNames: meta.shortNames,
			},
		},
	}

	if len(meta.versions) == 0 {
		klog.Errorf("No versions specified in CRD meta")
	}

	var versions []apiextensionsv1.CustomResourceDefinitionVersion
	for i, v := range meta.versions {
		validationSchema, err := v.validation()
		if err != nil {
			klog.Errorf("Error adding simple validation for %v CRD(%s API): %v", meta.kind, v.name, err)
		}
		if validationSchema == nil {
			klog.Errorf("No validation schema exists for %v CRD(%s API)", meta.kind, v.name)
		}
		version := apiextensionsv1.CustomResourceDefinitionVersion{
			Name:       v.name,
			Served:     true,
			Storage:    false,
			Schema:     validationSchema,
			Deprecated: v.deprecated,
		}
		// Set storage to true for the latest version.
		if i == 0 {
			version.Storage = true
		}
		versions = append(versions, version)
	}
	crd.Spec.Versions = versions
	return crd
}
