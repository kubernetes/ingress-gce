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

	"k8s.io/klog"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	utilpointer "k8s.io/utils/pointer"
)

const (
	// Sleep interval to check the Established condition of CRD.
	checkCRDEstablishedInterval = time.Second
	// Timeout for checking the Established condition of CRD.
	checkCRDEstablishedTimeout = 60 * time.Second
	v1String                   = "v1"
	v1beta1String              = "v1beta1"
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
func (h *CRDHandler) EnsureCRD(meta *CRDMeta) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
	crd, err := h.createOrUpdateCRD(meta)
	if err != nil {
		return nil, err
	}

	// After CRD creation, it might take a few seconds for the RESTful API endpoint
	// to be created. Keeps watching the Established condition of BackendConfig
	// CRD to be true.
	if err := wait.PollImmediate(checkCRDEstablishedInterval, checkCRDEstablishedTimeout, func() (bool, error) {
		crd, err = h.client.ApiextensionsV1beta1().CustomResourceDefinitions().Get(context.TODO(), crd.Name, metav1.GetOptions{})
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
		return nil, fmt.Errorf("timed out waiting for %v CRD to become Established: %v", meta.kind, err)
	}

	klog.V(0).Infof("%v CRD is Established.", meta.kind)
	return crd, nil
}

func (h *CRDHandler) createOrUpdateCRD(meta *CRDMeta) (*apiextensionsv1beta1.CustomResourceDefinition, error) {
	crd := crd(meta)
	existingCRD, err := h.client.ApiextensionsV1beta1().CustomResourceDefinitions().Get(context.TODO(), crd.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to verify the existence of %v CRD: %v", meta.kind, err)
	}

	// Update CRD if already present.
	if err == nil {
		klog.V(0).Infof("Updating existing %v CRD...", meta.kind)
		crd.ResourceVersion = existingCRD.ResourceVersion
		return h.client.ApiextensionsV1beta1().CustomResourceDefinitions().Update(context.TODO(), crd, metav1.UpdateOptions{})
	}

	klog.V(0).Infof("Creating %v CRD...", meta.kind)
	return h.client.ApiextensionsV1beta1().CustomResourceDefinitions().Create(context.TODO(), crd, metav1.CreateOptions{})
}

func crd(meta *CRDMeta) *apiextensionsv1beta1.CustomResourceDefinition {
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: meta.plural + "." + meta.groupName},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:    meta.groupName,
			Versions: getCRDVersions(meta.version),
			Scope:    apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Kind:       meta.kind,
				ListKind:   meta.listKind,
				Plural:     meta.plural,
				Singular:   meta.singular,
				ShortNames: meta.shortNames,
			},
		},
	}
	if meta.typeSource != "" && meta.fn != nil {
		validationSpec, err := validation(meta.typeSource, meta.fn)
		if err != nil {
			klog.Errorf("Error adding simple validation for %v CRD: %v", meta.kind, err)
		}
		crd.Spec.Validation = validationSpec
		// Drop unknown fields of a CRD resource. Note that this needs be specified
		// in conjunction with validation spec.
		crd.Spec.PreserveUnknownFields = utilpointer.BoolPtr(false)
	}
	return crd
}

func getCRDVersions(version string) []apiextensionsv1beta1.CustomResourceDefinitionVersion {
	switch version {
	case v1String:
		return []apiextensionsv1beta1.CustomResourceDefinitionVersion{
			{
				Name:    v1String,
				Served:  true,
				Storage: true,
			},
			{
				Name:    v1beta1String,
				Served:  true,
				Storage: false,
			},
		}
	case v1beta1String:
		return []apiextensionsv1beta1.CustomResourceDefinitionVersion{
			{
				Name:    v1beta1String,
				Served:  true,
				Storage: true,
			},
		}
	default:
		klog.Errorf("Unexpected CRD version %s", version)
		return []apiextensionsv1beta1.CustomResourceDefinitionVersion{}
	}
}
