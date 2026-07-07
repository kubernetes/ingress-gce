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
	"fmt"

	"time"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/neg/types/shared"
	"k8s.io/ingress-gce/pkg/network"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

// SvcNegStatusHandler implements StatusReporter for SvcNEG CRD.
type SvcNegStatusHandler struct {
	svcNEGClient svcnegclient.Interface
	svcNEGLister cache.Indexer
	namespace    string
	svcNegName   string
	networkInfo  network.NetworkInfo
	zoneGetter   *zonegetter.ZoneGetter
	negMetrics   *metrics.NegMetrics
	logger       klog.Logger
}

func NewSvcNegStatusHandler(
	client svcnegclient.Interface,
	lister cache.Indexer,
	namespace string,
	svcNegName string,
	networkInfo network.NetworkInfo,
	zoneGetter *zonegetter.ZoneGetter,
	negMetrics *metrics.NegMetrics,
	logger klog.Logger,
) *SvcNegStatusHandler {
	return &SvcNegStatusHandler{
		svcNEGClient: client,
		svcNEGLister: lister,
		namespace:    namespace,
		svcNegName:   svcNegName,
		networkInfo:  networkInfo,
		zoneGetter:   zoneGetter,
		negMetrics:   negMetrics,
		logger:       logger.WithValues("svcneg", klog.KRef(namespace, svcNegName)),
	}
}

// SubnetToZonesMap parses the stored GKE SvcNEG object references inside status,
// and resolves their subnetwork URL names mapping to the set of their GCE zone names.
func (h *SvcNegStatusHandler) SubnetToZonesMap() (shared.ZonesPerSubnetMap, error) {
	svcNegCR, err := h.getSvcNegFromStore()
	if err != nil {
		return nil, err
	}
	subnetZones := make(shared.ZonesPerSubnetMap)
	for _, ref := range svcNegCR.Status.NetworkEndpointGroups {
		subnetURL := h.networkInfo.SubnetworkURL
		if ref.SubnetURL != "" {
			subnetURL = ref.SubnetURL
		}
		subnetID, err := cloud.ParseResourceURL(subnetURL)
		if err != nil {
			h.logger.Error(err, "unable to parse subnet url", "url", subnetURL)
			h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		id, err := cloud.ParseResourceURL(ref.SelfLink)
		if err != nil {
			h.logger.Error(err, "unable to parse selflink", "selfLink", ref.SelfLink)
			h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		subnetName := subnetID.Key.Name
		if _, ok := subnetZones[subnetName]; !ok {
			subnetZones[subnetName] = sets.New[string]()
		}
		subnetZones[subnetName].Insert(id.Key.Zone)
	}
	return subnetZones, nil
}

// ReportStatus reports the updated list of successfully configured GCE NEGs inside SvcNEG status.
func (h *SvcNegStatusHandler) ReportStatus(negs []*composite.NetworkEndpointGroup, errList []error) error {
	origSvcNeg, err := h.getSvcNegFromStore()
	if err != nil {
		h.logger.Error(err, "Error updating init status for SvcNEG, failed to get SvcNEG from store.")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return err
	}

	negObjRefs := make([]negv1beta1.NegObjectReference, len(negs))
	for i, neg := range negs {
		negRef := negv1beta1.NegObjectReference{
			Id:                  fmt.Sprint(neg.Id),
			SelfLink:            neg.SelfLink,
			NetworkEndpointType: negv1beta1.NetworkEndpointType(neg.NetworkEndpointType),
		}
		if flags.F.EnableMultiSubnetClusterPhase1 {
			negRef.State = negv1beta1.ActiveState
			negRef.SubnetURL = neg.Subnetwork
		}
		negObjRefs[i] = negRef
	}

	svcNeg := origSvcNeg.DeepCopy()
	if flags.F.EnableMultiSubnetClusterPhase1 {
		nonActiveNegRefs := h.getNonActiveNegRefs(origSvcNeg.Status.NetworkEndpointGroups, negObjRefs, h.zoneGetter.ListSubnetsInDefaultNetwork(h.logger), h.networkInfo.SubnetworkURL)
		negObjRefs = append(negObjRefs, nonActiveNegRefs...)
	}
	svcNeg.Status.NetworkEndpointGroups = negObjRefs

	initializedCondition := h.getInitializedCondition(utilerrors.NewAggregate(errList))
	finalCondition := h.ensureCondition(svcNeg, initializedCondition)
	h.negMetrics.PublishNegInitializationMetrics(finalCondition.LastTransitionTime.Sub(origSvcNeg.GetCreationTimestamp().Time))

	_, err = h.patchSvcNegStatus(origSvcNeg.Status, svcNeg.Status)
	if err != nil {
		h.logger.Error(err, "Error updating SvcNeg CR")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return err
	}
	return nil
}

// ReportSyncStatus reports the result of a sync operation. It returns true if
// the syncer needs to re-initialize NEGs on next sync.
func (h *SvcNegStatusHandler) ReportSyncStatus(syncErr error) (needInit bool, err error) {
	origSvcNeg, err := h.getSvcNegFromStore()
	if err != nil {
		h.logger.Error(err, "Error updating status for SvcNEG, failed to get SvcNEG from store")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return false, err
	}
	svcNeg := origSvcNeg.DeepCopy()

	ts := metav1.Now()
	if _, _, exists := h.findCondition(svcNeg.Status.Conditions, negv1beta1.Initialized); !exists {
		needInit = true
	}
	h.negMetrics.PublishNegSyncerStalenessMetrics(ts.Sub(svcNeg.Status.LastSyncTime.Time))

	h.ensureCondition(svcNeg, h.getSyncedCondition(syncErr))
	svcNeg.Status.LastSyncTime = ts

	if len(svcNeg.Status.NetworkEndpointGroups) == 0 {
		needInit = true
	}

	_, err = h.patchSvcNegStatus(origSvcNeg.Status, svcNeg.Status)
	if err != nil {
		h.logger.Error(err, "Error updating SvcNeg CR")
		h.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return needInit, err
	}
	return needInit, nil
}

// LastSyncTime returns the last time the NEG syncer successfully synced associated NEGs.
func (h *SvcNegStatusHandler) LastSyncTime() (time.Time, error) {
	svcNegCR, err := h.getSvcNegFromStore()
	if err != nil {
		return time.Time{}, err
	}
	return svcNegCR.Status.LastSyncTime.Time, nil
}

func (h *SvcNegStatusHandler) getSvcNegFromStore() (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	n, exists, err := h.svcNEGLister.GetByKey(fmt.Sprintf("%s/%s", h.namespace, h.svcNegName))
	if err != nil {
		return nil, fmt.Errorf("error getting svcneg %s/%s from cache: %w", h.namespace, h.svcNegName, err)
	}
	if !exists {
		return nil, fmt.Errorf("svcneg %s/%s is not in store", h.namespace, h.svcNegName)
	}

	return n.(*negv1beta1.ServiceNetworkEndpointGroup), nil
}

// patchSvcNegStatus patches the specified SvcNeg status with the provided new status
func (h *SvcNegStatusHandler) patchSvcNegStatus(oldStatus, newStatus negv1beta1.ServiceNetworkEndpointGroupStatus) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	patchBytes, err := patch.MergePatchBytes(negv1beta1.ServiceNetworkEndpointGroup{Status: oldStatus}, negv1beta1.ServiceNetworkEndpointGroup{Status: newStatus})
	if err != nil {
		return nil, fmt.Errorf("failed to prepare patch bytes: %w", err)
	}

	start := time.Now()
	svcNeg, err := h.svcNEGClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(h.namespace).Patch(context.Background(), h.svcNegName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	h.negMetrics.PublishK8sRequestCountMetrics(start, metrics.PatchRequest, err)
	return svcNeg, err
}

// ensureCondition will update the condition on the SvcNeg object if necessary
func (h *SvcNegStatusHandler) ensureCondition(svcNeg *negv1beta1.ServiceNetworkEndpointGroup, expectedCondition negv1beta1.Condition) negv1beta1.Condition {
	condition, index, exists := h.findCondition(svcNeg.Status.Conditions, expectedCondition.Type)
	if !exists {
		svcNeg.Status.Conditions = append(svcNeg.Status.Conditions, expectedCondition)
		return expectedCondition
	}

	if condition.Status == expectedCondition.Status {
		expectedCondition.LastTransitionTime = condition.LastTransitionTime
	}

	svcNeg.Status.Conditions[index] = expectedCondition
	return expectedCondition
}

// getNonActiveNegRefs creates NEG references for NEGs in Inactive State and ToBeDeleted state.
// Inactive NEG are NEGs that are in zones that cluster is no longer in.
// ToBeDeleted NEGs are NEGs in subnets that no longer exist on the Topology CRD
func (h *SvcNegStatusHandler) getNonActiveNegRefs(oldNegRefs []negv1beta1.NegObjectReference, currentNegRefs []negv1beta1.NegObjectReference, subnetConfigs []nodetopologyv1.SubnetConfig, defaultSubnetURL string) []negv1beta1.NegObjectReference {
	subnets := sets.New[string]()
	for _, subnet := range subnetConfigs {
		subnets.Insert(subnet.Name)
	}

	activeNegs := sets.New[negtypes.NegInfo]()
	for _, negRef := range currentNegRefs {
		negInfo, err := negtypes.NegInfoFromNegRef(negRef)
		if err != nil {
			h.logger.Error(err, "Failed to extract name and zone information of a neg from the current snapshot", "negId", negRef.Id, "negSelfLink", negRef.SelfLink)
			continue
		}
		activeNegs.Insert(negInfo)
	}

	var nonActiveNegRefs []negv1beta1.NegObjectReference
	for _, origNegRef := range oldNegRefs {
		negInfo, err := negtypes.NegInfoFromNegRef(origNegRef)
		if err != nil {
			h.logger.Error(err, "Failed to extract name and zone information of a neg from the previous snapshot, skipping validating if it is an Inactive NEG", "negId", origNegRef.Id, "negSelfLink", origNegRef.SelfLink)
			continue
		}

		if activeNegs.Has(negInfo) {
			continue
		}
		// NEGs are listed based on the current node zones. If a NEG no longer
		// exists in the current list, it means there are no nodes/endpoints
		// in that specific zone, and we mark it as INACTIVE.
		// We use SelfLink as identifier since it contains the unique NEG zone
		// and name pair.

		nonActiveNegRef := origNegRef.DeepCopy()
		nonActiveNegRef.State = negv1beta1.InactiveState

		// Empty subnet is a remanent from a previous version and is only possible
		// with a NEG from the default subnet. The ref should be updated with default
		// subnet.
		if nonActiveNegRef.SubnetURL == "" {
			nonActiveNegRef.SubnetURL = defaultSubnetURL
		}

		subnetID, err := cloud.ParseResourceURL(nonActiveNegRef.SubnetURL)
		if err != nil {
			h.logger.Error(err, "Failed to extract subnet information from the previous snapshot, skipping validating if it is an Inactive or to-be-deleted NEG", "negId", nonActiveNegRef.Id, "negSelfLink", nonActiveNegRef.SelfLink)
			continue
		}

		if !subnets.Has(subnetID.Key.Name) {
			nonActiveNegRef.State = negv1beta1.ToBeDeletedState
		}

		nonActiveNegRefs = append(nonActiveNegRefs, *nonActiveNegRef)
	}
	return nonActiveNegRefs
}

// getSyncedCondition returns the expected synced condition based on given error
func (h *SvcNegStatusHandler) getSyncedCondition(err error) negv1beta1.Condition {
	if err != nil {
		return negv1beta1.Condition{
			Type:               negv1beta1.Synced,
			Status:             v1.ConditionFalse,
			Reason:             negtypes.NegSyncFailed,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
	}

	return negv1beta1.Condition{
		Type:               negv1beta1.Synced,
		Status:             v1.ConditionTrue,
		Reason:             negtypes.NegSyncSuccessful,
		LastTransitionTime: metav1.Now(),
	}
}

// getInitializedCondition returns the expected initialized condition based on given error
func (h *SvcNegStatusHandler) getInitializedCondition(err error) negv1beta1.Condition {
	if err != nil {
		return negv1beta1.Condition{
			Type:               negv1beta1.Initialized,
			Status:             v1.ConditionFalse,
			Reason:             negtypes.NegInitializationFailed,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
	}

	return negv1beta1.Condition{
		Type:               negv1beta1.Initialized,
		Status:             v1.ConditionTrue,
		Reason:             negtypes.NegInitializationSuccessful,
		LastTransitionTime: metav1.Now(),
	}
}

// findCondition finds a condition in the given list of conditions that has the type conditionType and returns the condition and its index.
func (h *SvcNegStatusHandler) findCondition(conditions []negv1beta1.Condition, conditionType string) (negv1beta1.Condition, int, bool) {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return condition, i, true
		}
	}
	return negv1beta1.Condition{}, -1, false
}
