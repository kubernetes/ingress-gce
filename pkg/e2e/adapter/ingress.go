package adapter

// Legacy conversion code for the old extensions v1beta1 API group.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	v1 "k8s.io/api/networking/v1"
	"k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

const (
	v1GroupVersion      = "networking.k8s.io/v1"
	v1beta1GroupVersion = "networking.k8s.io/v1beta1"
)

//TODO(srepakula) Remove adapter client once v1beta1 is no longer supported

// IngressCRUD wraps basic CRUD to allow use of old and new APIs.
type IngressCRUD struct {
	C *kubernetes.Clientset
}

// Get Ingress resource.
func (crud *IngressCRUD) Get(ns, name string) (*v1.Ingress, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}
	klog.Infof("Get %s/%s", ns, name)
	if useV1 {
		return crud.C.NetworkingV1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
	}
	klog.Warning("Using v1beta1 API")
	ing, err := crud.C.NetworkingV1beta1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return toIngressV1(ing)
}

// Create Ingress resource.
func (crud *IngressCRUD) Create(ing *v1.Ingress) (*v1.Ingress, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}
	klog.Infof("Create %s/%s", ing.Namespace, ing.Name)
	if useV1 {
		return crud.C.NetworkingV1().Ingresses(ing.Namespace).Create(context.TODO(), ing, metav1.CreateOptions{})
	}

	klog.Warning("Using v1beta1 API")
	legacyIng, err := toIngressV1beta1(ing)
	if err != nil {
		return nil, err
	}

	legacyIng, err = crud.C.NetworkingV1beta1().Ingresses(ing.Namespace).Create(context.TODO(), legacyIng, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return toIngressV1(legacyIng)
}

// Update Ingress resource.
func (crud *IngressCRUD) Update(ing *v1.Ingress) (*v1.Ingress, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}
	klog.Infof("Update %s/%s", ing.Namespace, ing.Name)
	if useV1 {
		return crud.C.NetworkingV1().Ingresses(ing.Namespace).Update(context.TODO(), ing, metav1.UpdateOptions{})
	}

	klog.Warning("Using legacy API")
	legacyIng, err := toIngressV1beta1(ing)
	if err != nil {
		return nil, err
	}

	legacyIng, err = crud.C.NetworkingV1beta1().Ingresses(ing.Namespace).Update(context.TODO(), legacyIng, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return toIngressV1(legacyIng)
}

// Patch Ingress resource.
func (crud *IngressCRUD) Patch(oldV1Ing, newV1Ing *v1.Ingress) (*v1.Ingress, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}

	var oldIng, newIng, dataStruct interface{}
	if useV1 {
		oldIng = oldV1Ing
		newIng = newV1Ing
		dataStruct = v1.Ingress{}
	} else {
		if oldIng, err = toIngressV1beta1(oldV1Ing); err != nil {
			return nil, err
		}
		if newIng, err = toIngressV1beta1(oldV1Ing); err != nil {
			return nil, err
		}
		dataStruct = v1beta1.Ingress{}
	}

	ingKey := fmt.Sprintf("%s/%s", oldV1Ing.Namespace, oldV1Ing.Name)
	oldData, err := json.Marshal(oldIng)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ingress %+v: %v", oldIng, err)
	}
	newData, err := json.Marshal(newIng)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ingress %+v: %v", newIng, err)
	}
	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, dataStruct)
	if err != nil {
		return nil, fmt.Errorf("failed to create TwoWayMergePatch for ingress %s: %v, old ingress: %+v new ingress: %+v", ingKey, err, oldIng, newIng)
	}

	klog.Infof("Patch %s/%s", oldV1Ing.Namespace, oldV1Ing.Name)
	if useV1 {
		return crud.C.NetworkingV1().Ingresses(oldV1Ing.Namespace).Patch(context.TODO(), oldV1Ing.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	}

	klog.Warning("Using v1beta1 API")
	legacyIng, err := crud.C.NetworkingV1beta1().Ingresses(oldV1Ing.Namespace).Patch(context.TODO(), oldV1Ing.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return nil, err
	}
	return toIngressV1(legacyIng)
}

// Delete Ingress resource.
func (crud *IngressCRUD) Delete(ns, name string) error {
	new, err := crud.supportsV1API()
	if err != nil {
		return err
	}
	klog.Infof("Delete %s/%s", ns, name)
	if new {
		return crud.C.NetworkingV1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	}
	klog.Warning("Using v1beta1 API")
	return crud.C.NetworkingV1beta1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

func (crud *IngressCRUD) supportsV1API() (bool, error) {
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion(v1GroupVersion); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == "Ingress" {
				return true, nil
			}
		}
	}
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion(v1beta1GroupVersion); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == "Ingress" {
				return false, nil
			}
		}
	}
	return false, errors.New("no Ingress resource found")
}

func toIngressV1(ing *v1beta1.Ingress) (*v1.Ingress, error) {
	v1Ing := &v1.Ingress{}
	err := Convert_v1beta1_Ingress_To_networking_Ingress(ing, v1Ing, nil)
	v1Ing.APIVersion = v1GroupVersion
	return v1Ing, err
}

func toIngressV1beta1(ing *v1.Ingress) (*v1beta1.Ingress, error) {
	v1beta1Ing := &v1beta1.Ingress{}
	err := Convert_networking_Ingress_To_v1beta1_Ingress(ing, v1beta1Ing, nil)
	v1beta1Ing.APIVersion = v1beta1GroupVersion
	return v1beta1Ing, err
}
