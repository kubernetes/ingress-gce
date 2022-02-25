package adapter

// Legacy conversion code for the old extensions v1beta1 API group.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	sav1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog"
)

const (
	v1PSC                 = "networking.gke.io/v1"
	v1beta1PSC            = "networking.gke.io/v1beta1"
	serviceAttachmentKind = "ServiceAttachment"
)

//TODO(srepakula) Remove adapter client once v1beta1 is no longer supported

// ServiceAttachmentCRUD wraps basic CRUD to allow use of old and new APIs.
type ServiceAttachmentCRUD struct {
	C serviceattachmentclient.Interface
}

// Get ServiceAttachment resource.
func (crud *ServiceAttachmentCRUD) Get(ns, name string) (*sav1.ServiceAttachment, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}
	klog.Infof("Get %s/%s", ns, name)
	if useV1 {
		return crud.C.NetworkingV1().ServiceAttachments(ns).Get(context.TODO(), name, metav1.GetOptions{})
	}
	klog.Warning("Using v1beta1 API")
	sa, err := crud.C.NetworkingV1beta1().ServiceAttachments(ns).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return toPSCV1(sa), nil
}

// Create ServiceAttachment resource.
func (crud *ServiceAttachmentCRUD) Create(sa *sav1.ServiceAttachment) (*sav1.ServiceAttachment, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}
	klog.Infof("Create %s/%s", sa.Namespace, sa.Name)
	if useV1 {
		return crud.C.NetworkingV1().ServiceAttachments(sa.Namespace).Create(context.TODO(), sa, metav1.CreateOptions{})
	}

	klog.Warning("Using v1beta1 API")
	legacySA := toPSCV1beta1(sa)

	legacySA, err = crud.C.NetworkingV1beta1().ServiceAttachments(sa.Namespace).Create(context.TODO(), legacySA, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return toPSCV1(legacySA), nil
}

// Update ServiceAttachment resource.
func (crud *ServiceAttachmentCRUD) Update(sa *sav1.ServiceAttachment) (*sav1.ServiceAttachment, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}
	klog.Infof("Update %s/%s", sa.Namespace, sa.Name)
	if useV1 {
		return crud.C.NetworkingV1().ServiceAttachments(sa.Namespace).Update(context.TODO(), sa, metav1.UpdateOptions{})
	}

	klog.Warning("Using legacy API")
	legacySA := toPSCV1beta1(sa)

	legacySA, err = crud.C.NetworkingV1beta1().ServiceAttachments(sa.Namespace).Update(context.TODO(), legacySA, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return toPSCV1(legacySA), nil
}

// Patch ServiceAttachment resource.
func (crud *ServiceAttachmentCRUD) Patch(oldV1SA, newV1SA *sav1.ServiceAttachment) (*sav1.ServiceAttachment, error) {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return nil, err
	}

	var oldSA, newSA interface{}
	if useV1 {
		oldSA = oldV1SA
		newSA = newV1SA
	} else {
		oldSA = toPSCV1beta1(oldV1SA)
		newSA = toPSCV1beta1(newV1SA)
	}

	saKey := fmt.Sprintf("%s/%s", oldV1SA.Namespace, oldV1SA.Name)
	oldData, err := json.Marshal(oldSA)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal service attachment %+v: %v", oldSA, err)
	}
	newData, err := json.Marshal(newSA)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal service attachment %+v: %v", newSA, err)
	}

	patchBytes, err := patch.MergePatchBytes(oldData, newData)
	if err != nil {
		return nil, fmt.Errorf("failed to create MergePatch for service attachment %s: %v, old sa: %+v new sa: %+v", saKey, err, oldSA, newSA)
	}

	klog.Infof("Patch %s/%s", oldV1SA.Namespace, oldV1SA.Name)
	if useV1 {
		return crud.C.NetworkingV1().ServiceAttachments(oldV1SA.Namespace).Patch(context.TODO(), oldV1SA.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	}

	klog.Warning("Using v1beta1 API")
	legacyIng, err := crud.C.NetworkingV1beta1().ServiceAttachments(oldV1SA.Namespace).Patch(context.TODO(), oldV1SA.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		return nil, err
	}
	return toPSCV1(legacyIng), nil
}

// Delete ServiceAttachment resource.
func (crud *ServiceAttachmentCRUD) Delete(ns, name string) error {
	useV1, err := crud.supportsV1API()
	if err != nil {
		return err
	}
	klog.Infof("Delete %s/%s", ns, name)
	if useV1 {
		return crud.C.NetworkingV1().ServiceAttachments(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
	}
	klog.Warning("Using v1beta1 API")
	return crud.C.NetworkingV1beta1().ServiceAttachments(ns).Delete(context.TODO(), name, metav1.DeleteOptions{})
}

// supportsV1API returns true if the V1 API is available
func (crud *ServiceAttachmentCRUD) supportsV1API() (bool, error) {
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion(v1PSC); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == serviceAttachmentKind {
				return true, nil
			}
		}
	}
	if apiList, err := crud.C.Discovery().ServerResourcesForGroupVersion(v1beta1PSC); err == nil {
		for _, r := range apiList.APIResources {
			if r.Kind == serviceAttachmentKind {
				return false, nil
			}
		}
	}
	return false, errors.New("no Service Attachment resource found")
}

// toPSCV1 converts the v1beta1 object to a v1 object
func toPSCV1(in *sav1beta1.ServiceAttachment) *sav1.ServiceAttachment {
	out := &sav1.ServiceAttachment{}
	out.ObjectMeta = in.ObjectMeta
	out.TypeMeta = metav1.TypeMeta{
		Kind:       in.Kind,
		APIVersion: v1PSC,
	}

	out.Spec = sav1.ServiceAttachmentSpec{
		ConnectionPreference: in.Spec.ConnectionPreference,
		NATSubnets:           in.Spec.NATSubnets,
		ResourceRef:          in.Spec.ResourceRef,
		ProxyProtocol:        in.Spec.ProxyProtocol,
		ConsumerAllowList:    convertConsumerProjectToV1(in.Spec.ConsumerAllowList),
		ConsumerRejectList:   in.Spec.ConsumerRejectList,
	}

	out.Status = sav1.ServiceAttachmentStatus{
		ServiceAttachmentURL:    in.Status.ServiceAttachmentURL,
		ForwardingRuleURL:       in.Status.ForwardingRuleURL,
		ConsumerForwardingRules: convertForwardingRulesToV1(in.Status.ConsumerForwardingRules),
		LastModifiedTimestamp:   in.Status.LastModifiedTimestamp,
	}

	return out
}

// convertConsumerProjectToV1 converts a list of v1beta1 Consumer Projects to a v1 list
func convertConsumerProjectToV1(in []sav1beta1.ConsumerProject) []sav1.ConsumerProject {
	var out []sav1.ConsumerProject
	for _, proj := range in {
		out = append(out, sav1.ConsumerProject(proj))
	}
	return out
}

// convertConsumerForwardingRulesToV1 converts a list of v1beta Consumer Forwarding Rules to a v1 list
func convertForwardingRulesToV1(in []sav1beta1.ConsumerForwardingRule) []sav1.ConsumerForwardingRule {
	var out []sav1.ConsumerForwardingRule
	for _, rule := range in {
		out = append(out, sav1.ConsumerForwardingRule(rule))
	}
	return out
}

// toPSCV1beta1 converts the v1 object to a v1beta1 object
func toPSCV1beta1(in *sav1.ServiceAttachment) *sav1beta1.ServiceAttachment {
	out := &sav1beta1.ServiceAttachment{}
	out.ObjectMeta = in.ObjectMeta
	out.TypeMeta = metav1.TypeMeta{
		Kind:       in.Kind,
		APIVersion: v1beta1PSC,
	}

	out.Spec = sav1beta1.ServiceAttachmentSpec{
		ConnectionPreference: in.Spec.ConnectionPreference,
		NATSubnets:           in.Spec.NATSubnets,
		ResourceRef:          in.Spec.ResourceRef,
		ProxyProtocol:        in.Spec.ProxyProtocol,
		ConsumerAllowList:    convertConsumerProjectToV1beta1(in.Spec.ConsumerAllowList),
		ConsumerRejectList:   in.Spec.ConsumerRejectList,
	}

	out.Status = sav1beta1.ServiceAttachmentStatus{
		ServiceAttachmentURL:    in.Status.ServiceAttachmentURL,
		ForwardingRuleURL:       in.Status.ForwardingRuleURL,
		ConsumerForwardingRules: convertForwardingRulesToV1beta1(in.Status.ConsumerForwardingRules),
		LastModifiedTimestamp:   in.Status.LastModifiedTimestamp,
	}

	return out
}

// convertConsumerProjectToV1 converts a list of v1 Consumer Projects to a v1beta1 list
func convertConsumerProjectToV1beta1(in []sav1.ConsumerProject) []sav1beta1.ConsumerProject {
	var out []sav1beta1.ConsumerProject
	for _, proj := range in {
		out = append(out, sav1beta1.ConsumerProject(proj))
	}
	return out
}

// convertConsumerForwardingRulesToV1 converts a list of v1 Consumer Forwarding Rules to a v1beta1 list
func convertForwardingRulesToV1beta1(in []sav1.ConsumerForwardingRule) []sav1beta1.ConsumerForwardingRule {
	var out []sav1beta1.ConsumerForwardingRule
	for _, rule := range in {
		out = append(out, sav1beta1.ConsumerForwardingRule(rule))
	}
	return out
}
