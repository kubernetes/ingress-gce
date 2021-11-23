/*
Copyright 2020 The Kubernetes Authors.

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

package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	client "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/klog"
)

// BackendConfigCRUD wraps basic CRUD to allow use of v1beta and v1 APIs.
type BackendConfigCRUD struct {
	C *client.Clientset
}

// Get BackendConfig resource.
func (crud *BackendConfigCRUD) Get(ns, name string) (*v1.BackendConfig, error) {
	isV1, err := crud.supportsV1()
	if err != nil {
		return nil, err
	}
	klog.V(3).Infof("Get BackendConfig %s/%s", ns, name)
	if isV1 {
		return crud.C.CloudV1().BackendConfigs(ns).Get(context.TODO(), name, metav1.GetOptions{})
	}
	klog.V(4).Info("Using BackendConfig V1beta1 API")
	bc, err := crud.C.CloudV1beta1().BackendConfigs(ns).Get(context.TODO(), name, metav1.GetOptions{})
	return toV1(bc), err
}

// Create BackendConfig resource.
func (crud *BackendConfigCRUD) Create(bc *v1.BackendConfig) (*v1.BackendConfig, error) {
	isV1, err := crud.supportsV1()
	if err != nil {
		return nil, err
	}
	klog.V(2).Infof("Create BackendConfig %s/%s", bc.Namespace, bc.Name)
	if isV1 {
		return crud.C.CloudV1().BackendConfigs(bc.Namespace).Create(context.TODO(), bc, metav1.CreateOptions{})
	}
	klog.V(2).Info("Using BackendConfig V1beta1 API")
	legacyBc := toV1beta1(bc)
	legacyBc, err = crud.C.CloudV1beta1().BackendConfigs(bc.Namespace).Create(context.TODO(), legacyBc, metav1.CreateOptions{})
	return toV1(legacyBc), err
}

// Update BackendConfig resource.
func (crud *BackendConfigCRUD) Update(bc *v1.BackendConfig) (*v1.BackendConfig, error) {
	isV1, err := crud.supportsV1()
	if err != nil {
		return nil, err
	}
	klog.V(2).Infof("Update %s/%s", bc.Namespace, bc.Name)
	if isV1 {
		return crud.C.CloudV1().BackendConfigs(bc.Namespace).Update(context.TODO(), bc, metav1.UpdateOptions{})
	}
	klog.V(2).Infof("Using BackendConfig V1beta1 API")
	legacyBC := toV1beta1(bc)
	legacyBC, err = crud.C.CloudV1beta1().BackendConfigs(bc.Namespace).Update(context.TODO(), legacyBC, metav1.UpdateOptions{})
	return toV1(legacyBC), err
}

// Ensure creates or updates the backend config
func (crud *BackendConfigCRUD) Ensure(bc *v1.BackendConfig) (*v1.BackendConfig, error) {
	existingBc, err := crud.Get(bc.Namespace, bc.Name)
	if err != nil {
		if strings.HasSuffix(err.Error(), "not found") {
			existingBc = nil
		} else {
			return nil, err
		}
	}

	if existingBc == nil {
		return crud.Create(bc)
	}
	bc.ObjectMeta.ResourceVersion = existingBc.ObjectMeta.ResourceVersion
	return crud.Update(bc)
}

// Delete BackendConfig resource.
func (crud *BackendConfigCRUD) Delete(ns, name string) error {
	isV1, err := crud.supportsV1()
	if err != nil {
		return err
	}
	klog.V(2).Infof("Delete BackendConfig %s/%s", ns, name)
	if isV1 {
		return crud.C.CloudV1().BackendConfigs(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	}
	klog.V(2).Info("Using BackendConfig V1beta1 API")
	return crud.C.CloudV1beta1().BackendConfigs(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

// List BackendConfig resources in given namespace.
func (crud *BackendConfigCRUD) List(ns string) (*v1.BackendConfigList, error) {
	isV1, err := crud.supportsV1()
	if err != nil {
		return nil, err
	}
	klog.V(3).Infof("List BackendConfigs in namespace(%s)", ns)
	if isV1 {
		return crud.C.CloudV1().BackendConfigs(ns).List(context.TODO(), metav1.ListOptions{})
	}
	klog.V(4).Info("Using BackendConfig V1beta1 API")
	bcl, err := crud.C.CloudV1beta1().BackendConfigs(ns).List(context.TODO(), metav1.ListOptions{})
	return toV1List(bcl), err
}

func (crud *BackendConfigCRUD) supportsV1() (bool, error) {
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion("cloud.google.com/v1"); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == "BackendConfig" {
				return true, nil
			}
		}
	}
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion("cloud.google.com/v1beta1"); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == "BackendConfig" {
				return false, nil
			}
		}
	}
	return false, errors.New("no BackendConfig resource found")
}

func toV1(bc *v1beta1.BackendConfig) *v1.BackendConfig {
	b := &bytes.Buffer{}
	e := json.NewEncoder(b)
	// This is used by test cases from test data, so we assume no issues with
	// conversion.
	if err := e.Encode(bc); err != nil {
		panic(err)
	}

	ret := &v1.BackendConfig{}
	d := json.NewDecoder(b)
	if err := d.Decode(ret); err != nil {
		panic(err)
	}

	return ret
}

func toV1beta1(bc *v1.BackendConfig) *v1beta1.BackendConfig {
	b := &bytes.Buffer{}
	e := json.NewEncoder(b)
	// This is used by test cases from test data, so we assume no issues with
	// conversion.
	if err := e.Encode(bc); err != nil {
		panic(err)
	}

	ret := &v1beta1.BackendConfig{}
	d := json.NewDecoder(b)
	if err := d.Decode(ret); err != nil {
		panic(err)
	}

	return ret
}

func toV1List(bcl *v1beta1.BackendConfigList) *v1.BackendConfigList {
	b := &bytes.Buffer{}
	e := json.NewEncoder(b)
	// This is used by test cases from test data, so we assume no issues with
	// conversion.
	if err := e.Encode(bcl); err != nil {
		panic(err)
	}

	ret := &v1.BackendConfigList{}
	d := json.NewDecoder(b)
	if err := d.Decode(ret); err != nil {
		panic(err)
	}

	return ret
}
