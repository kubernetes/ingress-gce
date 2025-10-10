package manager

import (
	"fmt"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	informernetwork "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	informernodetopology "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/informers/externalversions"
	"k8s.io/apimachinery/pkg/types"
	informers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/finalizer"
	"k8s.io/ingress-gce/pkg/multiproject/gce"
	"k8s.io/ingress-gce/pkg/multiproject/neg"
	syncMetrics "k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

type ProviderConfigControllersManager struct {
	controllers *ControllerMap

	logger               klog.Logger
	providerConfigClient providerconfigclient.Interface
	informersFactory     informers.SharedInformerFactory
	svcNegFactory        informersvcneg.SharedInformerFactory
	networkFactory       informernetwork.SharedInformerFactory
	nodeTopologyFactory  informernodetopology.SharedInformerFactory
	kubeClient           kubernetes.Interface
	svcNegClient         svcnegclient.Interface
	eventRecorderClient  kubernetes.Interface
	networkClient        networkclient.Interface
	nodetopologyClient   nodetopologyclient.Interface
	kubeSystemUID        types.UID
	clusterNamer         *namer.Namer
	l4Namer              *namer.L4Namer
	lpConfig             labels.PodLabelPropagationConfig
	gceCreator           gce.GCECreator
	globalStopCh         <-chan struct{}
	syncerMetrics        *syncMetrics.SyncerMetrics
}

type ControllerSet struct {
	stopCh chan<- struct{}
}

func NewProviderConfigControllerManager(
	kubeClient kubernetes.Interface,
	informersFactory informers.SharedInformerFactory,
	svcNegFactory informersvcneg.SharedInformerFactory,
	networkFactory informernetwork.SharedInformerFactory,
	nodeTopologyFactory informernodetopology.SharedInformerFactory,
	providerConfigClient providerconfigclient.Interface,
	svcNegClient svcnegclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	gceCreator gce.GCECreator,
	globalStopCh <-chan struct{},
	logger klog.Logger,
	syncerMetrics *syncMetrics.SyncerMetrics,
) *ProviderConfigControllersManager {
	return &ProviderConfigControllersManager{
		controllers:          NewControllerMap(),
		logger:               logger,
		providerConfigClient: providerConfigClient,
		informersFactory:     informersFactory,
		svcNegFactory:        svcNegFactory,
		networkFactory:       networkFactory,
		nodeTopologyFactory:  nodeTopologyFactory,
		kubeClient:           kubeClient,
		svcNegClient:         svcNegClient,
		eventRecorderClient:  eventRecorderClient,
		kubeSystemUID:        kubeSystemUID,
		clusterNamer:         clusterNamer,
		l4Namer:              l4Namer,
		lpConfig:             lpConfig,
		gceCreator:           gceCreator,
		globalStopCh:         globalStopCh,
		syncerMetrics:        syncerMetrics,
	}
}

func providerConfigKey(pc *providerconfig.ProviderConfig) string {
	return pc.Name
}

func (pccm *ProviderConfigControllersManager) StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	logger := pccm.logger.WithValues("providerConfigId", pc.Name)

	pcKey := providerConfigKey(pc)

	logger.Info("Starting controllers for provider config")

	// Check if controller already exists
	if _, exists := pccm.controllers.Get(pcKey); exists {
		logger.Info("Controllers for provider config already exist, skipping start")
		return nil
	}

	// Perform expensive operations without holding the lock
	err := finalizer.EnsureProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
	if err != nil {
		return fmt.Errorf("failed to ensure NEG cleanup finalizer for project %s: %v", pcKey, err)
	}

	cloud, err := pccm.gceCreator.GCEForProviderConfig(pc, logger)
	if err != nil {
		return fmt.Errorf("failed to create GCE client for provider config %+v: %v", pc, err)
	}

	negControllerStopCh, err := neg.StartNEGController(
		pccm.informersFactory,
		pccm.svcNegFactory,
		pccm.networkFactory,
		pccm.nodeTopologyFactory,
		pccm.kubeClient,
		pccm.eventRecorderClient,
		pccm.svcNegClient,
		pccm.networkClient,
		pccm.nodetopologyClient,
		pccm.kubeSystemUID,
		pccm.clusterNamer,
		pccm.l4Namer,
		pccm.lpConfig,
		cloud,
		pccm.globalStopCh,
		logger,
		pc,
		pccm.syncerMetrics,
	)
	if err != nil {
		cleanupErr := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
		if cleanupErr != nil {
			logger.Error(cleanupErr, "failed to clean up NEG finalizer after controller start failure", "originalError", err)
		}
		return fmt.Errorf("failed to start NEG controller for project %s: %v", pcKey, err)
	}

	// Store the result
	pccm.controllers.Set(pcKey, &ControllerSet{
		stopCh: negControllerStopCh,
	})

	logger.Info("Started controllers for provider config")
	return nil
}

func (pccm *ProviderConfigControllersManager) StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) {
	logger := pccm.logger.WithValues("providerConfigId", pc.Name)

	csKey := providerConfigKey(pc)

	// Remove controller from map using minimal lock scope
	cs, exists := pccm.controllers.Delete(csKey)
	if !exists {
		logger.Info("Controllers for provider config do not exist")
		return
	}

	// Perform cleanup operations without holding the lock
	close(cs.stopCh)

	err := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
	if err != nil {
		logger.Error(err, "failed to delete NEG cleanup finalizer for project")
	}
	logger.Info("Stopped controllers for provider config")
}
