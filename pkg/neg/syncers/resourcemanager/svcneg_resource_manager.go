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

package resourcemanager

import (
	"fmt"
	"time"

	"context"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	gcpmeta "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apiv1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

type SvcNegResourceManager struct {
	kubeSystemUID string
	customName    bool
	namer         negtypes.NetworkEndpointGroupNamer
	negSyncerKey  negtypes.NegSyncerKey
	networkInfo   network.NetworkInfo
	zoneGetter    *zonegetter.ZoneGetter
	recorder      record.EventRecorder

	version    gcpmeta.Version
	cloud      negtypes.NetworkEndpointGroupCloud
	negMetrics *metrics.NegMetrics
	logger     klog.Logger

	serviceLister cache.Indexer
	svcNegLister  cache.Indexer
	svcNegClient  svcnegclient.Interface
}

func NewSvcNegResourceManager(
	kubeSystemUID string,
	customName bool,
	namer negtypes.NetworkEndpointGroupNamer,
	negSyncerKey negtypes.NegSyncerKey,
	networkInfo network.NetworkInfo,
	zoneGetter *zonegetter.ZoneGetter,
	recorder record.EventRecorder,
	cloud negtypes.NetworkEndpointGroupCloud,
	negMetrics *metrics.NegMetrics,
	logger klog.Logger,
	serviceLister cache.Indexer,
	svcNegLister cache.Indexer,
	svcNegClient svcnegclient.Interface,
) *SvcNegResourceManager {
	return &SvcNegResourceManager{
		kubeSystemUID: kubeSystemUID,
		customName:    customName,
		namer:         namer,
		negSyncerKey:  negSyncerKey,
		networkInfo:   networkInfo,
		zoneGetter:    zoneGetter,
		recorder:      recorder,
		cloud:         cloud,
		negMetrics:    negMetrics,
		logger:        logger,
		serviceLister: serviceLister,
		svcNegLister:  svcNegLister,
		svcNegClient:  svcNegClient,
	}
}

func (s *SvcNegResourceManager) UpdateStatusNegsEnsured(negObjs []*composite.NetworkEndpointGroup, errList []error) {
	if s.svcNegClient == nil {
		return
	}

	origSvcNeg, err := s.getSvcNEGFromStore()
	if err != nil {
		s.logger.Error(err, "Error updating init status for neg, failed to get neg from store.")
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}

	negRefs := make([]negv1beta1.NegObjectReference, len(negObjs))
	for i, obj := range negObjs {
		negRefs[i] = s.negToNegRef(obj)
	}

	svcNeg := origSvcNeg.DeepCopy()
	if flags.F.EnableMultiSubnetClusterPhase1 {
		nonActiveNegRefs := s.getNonActiveNegRefs(origSvcNeg.Status.NetworkEndpointGroups, negRefs)
		negRefs = append(negRefs, nonActiveNegRefs...)
	}
	svcNeg.Status.NetworkEndpointGroups = negRefs

	initializedCondition := s.getInitializedCondition(utilerrors.NewAggregate(errList))
	finalCondition := s.ensureCondition(svcNeg, initializedCondition)
	s.negMetrics.PublishNegInitializationMetrics(finalCondition.LastTransitionTime.Sub(origSvcNeg.GetCreationTimestamp().Time))

	_, err = s.patchSvcNegStatus(origSvcNeg.Status, svcNeg.Status)
	if err != nil {
		s.logger.Error(err, "Error updating SvcNeg CR")
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	}
}

// UpdateStatusSyncCompleted will update the Synced condition as needed on the corresponding SvcNeg CR.
// If the Initialized condition or NetworkEndpointGroups are missing, needInit will be set to true. LastSyncTime will be updated as well.
func (s *SvcNegResourceManager) UpdateStatusSyncCompleted(syncErr error) (needInit bool) {
	if s.svcNegClient == nil {
		return false
	}
	origSvcNeg, err := s.getSvcNEGFromStore()
	if err != nil {
		s.logger.Error(err, "Error updating status for neg, failed to get neg from store")
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return false
	}
	svcNeg := origSvcNeg.DeepCopy()

	ts := metav1.Now()
	if _, _, exists := s.findCondition(svcNeg.Status.Conditions, negv1beta1.Initialized); !exists {
		needInit = true
	}
	s.negMetrics.PublishNegSyncerStalenessMetrics(ts.Sub(svcNeg.Status.LastSyncTime.Time))

	s.ensureCondition(svcNeg, s.getSyncedCondition(syncErr))
	svcNeg.Status.LastSyncTime = ts

	if len(svcNeg.Status.NetworkEndpointGroups) == 0 {
		needInit = true
	}

	_, err = s.patchSvcNegStatus(origSvcNeg.Status, svcNeg.Status)
	if err != nil {
		s.logger.Error(err, "Error updating SvcNeg CR")
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
	}

	return needInit
}

func (s *SvcNegResourceManager) LocationsChanged() bool {
	existingSubnets, err := s.getExistingSubnets()
	if err != nil {
		return false
	}
	currSubnets := sets.New[string]()
	subnets := s.ListSubnets()
	for _, subnet := range subnets {
		currSubnets.Insert(subnet.Name)
	}

	existingZones, err := s.getExistingZones()
	if err != nil {
		return false
	}
	zones, err := s.zoneGetter.ListZones(negtypes.NodeFilterForEndpointCalculatorMode(s.negSyncerKey.EpCalculatorMode), s.logger)
	if err != nil {
		s.logger.Error(err, "unable to list zones")
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return false
	}
	currZones := sets.New[string](zones...)

	return !currSubnets.Equal(existingSubnets) || !currZones.Equal(existingZones)
}

func (s *SvcNegResourceManager) getExistingSubnets() (sets.Set[string], error) {
	existingSubnets := sets.New[string]()
	svcNegCR, err := s.getSvcNEGFromStore()
	if err != nil {
		s.logger.Error(err, "unable to retrieve SvcNeg from the store", "SvcNeg", klog.KRef(s.negSyncerKey.Namespace, s.negSyncerKey.NegName))
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return existingSubnets, err
	}

	for _, ref := range svcNegCR.Status.NetworkEndpointGroups {
		// If the subnet url is empty it means that the reference was created before
		// Subnets were populated by the controller. This is only possible with the subnetwork
		// that is specificed in networkInfo, and therefore we can assume which subnetwork was
		// used for this NEG
		subnetURL := s.networkInfo.SubnetworkURL
		if ref.SubnetURL != "" {
			subnetURL = ref.SubnetURL
		}
		id, err := cloud.ParseResourceURL(subnetURL)
		if err != nil {
			s.logger.Error(err, "unable to parse subnet url", "url", ref.SubnetURL)
			s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}

		existingSubnets.Insert(id.Key.Name)
	}
	return existingSubnets, nil
}

func (s *SvcNegResourceManager) getExistingZones() (sets.Set[string], error) {
	existingZones := sets.New[string]()
	svcNegCR, err := s.getSvcNEGFromStore()
	if err != nil {
		s.logger.Error(err, "unable to retrieve SvcNeg from the store", "SvcNeg", klog.KRef(s.negSyncerKey.Namespace, s.negSyncerKey.NegName))
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return existingZones, err
	}

	for _, ref := range svcNegCR.Status.NetworkEndpointGroups {
		id, err := cloud.ParseResourceURL(ref.SelfLink)
		if err != nil {
			s.logger.Error(err, "unable to parse selflink", "selfLink", ref.SelfLink)
			s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
			continue
		}
		existingZones.Insert(id.Key.Zone)
	}
	return existingZones, nil
}

func (s *SvcNegResourceManager) GetNegNameForSubnet(subnet string) (string, error) {
	if !s.networkInfo.IsDefault {
		return s.negSyncerKey.NegName, nil
	}

	defaultSubnet, err := utils.KeyName(s.networkInfo.SubnetworkURL)
	if err != nil {
		s.logger.Error(err, "Errored getting default subnet from NetworkInfo when retrieving existing endpoints")
		return "", err
	}

	if subnet == defaultSubnet {
		return s.negSyncerKey.NegName, nil
	}

	return s.getNonDefaultSubnetNEGName(subnet)
}

func (s *SvcNegResourceManager) getNonDefaultSubnetNEGName(subnet string) (string, error) {
	if s.customName {
		negName, err := s.namer.NonDefaultSubnetCustomNEG(s.negSyncerKey.NegName, subnet)
		if err != nil {
			return "", err
		}
		return negName, nil
	}

	return s.namer.NonDefaultSubnetNEG(s.negSyncerKey.Namespace, s.negSyncerKey.Name, subnet, s.negSyncerKey.PortTuple.Port), nil
}

func (s *SvcNegResourceManager) ListSubnets() []nodetopologyv1.SubnetConfig {
	if !s.networkInfo.IsDefault {
		subnet, err := utils.KeyName(s.networkInfo.SubnetworkURL)
		if err != nil {
			s.logger.Error(err, "Failed to get subnet from NetworkInfo", "subnetworkURL", s.networkInfo.SubnetworkURL)
			return nil
		}
		return []nodetopologyv1.SubnetConfig{{Name: subnet}}
	}
	return s.zoneGetter.ListSubnets(s.logger)
}
func (s *SvcNegResourceManager) ListZonesForSubnet(subnet string, filter zonegetter.Filter) ([]string, error) {
	return s.zoneGetter.ListZones(filter, s.logger)
}

func (s *SvcNegResourceManager) GenerateSubnetToNegNameMap(subnetConfigs []nodetopologyv1.SubnetConfig) (map[string]string, error) {
	defaultSubnet, err := utils.KeyName(s.networkInfo.SubnetworkURL)
	if err != nil {
		s.logger.Error(err, "Errored getting default subnet from NetworkInfo when generating subnet to NEG name map")
		return nil, err
	}

	subnetToNegMapping := make(map[string]string)
	// If networkInfo is not on the default subnet, then this service is using
	// multi-networking which cannot be used with multi subnet clusters. Even though
	// multi-networking subnet is using a non default subnet name, we use the default
	// neg naming which differs from how multi subnet cluster non default NEG names are
	// handled.
	if !s.networkInfo.IsDefault {
		subnetToNegMapping[defaultSubnet] = s.negSyncerKey.NegName
		return subnetToNegMapping, nil
	}

	for _, subnetConfig := range subnetConfigs {
		negName, err := s.GetNegNameForSubnet(subnetConfig.Name)
		if err != nil {
			s.logger.Error(err, "Errored when generating subnet to NEG name map")
			return nil, err
		}
		subnetToNegMapping[subnetConfig.Name] = negName
	}

	return subnetToNegMapping, nil
}

func (s *SvcNegResourceManager) EnsureNeg(subnet, zone string, networkInfo network.NetworkInfo) (*composite.NetworkEndpointGroup, error) {
	negName, err := s.GetNegNameForSubnet(subnet)
	if err != nil {
		return nil, err
	}

	subnetURL := networkInfo.SubnetworkURL
	if networkInfo.IsDefault {
		// For default network, we might have multiple subnets (MSC).
		// We use the provided subnet name to construct the URL, using networkInfo.SubnetworkURL as template.
		id, err := cloud.ParseResourceURL(networkInfo.SubnetworkURL)
		if err != nil {
			s.logger.Error(err, "Failed to parse subnetwork URL from networkInfo", "subnetworkURL", networkInfo.SubnetworkURL)
			return nil, err
		}
		id.Key.Name = subnet
		subnetURL = cloud.SelfLink(gcpmeta.VersionGA, id.ProjectID, id.Resource, id.Key)
	}

	// Update networkInfo to use the resolved subnet URL
	resolvedNetworkInfo := networkInfo
	resolvedNetworkInfo.SubnetworkURL = subnetURL

	return ensureNetworkEndpointGroup(
		s.negSyncerKey.Namespace,
		s.negSyncerKey.Name,
		negName,
		zone,
		s.negSyncerKey.String(),
		s.kubeSystemUID,
		fmt.Sprint(s.negSyncerKey.PortTuple.Port),
		s.negSyncerKey.NegType,
		s.cloud,
		s.serviceLister,
		s.recorder,
		s.version,
		s.customName,
		resolvedNetworkInfo,
		s.logger,
		s.negMetrics,
	)
}

func (s *SvcNegResourceManager) ListEndpoints(subnet, zone string, showHealthStatus bool, candidateZonesMap sets.Set[string]) ([]*composite.NetworkEndpointWithHealthStatus, error) {
	negName, err := s.GetNegNameForSubnet(subnet)
	if err != nil {
		return nil, err
	}
	networkEndpointsWithHealthStatus, err := s.cloud.ListNetworkEndpoints(negName, zone, showHealthStatus, s.version, s.logger)
	if err != nil {
		// It is possible for a NEG to be missing in a zone without candidate nodes. Log and ignore this error.
		// NEG not found in a candidate zone is an error.
		if utils.IsNotFoundError(err) && !candidateZonesMap.Has(zone) {
			s.logger.Info("Ignoring NotFound error for NEG", "negName", negName, "zone", zone)
			s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
			return nil, nil
		}
		return nil, err
	}
	return networkEndpointsWithHealthStatus, nil
}

func (s *SvcNegResourceManager) ComputeEPSStaleness(endpointSlices []*discovery.EndpointSlice) {
	negCR, err := s.getSvcNEGFromStore()
	if err != nil {
		s.logger.Error(err, "unable to retrieve SvcNeg from the store", "SvcNeg", klog.KRef(s.negSyncerKey.Namespace, s.negSyncerKey.NegName))
		s.negMetrics.PublishNegControllerErrorCountMetrics(err, true)
		return
	}
	lastSyncTimestamp := negCR.Status.LastSyncTime
	for _, endpointSlice := range endpointSlices {
		epsCreationTimestamp := endpointSlice.ObjectMeta.CreationTimestamp

		epsStaleness := time.Since(lastSyncTimestamp.Time)
		// if this endpoint slice is newly created/created after last sync
		if lastSyncTimestamp.Before(&epsCreationTimestamp) {
			epsStaleness = time.Since(epsCreationTimestamp.Time)
		}
		s.negMetrics.PublishNegEPSStalenessMetrics(epsStaleness)
		s.logger.V(3).Info("Endpoint slice syncs", "Namespace", endpointSlice.Namespace, "Name", endpointSlice.Name, "staleness", epsStaleness)
	}
}

// getSvcNEGFromStore returns the neg associated with the provided namespace and neg name if it exists otherwise throws an error
func (s *SvcNegResourceManager) getSvcNEGFromStore() (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	n, exists, err := s.svcNegLister.GetByKey(fmt.Sprintf("%s/%s", s.negSyncerKey.Namespace, s.negSyncerKey.NegName))
	if err != nil {
		return nil, fmt.Errorf("error getting SvcNeg %s/%s from cache: %w", s.negSyncerKey.Namespace, s.negSyncerKey.NegName, err)
	}
	if !exists {
		return nil, fmt.Errorf("SvcNeg %s/%s is not in store", s.negSyncerKey.Namespace, s.negSyncerKey.NegName)
	}

	return n.(*negv1beta1.ServiceNetworkEndpointGroup), nil
}

func (s *SvcNegResourceManager) negToNegRef(neg *composite.NetworkEndpointGroup) negv1beta1.NegObjectReference {
	negRef := negv1beta1.NegObjectReference{
		Id:                  fmt.Sprint(neg.Id),
		SelfLink:            neg.SelfLink,
		NetworkEndpointType: negv1beta1.NetworkEndpointType(neg.NetworkEndpointType),
	}
	if flags.F.EnableMultiSubnetClusterPhase1 {
		negRef.State = negv1beta1.ActiveState
		negRef.SubnetURL = neg.Subnetwork
	}
	return negRef
}

// patchSvcNegStatus patches the specified SvcNeg CR status with the provided new status
func (s *SvcNegResourceManager) patchSvcNegStatus(oldStatus, newStatus negv1beta1.ServiceNetworkEndpointGroupStatus) (*negv1beta1.ServiceNetworkEndpointGroup, error) {
	patchBytes, err := patch.MergePatchBytes(negv1beta1.ServiceNetworkEndpointGroup{Status: oldStatus}, negv1beta1.ServiceNetworkEndpointGroup{Status: newStatus})
	if err != nil {
		return nil, fmt.Errorf("failed to prepare patch bytes: %w", err)
	}

	start := time.Now()
	neg, err := s.svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(s.negSyncerKey.Namespace).Patch(context.Background(), s.negSyncerKey.NegName, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	s.negMetrics.PublishK8sRequestCountMetrics(start, metrics.DeleteRequest, err)
	return neg, err
}

// findCondition finds a condition in the given list of conditions that has the type conditionType and returns the condition and its index.
// If no condition is found, an empty condition, -1 and false will be returned to indicate the condition does not exist.
func (s *SvcNegResourceManager) findCondition(conditions []negv1beta1.Condition, conditionType string) (negv1beta1.Condition, int, bool) {
	for i, c := range conditions {
		if c.Type == conditionType {
			return c, i, true
		}
	}

	return negv1beta1.Condition{}, -1, false
}

// getInitializedCondition returns the expected initialized condition based on given error
func (s *SvcNegResourceManager) getInitializedCondition(err error) negv1beta1.Condition {
	if err != nil {
		return negv1beta1.Condition{
			Type:               negv1beta1.Initialized,
			Status:             apiv1.ConditionFalse,
			Reason:             negtypes.NegInitializationFailed,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
	}

	return negv1beta1.Condition{
		Type:               negv1beta1.Initialized,
		Status:             apiv1.ConditionTrue,
		Reason:             negtypes.NegInitializationSuccessful,
		LastTransitionTime: metav1.Now(),
	}
}

// ensureCondition will update the condition on the neg object if necessary
func (s *SvcNegResourceManager) ensureCondition(neg *negv1beta1.ServiceNetworkEndpointGroup, expectedCondition negv1beta1.Condition) negv1beta1.Condition {
	condition, index, exists := s.findCondition(neg.Status.Conditions, expectedCondition.Type)
	if !exists {
		neg.Status.Conditions = append(neg.Status.Conditions, expectedCondition)
		return expectedCondition
	}

	if condition.Status == expectedCondition.Status {
		expectedCondition.LastTransitionTime = condition.LastTransitionTime
	}

	neg.Status.Conditions[index] = expectedCondition
	return expectedCondition
}

// getNonActiveNegRefs creates NEG references for NEGs in Inactive State and ToBeDeleted state.
// Inactive NEG are NEGs that are in zones that cluster is no longer in.
// ToBeDeleted NEGs are NEGs in subnets that no longer exist on the Topology CRD
func (s *SvcNegResourceManager) getNonActiveNegRefs(oldNegRefs []negv1beta1.NegObjectReference, currentNegRefs []negv1beta1.NegObjectReference) []negv1beta1.NegObjectReference {
	subnetConfigs := s.ListSubnets()
	subnetMap := make(map[string]struct{})
	for _, subnet := range subnetConfigs {
		subnetMap[subnet.Name] = struct{}{}
	}

	activeNegs := make(map[negtypes.NegInfo]struct{})
	for _, negRef := range currentNegRefs {
		negInfo, err := negtypes.NegInfoFromNegRef(negRef)
		if err != nil {
			s.logger.Error(err, "Failed to extract name and zone information of a neg from the current snapshot", "negId", negRef.Id, "negSelfLink", negRef.SelfLink)
			continue
		}
		activeNegs[negInfo] = struct{}{}
	}

	var nonActiveNegRefs []negv1beta1.NegObjectReference
	for _, origNegRef := range oldNegRefs {
		negInfo, err := negtypes.NegInfoFromNegRef(origNegRef)
		if err != nil {
			s.logger.Error(err, "Failed to extract name and zone information of a neg from the previous snapshot, skipping validating if it is an Inactive NEG", "negId", origNegRef.Id, "negSelfLink", origNegRef.SelfLink)
			continue
		}

		if _, exists := activeNegs[negInfo]; exists {
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
			nonActiveNegRef.SubnetURL = s.networkInfo.SubnetworkURL
		}

		resID, err := cloud.ParseResourceURL(nonActiveNegRef.SubnetURL)
		if err != nil {
			s.logger.Error(err, "Failed to extract subnet information from the previous snapshot, skipping validating if it is an Inactive or to-be-deleted NEG", "negId", nonActiveNegRef.Id, "negSelfLink", nonActiveNegRef.SelfLink)
			continue
		}

		if _, exists := subnetMap[resID.Key.Name]; !exists {
			nonActiveNegRef.State = negv1beta1.ToBeDeletedState
		}

		nonActiveNegRefs = append(nonActiveNegRefs, *nonActiveNegRef)
	}
	return nonActiveNegRefs
}

// getSyncedCondition returns the expected synced condition based on given error
func (s *SvcNegResourceManager) getSyncedCondition(err error) negv1beta1.Condition {
	if err != nil {
		return negv1beta1.Condition{
			Type:               negv1beta1.Synced,
			Status:             apiv1.ConditionFalse,
			Reason:             negtypes.NegSyncFailed,
			LastTransitionTime: metav1.Now(),
			Message:            err.Error(),
		}
	}

	return negv1beta1.Condition{
		Type:               negv1beta1.Synced,
		Status:             apiv1.ConditionTrue,
		Reason:             negtypes.NegSyncSuccessful,
		LastTransitionTime: metav1.Now(),
	}
}
