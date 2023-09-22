package adapter

// Legacy conversion code for the old extensions v1beta1 API group.

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// IngressCRUD wraps basic CRUD to allow use of old and new APIs.
type IngressCRUD struct {
	C kubernetes.Interface
}

// Get Ingress resource.
func (crud *IngressCRUD) Get(ns, name string) (*v1.Ingress, error) {
	klog.Infof("Get %s/%s", ns, name)
	return crud.C.NetworkingV1().Ingresses(ns).Get(context.TODO(), name, metav1.GetOptions{})
}

// Create Ingress resource.
func (crud *IngressCRUD) Create(ing *v1.Ingress) (*v1.Ingress, error) {
	klog.Infof("Create %s/%s", ing.Namespace, ing.Name)
	return crud.C.NetworkingV1().Ingresses(ing.Namespace).Create(context.TODO(), ing, metav1.CreateOptions{})
}

// Update Ingress resource.
func (crud *IngressCRUD) Update(ing *v1.Ingress) (*v1.Ingress, error) {
	klog.Infof("Update %s/%s", ing.Namespace, ing.Name)
	return crud.C.NetworkingV1().Ingresses(ing.Namespace).Update(context.TODO(), ing, metav1.UpdateOptions{})
}

// Patch Ingress resource.
func (crud *IngressCRUD) Patch(oldIng, newIng *v1.Ingress) (*v1.Ingress, error) {
	var dataStruct interface{}
	dataStruct = v1.Ingress{}

	ingKey := fmt.Sprintf("%s/%s", oldIng.Namespace, oldIng.Name)
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

	klog.Infof("Patch %s/%s", oldIng.Namespace, oldIng.Name)
	return crud.C.NetworkingV1().Ingresses(oldIng.Namespace).Patch(context.TODO(), oldIng.Name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
}

// Delete Ingress resource.
func (crud *IngressCRUD) Delete(ns, name string) error {
	klog.Infof("Delete %s/%s", ns, name)
	return crud.C.NetworkingV1().Ingresses(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}
