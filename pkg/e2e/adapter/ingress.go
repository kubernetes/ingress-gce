package adapter

// Legacy conversion code for the old extensions v1beta1 API group.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	extv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

// IngressCRUD wraps basic CRUD to allow use of old and new APIs.
type IngressCRUD struct {
	C *kubernetes.Clientset
}

// Get Ingress resource.
func (crud *IngressCRUD) Get(ns, name string) (*v1beta1.Ingress, error) {
	new, err := crud.supportsNewAPI()
	if err != nil {
		return nil, err
	}
	klog.Infof("Get %s/%s", ns, name)
	if new {
		return crud.C.NetworkingV1beta1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
	}
	klog.Warning("Using legacy API")
	ing, err := crud.C.ExtensionsV1beta1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
	return toIngressNetworkingGroup(ing), err
}

// Create Ingress resource.
func (crud *IngressCRUD) Create(ing *v1beta1.Ingress) (*v1beta1.Ingress, error) {
	new, err := crud.supportsNewAPI()
	if err != nil {
		return nil, err
	}
	klog.Infof("Create %s/%s", ing.Namespace, ing.Name)
	if new {
		return crud.C.NetworkingV1beta1().Ingresses(ing.Namespace).Create(context.TODO(), ing, metav1.CreateOptions{})
	}
	klog.Warning("Using legacy API")
	legacyIng := toIngressExtensionsGroup(ing)
	legacyIng, err = crud.C.ExtensionsV1beta1().Ingresses(ing.Namespace).Create(context.TODO(), legacyIng, metav1.CreateOptions{})
	return toIngressNetworkingGroup(legacyIng), err
}

// Update Ingress resource.
func (crud *IngressCRUD) Update(ing *v1beta1.Ingress) (*v1beta1.Ingress, error) {
	new, err := crud.supportsNewAPI()
	if err != nil {
		return nil, err
	}
	klog.Infof("Update %s/%s", ing.Namespace, ing.Name)
	if new {
		return crud.C.NetworkingV1beta1().Ingresses(ing.Namespace).Update(context.TODO(), ing, metav1.UpdateOptions{})
	}
	klog.Warning("Using legacy API")
	legacyIng := toIngressExtensionsGroup(ing)
	legacyIng, err = crud.C.ExtensionsV1beta1().Ingresses(ing.Namespace).Update(context.TODO(), legacyIng, metav1.UpdateOptions{})
	return toIngressNetworkingGroup(legacyIng), err
}

// Delete Ingress resource.
func (crud *IngressCRUD) Delete(ns, name string) error {
	new, err := crud.supportsNewAPI()
	if err != nil {
		return err
	}
	klog.Infof("Delete %s/%s", ns, name)
	if new {
		return crud.C.NetworkingV1beta1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	}
	klog.Warning("Using legacy API")
	return crud.C.ExtensionsV1beta1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (crud *IngressCRUD) supportsNewAPI() (bool, error) {
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion("networking.k8s.io/v1beta1"); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == "Ingress" {
				return true, nil
			}
		}
	}
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion("extensions/v1beta1"); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == "Ingress" {
				return false, nil
			}
		}
	}
	return false, errors.New("no Ingress resource found")
}

func toIngressExtensionsGroup(ing *v1beta1.Ingress) *extv1beta1.Ingress {
	b := &bytes.Buffer{}
	e := json.NewEncoder(b)
	// This is used by test cases from test data, so we assume no issues with
	// conversion.
	if err := e.Encode(ing); err != nil {
		panic(err)
	}

	ret := &extv1beta1.Ingress{}
	d := json.NewDecoder(b)
	if err := d.Decode(ret); err != nil {
		panic(err)
	}

	ret.APIVersion = "extensions/v1beta1"

	return ret
}

func toIngressNetworkingGroup(ing *extv1beta1.Ingress) *v1beta1.Ingress {
	b := &bytes.Buffer{}
	e := json.NewEncoder(b)
	// This is used by test cases from test data, so we assume no issues with
	// conversion.
	if err := e.Encode(ing); err != nil {
		panic(err)
	}

	ret := &v1beta1.Ingress{}
	d := json.NewDecoder(b)
	if err := d.Decode(ret); err != nil {
		panic(err)
	}

	ret.APIVersion = "networking.k8s.io/v1beta1"
	return ret
}
