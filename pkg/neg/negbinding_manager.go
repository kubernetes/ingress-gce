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

package neg

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	negbindingv1beta1 "k8s.io/ingress-gce/pkg/apis/negbinding/v1beta1"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	"k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/readiness"
	negsyncer "k8s.io/ingress-gce/pkg/neg/syncers"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	"k8s.io/ingress-gce/pkg/neg/syncers/negstatushandler"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	negbindingclient "k8s.io/ingress-gce/pkg/negbinding/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

const (
	// ServiceKeyIndex is the name of the index that maps service key (namespace/name) to NEGBinding.
	ServiceKeyIndex = "serviceKey"
)

var (
	ErrNEGBindingMultiNetworkNotSupported = errors.New("NEGBinding does not support multi-network")
	ErrServiceNotFound                    = errors.New("service not found in cache")
	ErrInvalidBackendRef                  = errors.New("BackendRef is nil")
	ErrInvalidBackendRefKind              = errors.New("unsupported BackendRef Kind")
)

// ServiceKeyIndexFunc maps NEGBinding to its backend service key if kind is Service.
func ServiceKeyIndexFunc(obj interface{}) ([]string, error) {
	binding, ok := obj.(*negbindingv1beta1.NetworkEndpointGroupBinding)
	if !ok {
		return []string{}, fmt.Errorf("unexpected object type %T", obj)
	}
	if binding.Spec.BackendRef != nil && binding.Spec.BackendRef.Kind == negbindingv1beta1.ServiceKind {
		return []string{fmt.Sprintf("%s/%s", binding.Namespace, binding.Spec.BackendRef.Name)}, nil
	}
	return []string{}, nil
}

type syncerConfig struct {
	portTuple   negtypes.SvcPortTuple
	networkInfo network.NetworkInfo
}

func (c syncerConfig) Equals(other syncerConfig) bool {
	return c.portTuple.Port == other.portTuple.Port &&
		c.portTuple.Name == other.portTuple.Name &&
		c.portTuple.TargetPort == other.portTuple.TargetPort &&
		c.networkInfo.IsDefault == other.networkInfo.IsDefault &&
		c.networkInfo.NetworkURL == other.networkInfo.NetworkURL &&
		c.networkInfo.SubnetworkURL == other.networkInfo.SubnetworkURL &&
		c.networkInfo.K8sNetwork == other.networkInfo.K8sNetwork
}

// negOwnershipRegistry allows to track which NEGBinding CR's syncer has rights to modify endpoints of the NEGs based on their name.
type negOwnershipRegistry struct {
	mu        sync.Mutex
	owners    map[string]string // negName -> ownerKey
	onRelease func(negName string)
}

// newNEGOwnershipRegistry constructs a new negOwnershipRegistry.
func newNEGOwnershipRegistry(onRelease func(string)) *negOwnershipRegistry {
	return &negOwnershipRegistry{
		owners:    make(map[string]string),
		onRelease: onRelease,
	}
}

// Acquire tries to get exclusive ownership of NEGs with name negName for owner
func (r *negOwnershipRegistry) Acquire(negName string, owner string) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	currentOwner, ok := r.owners[negName]
	if !ok {
		r.owners[negName] = owner
		return true, ""
	}
	if currentOwner == owner {
		return true, ""
	}
	return false, currentOwner
}

// ReleaseAllOwnedExcept releases all owned by owner NEG names, except ones in keep set
func (r *negOwnershipRegistry) ReleaseAllOwnedExcept(owner string, keep sets.Set[string]) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if keep == nil {
		keep = sets.New[string]()
	}

	for negName, currentOwner := range r.owners {
		if currentOwner == owner {
			if !keep.Has(negName) {
				delete(r.owners, negName)
				if r.onRelease != nil {
					go r.onRelease(negName)
				}
			}
		}
	}
}

// GetOwner gets current owner of the NEG name
func (r *negOwnershipRegistry) GetOwner(negName string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.owners[negName]
}

// negBindingManager manages the lifecycle of syncers associated with NEGBinding CRs.
type negBindingManager struct {
	negBindingClient negbindingclient.Interface
	negBindingLister cache.Indexer

	// Listers needed for syncer
	podLister           cache.Indexer
	serviceLister       cache.Indexer
	endpointSliceLister cache.Indexer
	nodeLister          cache.Indexer

	zoneGetter      *zonegetter.ZoneGetter
	networkResolver network.Resolver
	cloud           negtypes.NetworkEndpointGroupCloud
	recorder        record.EventRecorder
	namer           negtypes.NetworkEndpointGroupNamer

	// Syncers map keyed by binding namespace/name (bindingKey)
	mu            sync.Mutex
	syncerMap     map[string]negtypes.NegSyncer
	syncerConfigs map[string]syncerConfig

	// Metrics
	negMetrics    *metrics.NegMetrics
	syncerMetrics *metricscollector.SyncerMetrics

	reflector     readiness.Reflector
	kubeSystemUID types.UID

	ownershipRegistry *negOwnershipRegistry

	logger klog.Logger
}

// newNEGBindingManager constructs a new negBindingManager.
func newNEGBindingManager(
	negBindingClient negbindingclient.Interface,
	negBindingLister cache.Indexer,
	podLister cache.Indexer,
	serviceLister cache.Indexer,
	endpointSliceLister cache.Indexer,
	nodeLister cache.Indexer,
	zoneGetter *zonegetter.ZoneGetter,
	networkResolver network.Resolver,
	cloud negtypes.NetworkEndpointGroupCloud,
	recorder record.EventRecorder,
	namer negtypes.NetworkEndpointGroupNamer,
	negMetrics *metrics.NegMetrics,
	syncerMetrics *metricscollector.SyncerMetrics,
	reflector readiness.Reflector,
	kubeSystemUID types.UID,
	logger klog.Logger,
) *negBindingManager {
	m := &negBindingManager{
		negBindingClient:    negBindingClient,
		negBindingLister:    negBindingLister,
		podLister:           podLister,
		serviceLister:       serviceLister,
		endpointSliceLister: endpointSliceLister,
		nodeLister:          nodeLister,
		zoneGetter:          zoneGetter,
		networkResolver:     networkResolver,
		cloud:               cloud,
		recorder:            recorder,
		namer:               namer,
		syncerMap:           make(map[string]negtypes.NegSyncer),
		syncerConfigs:       make(map[string]syncerConfig),
		negMetrics:          negMetrics,
		syncerMetrics:       syncerMetrics,
		reflector:           reflector,
		kubeSystemUID:       kubeSystemUID,
		logger:              logger.WithName("NEGBindingManager"),
	}
	m.ownershipRegistry = newNEGOwnershipRegistry(func(negName string) {
		m.tryAssignNEGToBinding(negName)
	})
	return m
}

// EnsureSyncerForNEGBinding ensures corresponding syncer is started for the binding.
func (m *negBindingManager) EnsureSyncerForNEGBinding(binding *negbindingv1beta1.NetworkEndpointGroupBinding) error {
	err := m.validateBackendRef(binding)
	if err != nil {
		_ = m.updateBackendRefCondition(binding, err)
		return err
	}

	svcName := binding.Spec.BackendRef.Name
	svcKey := fmt.Sprintf("%s/%s", binding.Namespace, svcName)
	svc, err := m.getServiceFromCache(svcKey)
	if err != nil {
		_ = m.updateBackendRefCondition(binding, ErrServiceNotFound)
		return err
	}

	networkInfo, err := m.getAndVerifyNetworkInfo(svc)
	if err != nil {
		_ = m.updateBackendRefCondition(binding, err)
		return err
	}

	syncer, err := m.ensureSyncerForNEGBinding(binding, svc, networkInfo)
	if err != nil {
		return err
	}

	if syncer != nil {
		syncer.Sync()
	}
	return nil
}

// EnsureSyncersForService ensures syncers for all bindings referencing the given service.
func (m *negBindingManager) EnsureSyncersForService(svcNamespace, svcName string) error {
	svcKey := fmt.Sprintf("%s/%s", svcNamespace, svcName)
	svc, err := m.getServiceFromCache(svcKey)
	if err != nil {
		return err
	}

	objs, err := m.negBindingLister.ByIndex(ServiceKeyIndex, svcKey)
	if err != nil {
		return fmt.Errorf("failed to list bindings for service %s from index: %w", svcKey, err)
	}

	networkInfo, networkInfoErr := m.getAndVerifyNetworkInfo(svc)

	var errs []error
	for _, obj := range objs {
		binding, ok := obj.(*negbindingv1beta1.NetworkEndpointGroupBinding)
		if !ok {
			errs = append(errs, fmt.Errorf("unexpected object type %T in binding index for service %s", obj, svcKey))
			continue
		}

		err := m.validateBackendRef(binding)
		if err != nil {
			_ = m.updateBackendRefCondition(binding, err)
			return err
		}

		if networkInfoErr != nil {
			_ = m.updateBackendRefCondition(binding, networkInfoErr)
			continue
		}

		syncer, err := m.ensureSyncerForNEGBinding(binding, svc, networkInfo)
		if err != nil {
			errs = append(errs, err)
		}

		if syncer != nil {
			_ = syncer.Sync()
		}
	}

	if networkInfoErr != nil {
		errs = append(errs, networkInfoErr)
	}

	return utilerrors.NewAggregate(errs)
}

func (m *negBindingManager) ensureSyncerForNEGBinding(
	binding *negbindingv1beta1.NetworkEndpointGroupBinding,
	svc *apiv1.Service,
	networkInfo *network.NetworkInfo,
) (negtypes.NegSyncer, error) {
	svcName := binding.Spec.BackendRef.Name
	svcPort := binding.Spec.BackendRef.Port

	portTuple, err := m.getPortTuple(svc, svcPort)
	if err != nil {
		_ = m.updateBackendRefCondition(binding, err)
		return nil, err
	}

	if err := m.updateBackendRefCondition(binding, nil); err != nil {
		return nil, err
	}

	bindingKey := fmt.Sprintf("%s/%s", binding.Namespace, binding.Name)
	if len(binding.Spec.NetworkEndpointGroups) == 0 {
		m.logger.Info("NEGBinding has no NEGs defined", "binding", bindingKey)
		m.StopSyncer(binding.Namespace, binding.Name)
		return nil, nil
	}

	newConfig := syncerConfig{portTuple: portTuple, networkInfo: *networkInfo}

	m.mu.Lock()
	defer m.mu.Unlock()

	syncer, ok := m.syncerMap[bindingKey]
	if ok {
		oldConfig, hasConfig := m.syncerConfigs[bindingKey]
		if !hasConfig || !oldConfig.Equals(newConfig) {
			m.logger.Info("Configuration changed for NEGBinding syncer, recreating", "binding", bindingKey, "old", oldConfig, "new", newConfig)
			syncer.Stop()
			m.ownershipRegistry.ReleaseAllOwnedExcept(bindingKey, nil)
			delete(m.syncerMap, bindingKey)
			delete(m.syncerConfigs, bindingKey)
			// Proceed to create new syncer
		} else {
			if syncer.IsStopped() {
				if err := syncer.Start(); err != nil {
					return nil, fmt.Errorf("failed to start existing syncer for binding %s: %w", bindingKey, err)
				}
			}
			return syncer, nil
		}
	}

	defaultSubnetURL := networkInfo.SubnetworkURL

	syncerKey := negtypes.NegSyncerKey{
		Namespace:        binding.Namespace,
		Name:             svcName,
		NEGBindingName:   binding.Name,
		PortTuple:        portTuple,
		NegType:          negtypes.VmIpPortEndpointType,
		EpCalculatorMode: negtypes.L7Mode,
	}

	tp, err := negsyncer.NewNEGBindingTopologyProvider(binding.Namespace, binding.Name, m.negBindingLister, defaultSubnetURL, m.ownershipRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create topology provider: %w", err)
	}

	statusHandler := negstatushandler.NewNEGBindingStatusHandler(
		binding.Name,
		binding.Namespace,
		m.negBindingClient,
		m.negBindingLister,
		m.negMetrics,
		m.logger,
	)

	epc := negsyncer.GetEndpointsCalculator(
		m.podLister,
		m.nodeLister,
		m.serviceLister,
		m.zoneGetter,
		syncerKey,
		negtypes.L7Mode,
		m.logger.WithValues("service", klog.KRef(syncerKey.Namespace, syncerKey.Name), "negBindingName", syncerKey.NEGBindingName),
		false,
		m.syncerMetrics,
		networkInfo,
		"",
		m.negMetrics,
	)

	nbNamer := namer.NewNegBindingNamer(binding.Namespace, binding.Name, m.negBindingLister)

	syncer = negsyncer.NewTransactionSyncer(
		syncerKey,
		m.recorder,
		m.cloud,
		tp,
		m.podLister,
		m.serviceLister,
		m.endpointSliceLister,
		m.nodeLister,
		statusHandler,
		m.reflector,
		epc,
		string(m.kubeSystemUID),
		m.syncerMetrics,
		false,
		false,
		m.logger,
		labels.PodLabelPropagationConfig{},
		false,
		*networkInfo,
		nbNamer,
		m.negMetrics,
	)

	if err := syncer.Start(); err != nil {
		return nil, fmt.Errorf("failed to start syncer for binding %s: %w", bindingKey, err)
	}

	m.syncerMap[bindingKey] = syncer
	m.syncerConfigs[bindingKey] = newConfig
	return syncer, nil
}

// ProcessServiceDeletion handles service deletion by stopping syncers for referencing bindings.
func (m *negBindingManager) ProcessServiceDeletion(svcNamespace, svcName string) {
	svcKey := fmt.Sprintf("%s/%s", svcNamespace, svcName)
	objs, err := m.negBindingLister.ByIndex(ServiceKeyIndex, svcKey)
	if err != nil {
		m.logger.Error(err, "failed to list bindings for service from index during deletion processing", "service", svcKey)
		return
	}
	for _, obj := range objs {
		binding, ok := obj.(*negbindingv1beta1.NetworkEndpointGroupBinding)
		if !ok {
			m.logger.Error(nil, "Unexpected object type in negBindingLister during service deletion processing", "type", fmt.Sprintf("%T", obj))
			continue
		}
		bindingKey := fmt.Sprintf("%s/%s", binding.Namespace, binding.Name)
		m.logger.Info("Service deleted, stopping syncer for binding", "binding", bindingKey, "service", svcKey)
		m.StopSyncer(binding.Namespace, binding.Name)
		_ = m.updateBackendRefCondition(binding, fmt.Errorf("%w: %s/%s", ErrServiceNotFound, svcNamespace, svcName))
	}
}

// StopSyncer stops the syncer associated with the binding and removes it from the map.
func (m *negBindingManager) StopSyncer(namespace, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bindingKey := fmt.Sprintf("%s/%s", namespace, name)
	if syncer, ok := m.syncerMap[bindingKey]; ok {
		syncer.Stop()
		m.ownershipRegistry.ReleaseAllOwnedExcept(bindingKey, nil)
		delete(m.syncerMap, bindingKey)
		delete(m.syncerConfigs, bindingKey)
	}
}

// Sync triggers a sync for the syncer associated with the binding.
func (m *negBindingManager) Sync(namespace, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	bindingKey := fmt.Sprintf("%s/%s", namespace, name)
	if syncer, ok := m.syncerMap[bindingKey]; ok {
		if !syncer.IsStopped() {
			syncer.Sync()
		}
	}
}

// ShutDown stops all running syncers managed by this manager.
func (m *negBindingManager) ShutDown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, syncer := range m.syncerMap {
		syncer.Stop()
	}
	m.syncerMap = make(map[string]negtypes.NegSyncer)
	m.syncerConfigs = make(map[string]syncerConfig)
}

func (m *negBindingManager) getServiceFromCache(svcKey string) (*apiv1.Service, error) {
	svcObj, exists, err := m.serviceLister.GetByKey(svcKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get service %s from cache: %w", svcKey, err)
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrServiceNotFound, svcKey)
	}
	svc, ok := svcObj.(*apiv1.Service)
	if !ok {
		return nil, fmt.Errorf("unexpected object type %T in service cache for %s", svcObj, svcKey)
	}
	return svc, nil
}

func (m *negBindingManager) getAndVerifyNetworkInfo(svc *apiv1.Service) (*network.NetworkInfo, error) {
	networkInfo, err := m.networkResolver.ServiceNetwork(svc)
	if err != nil {
		return nil, fmt.Errorf("failed to get network info for service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	if !networkInfo.IsDefault {
		return nil, fmt.Errorf("%w, service: %s/%s", ErrNEGBindingMultiNetworkNotSupported, svc.Namespace, svc.Name)
	}
	return networkInfo, nil
}

// SyncAllSyncers triggers sync for all running syncers managed by this manager.
func (m *negBindingManager) SyncAllSyncers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, syncer := range m.syncerMap {
		if !syncer.IsStopped() {
			syncer.Sync()
		}
	}
}

func (m *negBindingManager) getPortTuple(svc *apiv1.Service, port int32) (negtypes.SvcPortTuple, error) {
	for _, sp := range svc.Spec.Ports {
		if sp.Port == port {
			portTuple := negtypes.SvcPortTuple{
				Port:       sp.Port,
				Name:       sp.Name,
				TargetPort: sp.TargetPort.String(),
			}
			return portTuple, nil
		}
	}
	return negtypes.SvcPortTuple{}, fmt.Errorf("port %d not found in service %s/%s spec", port, svc.Namespace, svc.Name)
}

func (m *negBindingManager) validateBackendRef(binding *negbindingv1beta1.NetworkEndpointGroupBinding) error {
	if binding.Spec.BackendRef == nil {
		return ErrInvalidBackendRef
	}
	if binding.Spec.BackendRef.Kind != negbindingv1beta1.ServiceKind {
		return fmt.Errorf("%w: unsupported Kind %q", ErrInvalidBackendRefKind, binding.Spec.BackendRef.Kind)
	}
	return nil
}

func (m *negBindingManager) updateBackendRefCondition(binding *negbindingv1beta1.NetworkEndpointGroupBinding, validationErr error) error {
	cond := negbindingv1beta1.Condition{
		Type:               "BackendRef",
		LastTransitionTime: metav1.Now(),
	}
	if validationErr == nil {
		cond.Status = metav1.ConditionTrue
		cond.Reason = "BackendRefValid"
	} else {
		cond.Status = metav1.ConditionFalse
		cond.Message = validationErr.Error()
		if errors.Is(validationErr, ErrInvalidBackendRefKind) {
			cond.Reason = "InvalidBackendRefKind"
		} else if errors.Is(validationErr, ErrServiceNotFound) {
			cond.Reason = "ServiceNotFound"
		} else if errors.Is(validationErr, ErrInvalidBackendRef) {
			cond.Reason = "InvalidBackendRef"
		} else {
			cond.Reason = "InvalidBackendRef" // Default
		}
	}

	origBinding := binding.DeepCopy()
	m.ensureCondition(binding, cond)

	patchBytes, err := patch.MergePatchBytes(negbindingv1beta1.NetworkEndpointGroupBinding{Status: origBinding.Status}, negbindingv1beta1.NetworkEndpointGroupBinding{Status: binding.Status})
	if err != nil {
		return fmt.Errorf("failed to prepare patch bytes for status update: %w", err)
	}

	if string(patchBytes) == "{}" {
		return nil
	}

	start := time.Now()
	_, err = m.negBindingClient.NetworkingV1beta1().NetworkEndpointGroupBindings(binding.Namespace).Patch(context.Background(), binding.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	m.negMetrics.PublishK8sRequestCountMetrics(start, metrics.PatchRequest, err)
	return err
}

func (m *negBindingManager) ensureCondition(binding *negbindingv1beta1.NetworkEndpointGroupBinding, expectedCondition negbindingv1beta1.Condition) {
	var index int
	var found bool
	for i, condition := range binding.Status.Conditions {
		if condition.Type == expectedCondition.Type {
			index = i
			found = true
			break
		}
	}

	if !found {
		binding.Status.Conditions = append(binding.Status.Conditions, expectedCondition)
		return
	}

	condition := binding.Status.Conditions[index]
	if condition.Status == expectedCondition.Status {
		expectedCondition.LastTransitionTime = condition.LastTransitionTime
	}

	binding.Status.Conditions[index] = expectedCondition
}

// tryAssignNEGToBinding is a callback for released NEGs. In case any other NEGBinding CR refers to the released NEG name, its syncer will be ensured and synced.
func (m *negBindingManager) tryAssignNEGToBinding(negName string) {
	objs := m.negBindingLister.List()
	for _, obj := range objs {
		binding, ok := obj.(*negbindingv1beta1.NetworkEndpointGroupBinding)
		if !ok {
			continue
		}

		for _, ref := range binding.Spec.NetworkEndpointGroups {
			if ref.Name == negName {
				bindingKey := fmt.Sprintf("%s/%s", binding.Namespace, binding.Name)
				// It's not guaranteed that this binding will have ownership if conflict with other binding still exists
				m.logger.Info("Triggering ensure/sync for binding which refers to released NEG", "binding", bindingKey, "negName", negName)
				if err := m.EnsureSyncerForNEGBinding(binding); err != nil {
					m.logger.Error(err, "Failed to ensure syncer for binding after NEG release", "binding", bindingKey, "negName", negName)
				}
				break
			}
		}
	}
}
