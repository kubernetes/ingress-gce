/*
Copyright 2026 The Kubernetes Authors.

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

package negstatushandler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	composite "k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	negbindingclient "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

type negOwnershipRegistry interface {
	GetOwner(negName string) string
}

const (
	BackendRefAttachedCondition = "BackendRefAttached"
	ManagedCondition            = "Managed"
	NegsAttachedCondition       = "NegsAttached"

	BackendRefValidReason       = "BackendRefValid"
	InvalidBackendRefKindReason = "InvalidBackendRefKind"
	ServiceNotFoundReason       = "ServiceNotFound"
	InvalidBackendRefReason     = "InvalidBackendRef"
	NEGOwnershipNoConflicts     = "NEGOwnershipNoConflicts"
	NEGOwnershipConflict        = "NEGOwnershipConflict"
	NEGsAttachmentSuccessful    = "NEGsAttachmentSuccessful"
	NEGsAttachmentFailed        = "NEGsAttachmentFailed"
)

var (
	ErrServiceNotFound       = errors.New("service not found in cache")
	ErrInvalidBackendRef     = errors.New("BackendRef is nil")
	ErrInvalidBackendRefKind = errors.New("unsupported BackendRef Kind")
)

// NEGBindingStatusHandler implements NEGStatusHandler for NetworkEndpointGroupBinding custom resource.
type NEGBindingStatusHandler struct {
	negBindingName   string
	namespace        string
	negBindingClient negbindingclient.Interface
	negBindingLister cache.Indexer
	negMetrics       *metrics.NegMetrics
	registry         negOwnershipRegistry
	logger           klog.Logger

	patchMu           sync.Mutex
	lastPatchedUID    types.UID
	lastPatchedStatus *negbindingv1beta1.NetworkEndpointGroupBindingStatus
}

// NewNEGBindingStatusHandler constructs a new NEGBindingStatusHandler.
func NewNEGBindingStatusHandler(
	negBindingName string,
	namespace string,
	negBindingClient negbindingclient.Interface,
	negBindingLister cache.Indexer,
	negMetrics *metrics.NegMetrics,
	registry negOwnershipRegistry,
	logger klog.Logger,
) *NEGBindingStatusHandler {
	return &NEGBindingStatusHandler{
		namespace:        namespace,
		negBindingName:   negBindingName,
		negBindingClient: negBindingClient,
		negBindingLister: negBindingLister,
		negMetrics:       negMetrics,
		registry:         registry,
		logger:           logger.WithName("NEGBindingStatusHandler").WithValues("binding", fmt.Sprintf("%s/%s", namespace, negBindingName)),
	}
}

func (h *NEGBindingStatusHandler) getBinding() (*negbindingv1beta1.NetworkEndpointGroupBinding, error) {
	key := fmt.Sprintf("%s/%s", h.namespace, h.negBindingName)
	obj, exists, err := h.negBindingLister.GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("error getting negbinding from cache: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("negbinding %s is not in store", key)
	}
	binding, ok := obj.(*negbindingv1beta1.NetworkEndpointGroupBinding)
	if !ok {
		return nil, fmt.Errorf("cached object %q is of type %T, expected *NetworkEndpointGroupBinding", key, obj)
	}
	return binding, nil
}

// SubnetToZonesMap returns the current subnet-to-zones mapping reported inside the NEGBinding CR status.
func (h *NEGBindingStatusHandler) SubnetToZonesMap() (shared.ZonesPerSubnetMap, error) {
	binding, err := h.getBinding()
	if err != nil {
		return nil, err
	}

	subnetToZones := make(shared.ZonesPerSubnetMap)
	for _, ref := range binding.Status.NetworkEndpointGroups {
		subnetID, err := cloud.ParseResourceURL(ref.SubnetURL)
		if err != nil {
			h.logger.Error(err, "Failed to parse subnet URL from status", "subnetURL", ref.SubnetURL)
			h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		negID, err := cloud.ParseResourceURL(ref.ResourceURL)
		if err != nil {
			h.logger.Error(err, "Failed to parse NEG URL from status", "resourceURL", ref.ResourceURL)
			h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}

		subnetName := subnetID.Key.Name
		zone := negID.Key.Zone
		if _, ok := subnetToZones[subnetName]; !ok {
			subnetToZones[subnetName] = sets.New[string]()
		}
		subnetToZones[subnetName].Insert(zone)
	}
	return subnetToZones, nil
}

// UpdateBackendRefAttachedCondition updates the BackendRefAttached condition inside the NEGBinding CR status.
func (h *NEGBindingStatusHandler) UpdateBackendRefAttachedCondition(validationErr error) error {
	h.patchMu.Lock()
	defer h.patchMu.Unlock()

	origBinding, err := h.getBinding()
	if err != nil {
		h.logger.Error(err, "Error updating BackendRefAttached status for NEGBinding, failed to get NEGBinding from store")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return err
	}

	binding := h.bindingForPatchBase(origBinding)
	h.ensureCondition(binding, h.getBackendRefAttachedCondition(validationErr))

	err = h.patchNegBindingStatus(origBinding.Status, binding.Status)
	if err != nil {
		h.logger.Error(err, "Error updating NEGBinding status")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return err
	}
	h.saveLastPatchedStatus(origBinding.UID, binding.Status)
	return nil
}

// ReportStatus reports the updated list of successfully configured GCE NEGs inside the NEGBinding CR status.
func (h *NEGBindingStatusHandler) ReportStatus(negs []*composite.NetworkEndpointGroup, errList []error) error {
	h.patchMu.Lock()
	defer h.patchMu.Unlock()

	origBinding, err := h.getBinding()
	if err != nil {
		h.logger.Error(err, "Error updating init status for NEGBinding, failed to get NEGBinding from store.")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return err
	}

	statusNegs := make([]negbindingv1beta1.StatusNegRef, len(negs))
	for i, neg := range negs {
		statusNegs[i] = negbindingv1beta1.StatusNegRef{
			ResourceURL: neg.SelfLink,
			SubnetURL:   neg.Subnetwork,
		}
	}

	binding := h.bindingForPatchBase(origBinding)
	binding.Status.NetworkEndpointGroups = statusNegs

	managedCond := h.getManagedCondition(binding)
	h.ensureCondition(binding, managedCond)

	attachedCondition := h.getAttachedCondition(utilerrors.NewAggregate(errList))
	finalCondition := h.ensureCondition(binding, attachedCondition)
	h.negMetrics.PublishNegInitializationMetrics(finalCondition.LastTransitionTime.Sub(origBinding.GetCreationTimestamp().Time))

	err = h.patchNegBindingStatus(origBinding.Status, binding.Status)
	if err != nil {
		h.logger.Error(err, "Error updating NEGBinding status")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return err
	}
	h.saveLastPatchedStatus(origBinding.UID, binding.Status)
	return nil
}

// ReportSyncStatus reports the result of a sync operation. It returns true if
// the syncer needs to re-initialize NEGs on next sync.
func (h *NEGBindingStatusHandler) ReportSyncStatus(syncErr error) (bool, error) {
	h.patchMu.Lock()
	defer h.patchMu.Unlock()

	origBinding, err := h.getBinding()
	if err != nil {
		h.logger.Error(err, "Error updating status for NEGBinding, failed to get NEGBinding from store")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return false, err
	}
	binding := h.bindingForPatchBase(origBinding)

	ts := metav1.Now()
	needInit := false
	if _, _, exists := h.findCondition(binding.Status.Conditions, NegsAttachedCondition); !exists {
		needInit = true
	}
	h.negMetrics.PublishNegSyncerStalenessMetrics(ts.Sub(binding.Status.LastSyncTime.Time))

	h.ensureCondition(binding, h.getManagedCondition(binding))
	h.ensureCondition(binding, h.getAttachedCondition(syncErr))
	binding.Status.LastSyncTime = ts

	if len(binding.Status.NetworkEndpointGroups) == 0 {
		needInit = true
	}

	err = h.patchNegBindingStatus(origBinding.Status, binding.Status)
	if err != nil {
		h.logger.Error(err, "Error updating NEGBinding status")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return needInit, err
	}
	h.saveLastPatchedStatus(origBinding.UID, binding.Status)
	return needInit, nil
}

// LastSyncTime returns the last time the NEG syncer successfully synced associated NEGs.
func (h *NEGBindingStatusHandler) LastSyncTime() (time.Time, error) {
	binding, err := h.getBinding()
	if err != nil {
		return time.Time{}, err
	}
	return binding.Status.LastSyncTime.Time, nil
}

// bindingForPatchBase returns a copy of origBinding with lastPatchedStatus applied so new patches
// do not overwrite fields from recent patches that have not propagated to the informer cache yet.
// Cached status is ignored if the binding has a different UID.
func (h *NEGBindingStatusHandler) bindingForPatchBase(origBinding *negbindingv1beta1.NetworkEndpointGroupBinding) *negbindingv1beta1.NetworkEndpointGroupBinding {
	baseBinding := origBinding.DeepCopy()
	if h.lastPatchedStatus != nil && h.lastPatchedUID == origBinding.UID {
		baseBinding.Status = *h.lastPatchedStatus.DeepCopy()
	}
	return baseBinding
}

func (h *NEGBindingStatusHandler) saveLastPatchedStatus(uid types.UID, status negbindingv1beta1.NetworkEndpointGroupBindingStatus) {
	h.lastPatchedStatus = status.DeepCopy()
	h.lastPatchedUID = uid
}

func (h *NEGBindingStatusHandler) ensureCondition(binding *negbindingv1beta1.NetworkEndpointGroupBinding, expectedCondition negbindingv1beta1.Condition) negbindingv1beta1.Condition {
	condition, index, exists := h.findCondition(binding.Status.Conditions, expectedCondition.Type)
	if !exists {
		binding.Status.Conditions = append(binding.Status.Conditions, expectedCondition)
		return expectedCondition
	}

	if condition.Status == expectedCondition.Status {
		expectedCondition.LastTransitionTime = condition.LastTransitionTime
	}

	binding.Status.Conditions[index] = expectedCondition
	return expectedCondition
}

func (h *NEGBindingStatusHandler) getBackendRefAttachedCondition(validationErr error) negbindingv1beta1.Condition {
	cond := negbindingv1beta1.Condition{
		Type:               BackendRefAttachedCondition,
		LastTransitionTime: metav1.Now(),
	}
	if validationErr == nil {
		cond.Status = metav1.ConditionTrue
		cond.Reason = BackendRefValidReason
		return cond
	}

	cond.Status = metav1.ConditionFalse
	cond.Message = validationErr.Error()
	if errors.Is(validationErr, ErrInvalidBackendRefKind) {
		cond.Reason = InvalidBackendRefKindReason
	} else if errors.Is(validationErr, ErrServiceNotFound) {
		cond.Reason = ServiceNotFoundReason
	} else if errors.Is(validationErr, ErrInvalidBackendRef) {
		cond.Reason = InvalidBackendRefReason
	} else {
		cond.Reason = InvalidBackendRefReason
	}
	return cond
}

func (h *NEGBindingStatusHandler) getAttachedCondition(err error) negbindingv1beta1.Condition {
	if err != nil {
		return negbindingv1beta1.Condition{
			Type:               NegsAttachedCondition,
			Status:             metav1.ConditionFalse,
			Reason:             NEGsAttachmentFailed,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
	}

	return negbindingv1beta1.Condition{
		Type:               NegsAttachedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             NEGsAttachmentSuccessful,
		LastTransitionTime: metav1.Now(),
		Message:            "NEGs have been successfully attached and synced",
	}
}

func (h *NEGBindingStatusHandler) findCondition(conditions []negbindingv1beta1.Condition, conditionType string) (negbindingv1beta1.Condition, int, bool) {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return condition, i, true
		}
	}
	return negbindingv1beta1.Condition{}, -1, false
}

func (h *NEGBindingStatusHandler) patchNegBindingStatus(oldStatus, newStatus negbindingv1beta1.NetworkEndpointGroupBindingStatus) error {
	patchBytes, err := patch.MergePatchBytes(negbindingv1beta1.NetworkEndpointGroupBinding{Status: oldStatus}, negbindingv1beta1.NetworkEndpointGroupBinding{Status: newStatus})
	if err != nil {
		return fmt.Errorf("failed to prepare patch bytes: %w", err)
	}

	// Skip patch if no changes to be done
	if string(patchBytes) == "{}" {
		return nil
	}

	start := time.Now()
	_, err = h.negBindingClient.NetworkingV1beta1().NetworkEndpointGroupBindings(h.namespace).Patch(context.Background(), h.negBindingName, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	h.negMetrics.PublishK8sRequestCountMetrics(start, metrics.PatchRequest, err)
	return err
}

func (h *NEGBindingStatusHandler) getManagedCondition(binding *negbindingv1beta1.NetworkEndpointGroupBinding) negbindingv1beta1.Condition {
	ownerKey := fmt.Sprintf("%s/%s", h.namespace, h.negBindingName)
	var conflicts []string
	for _, ref := range binding.Spec.NetworkEndpointGroups {
		owner := h.registry.GetOwner(ref.Name)
		if owner != "" && owner != ownerKey {
			conflicts = append(conflicts, fmt.Sprintf("NEG %q is owned by %q", ref.Name, owner))
		}
	}
	if len(conflicts) == 0 {
		return negbindingv1beta1.Condition{
			Type:               ManagedCondition,
			Status:             metav1.ConditionTrue,
			Reason:             NEGOwnershipNoConflicts,
			LastTransitionTime: metav1.Now(),
			Message:            "All NEGs from Spec currently managed",
		}
	}

	message := strings.Join(conflicts, "; ")
	return negbindingv1beta1.Condition{
		Type:               ManagedCondition,
		Status:             metav1.ConditionFalse,
		Reason:             NEGOwnershipConflict,
		LastTransitionTime: metav1.Now(),
		Message:            message,
	}
}
