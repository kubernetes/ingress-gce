package manager

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	informers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clusterslice "k8s.io/ingress-gce/pkg/apis/clusterslice/v1"
	clustersliceclient "k8s.io/ingress-gce/pkg/clusterslice/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/multiproject/finalizer"
	"k8s.io/ingress-gce/pkg/multiproject/neg"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

type ClusterSliceControllersManager struct {
	controllers         map[string]*ControllerSet
	mu                  sync.Mutex
	logger              klog.Logger
	clusterSliceClient  clustersliceclient.Interface
	informersFactory    informers.SharedInformerFactory
	kubeClient          kubernetes.Interface
	svcNegClient        svcnegclient.Interface
	eventRecorderClient kubernetes.Interface
	kubeSystemUID       types.UID
	clusterNamer        *namer.Namer
	l4Namer             *namer.L4Namer
	lpConfig            labels.PodLabelPropagationConfig
	defaultCloudConfig  string
	globalStopCh        <-chan struct{}
}

type ControllerSet struct {
	stopCh chan struct{}
}

func NewClusterSliceControllerManager(
	kubeClient kubernetes.Interface,
	informersFactory informers.SharedInformerFactory,
	clusterSliceClient clustersliceclient.Interface,
	svcNegClient svcnegclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	defaultCloudConfig string,
	globalStopCh <-chan struct{},
	logger klog.Logger,
) *ClusterSliceControllersManager {
	return &ClusterSliceControllersManager{
		controllers:         make(map[string]*ControllerSet),
		logger:              logger,
		clusterSliceClient:  clusterSliceClient,
		informersFactory:    informersFactory,
		kubeClient:          kubeClient,
		svcNegClient:        svcNegClient,
		eventRecorderClient: eventRecorderClient,
		kubeSystemUID:       kubeSystemUID,
		clusterNamer:        clusterNamer,
		l4Namer:             l4Namer,
		lpConfig:            lpConfig,
		defaultCloudConfig:  defaultCloudConfig,
		globalStopCh:        globalStopCh,
	}
}

func clusterSliceKey(cs *clusterslice.ClusterSlice) string {
	return cs.Namespace + "/" + cs.Name
}

func (cscm *ClusterSliceControllersManager) StartControllersForClusterSlice(cs *clusterslice.ClusterSlice) error {
	cscm.mu.Lock()
	defer cscm.mu.Unlock()

	csKey := clusterSliceKey(cs)
	if _, exists := cscm.controllers[csKey]; exists {
		cscm.logger.Info("Controllers for cluster slice already exist, skipping start", "clusterSliceId", csKey)
		return nil
	}

	err := finalizer.EnsureClusterSliceNEGCleanupFinalizer(cs, cscm.clusterSliceClient, cscm.logger)
	if err != nil {
		return fmt.Errorf("failed to ensure NEG cleanup finalizer for project %s: %v", csKey, err)
	}
	negControllerStopCh, err := neg.StartNEGController(cscm.informersFactory, cscm.kubeClient, cscm.svcNegClient, cscm.eventRecorderClient, cscm.kubeSystemUID, cscm.clusterNamer, cscm.l4Namer, cscm.lpConfig, cscm.defaultCloudConfig, cscm.globalStopCh, cscm.logger, cs)
	if err != nil {
		return fmt.Errorf("failed to start NEG controller for project %s: %v", csKey, err)
	}
	cscm.controllers[csKey] = &ControllerSet{
		stopCh: negControllerStopCh,
	}

	cscm.logger.Info("Started controllers for cluster slice", "clusterSliceId", csKey)
	return nil
}

func (cscm *ClusterSliceControllersManager) StopControllersForClusterSlice(cs *clusterslice.ClusterSlice) {
	cscm.mu.Lock()
	defer cscm.mu.Unlock()

	csKey := clusterSliceKey(cs)
	_, exists := cscm.controllers[csKey]
	if !exists {
		cscm.logger.Info("Controllers for cluster slice do not exist", "clusterSliceId", csKey)
		return
	}

	close(cscm.controllers[csKey].stopCh)

	delete(cscm.controllers, csKey)
	err := finalizer.DeleteClusterSliceNEGCleanupFinalizer(cs, cscm.clusterSliceClient, cscm.logger)
	if err != nil {
		cscm.logger.Error(err, "failed to delete NEG cleanup finalizer for project", "clusterSliceId", csKey)
	}
	cscm.logger.Info("Stopped controllers for cluster slice", "clusterSliceId", csKey)
}
