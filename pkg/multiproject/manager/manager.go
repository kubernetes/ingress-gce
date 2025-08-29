package manager

import (
	"fmt"
	"sync"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/finalizer"
	"k8s.io/ingress-gce/pkg/multiproject/gce"
	multiprojectinformers "k8s.io/ingress-gce/pkg/multiproject/informerset"
	"k8s.io/ingress-gce/pkg/multiproject/neg"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

type ProviderConfigControllersManager struct {
	mu          sync.Mutex
	controllers map[string]*ControllerSet

	logger               klog.Logger
	providerConfigClient providerconfigclient.Interface
	// Base informers shared across all ProviderConfigs
	informers           *multiprojectinformers.InformerSet
	kubeClient          kubernetes.Interface
	svcNegClient        svcnegclient.Interface
	eventRecorderClient kubernetes.Interface
	networkClient       networkclient.Interface
	nodetopologyClient  nodetopologyclient.Interface
	kubeSystemUID       types.UID
	clusterNamer        *namer.Namer
	l4Namer             *namer.L4Namer
	lpConfig            labels.PodLabelPropagationConfig
	gceCreator          gce.GCECreator
	globalStopCh        <-chan struct{}
}

type ControllerSet struct {
	stopCh chan<- struct{}
}

func NewProviderConfigControllerManager(
	kubeClient kubernetes.Interface,
	informers *multiprojectinformers.InformerSet,
	providerConfigClient providerconfigclient.Interface,
	svcNegClient svcnegclient.Interface,
	networkClient networkclient.Interface,
	nodetopologyClient nodetopologyclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	gceCreator gce.GCECreator,
	globalStopCh <-chan struct{},
	logger klog.Logger,
) *ProviderConfigControllersManager {
	return &ProviderConfigControllersManager{
		controllers:          make(map[string]*ControllerSet),
		logger:               logger,
		providerConfigClient: providerConfigClient,
		informers:            informers,
		kubeClient:           kubeClient,
		svcNegClient:         svcNegClient,
		eventRecorderClient:  eventRecorderClient,
		networkClient:        networkClient,
		nodetopologyClient:   nodetopologyClient,
		kubeSystemUID:        kubeSystemUID,
		clusterNamer:         clusterNamer,
		l4Namer:              l4Namer,
		lpConfig:             lpConfig,
		gceCreator:           gceCreator,
		globalStopCh:         globalStopCh,
	}
}

func providerConfigKey(pc *providerconfig.ProviderConfig) string {
	return pc.Name
}

func (pccm *ProviderConfigControllersManager) StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	pccm.mu.Lock()
	defer pccm.mu.Unlock()

	logger := pccm.logger.WithValues("providerConfigId", pc.Name)

	pcKey := providerConfigKey(pc)

	logger.Info("Starting controllers for provider config")
	if _, exists := pccm.controllers[pcKey]; exists {
		logger.Info("Controllers for provider config already exist, skipping start")
		return nil
	}

	err := finalizer.EnsureProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
	if err != nil {
		return fmt.Errorf("failed to ensure NEG cleanup finalizer for project %s: %v", pcKey, err)
	}

	cloud, err := pccm.gceCreator.GCEForProviderConfig(pc, logger)
	if err != nil {
		return fmt.Errorf("failed to create GCE client for provider config %+v: %v", pc, err)
	}

	negControllerStopCh, err := neg.StartNEGController(
		pccm.informers,
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
	)
	if err != nil {
		cleanupErr := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
		if cleanupErr != nil {
			logger.Error(cleanupErr, "failed to clean up NEG finalizer after controller start failure", "originalError", err)
		}
		return fmt.Errorf("failed to start NEG controller for project %s: %v", pcKey, err)
	}

	pccm.controllers[pcKey] = &ControllerSet{
		stopCh: negControllerStopCh,
	}

	logger.Info("Started controllers for provider config")
	return nil
}

func (pccm *ProviderConfigControllersManager) StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) {
	pccm.mu.Lock()
	defer pccm.mu.Unlock()

	logger := pccm.logger.WithValues("providerConfigId", pc.Name)

	csKey := providerConfigKey(pc)
	_, exists := pccm.controllers[csKey]
	if !exists {
		logger.Info("Controllers for provider config do not exist")
		return
	}

	close(pccm.controllers[csKey].stopCh)

	delete(pccm.controllers, csKey)
	err := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
	if err != nil {
		logger.Error(err, "failed to delete NEG cleanup finalizer for project")
	}
	logger.Info("Stopped controllers for provider config")
}
