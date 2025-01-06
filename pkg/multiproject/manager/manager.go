package manager

import (
	"fmt"
	"sync"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	"k8s.io/apimachinery/pkg/types"
	informers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/finalizer"
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
	informersFactory     informers.SharedInformerFactory
	kubeClient           kubernetes.Interface
	svcNegClient         svcnegclient.Interface
	eventRecorderClient  kubernetes.Interface
	networkClient        networkclient.Interface
	nodetopologyClient   nodetopologyclient.Interface
	kubeSystemUID        types.UID
	clusterNamer         *namer.Namer
	l4Namer              *namer.L4Namer
	lpConfig             labels.PodLabelPropagationConfig
	defaultCloudConfig   string
	globalStopCh         <-chan struct{}
}

type ControllerSet struct {
	stopCh chan<- struct{}
}

func NewProviderConfigControllerManager(
	kubeClient kubernetes.Interface,
	informersFactory informers.SharedInformerFactory,
	providerConfigClient providerconfigclient.Interface,
	svcNegClient svcnegclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	defaultCloudConfig string,
	globalStopCh <-chan struct{},
	logger klog.Logger,
) *ProviderConfigControllersManager {
	return &ProviderConfigControllersManager{
		controllers:          make(map[string]*ControllerSet),
		logger:               logger,
		providerConfigClient: providerConfigClient,
		informersFactory:     informersFactory,
		kubeClient:           kubeClient,
		svcNegClient:         svcNegClient,
		eventRecorderClient:  eventRecorderClient,
		kubeSystemUID:        kubeSystemUID,
		clusterNamer:         clusterNamer,
		l4Namer:              l4Namer,
		lpConfig:             lpConfig,
		defaultCloudConfig:   defaultCloudConfig,
		globalStopCh:         globalStopCh,
	}
}

func providerConfigKey(cs *providerconfig.ProviderConfig) string {
	return cs.Namespace + "/" + cs.Name
}

func (pccm *ProviderConfigControllersManager) StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	pccm.mu.Lock()
	defer pccm.mu.Unlock()

	pcKey := providerConfigKey(pc)
	if _, exists := pccm.controllers[pcKey]; exists {
		pccm.logger.Info("Controllers for provider config already exist, skipping start", "providerConfigId", pcKey)
		return nil
	}

	err := finalizer.EnsureProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, pccm.logger)
	if err != nil {
		return fmt.Errorf("failed to ensure NEG cleanup finalizer for project %s: %v", pcKey, err)
	}

	negControllerStopCh, err := neg.StartNEGController(
		pccm.informersFactory,
		pccm.kubeClient,
		pccm.svcNegClient,
		pccm.eventRecorderClient,
		pccm.networkClient,
		pccm.nodetopologyClient,
		pccm.kubeSystemUID,
		pccm.clusterNamer,
		pccm.l4Namer,
		pccm.lpConfig,
		pccm.defaultCloudConfig,
		pccm.globalStopCh,
		pccm.logger,
		pc,
	)
	if err != nil {
		cleanupErr := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, pccm.logger)
		if cleanupErr != nil {
			pccm.logger.Error(cleanupErr, "failed to clean up NEG finalizer after controller start failure",
				"providerConfigId", pcKey, "originalError", err)
		}
		return fmt.Errorf("failed to start NEG controller for project %s: %v", pcKey, err)
	}

	pccm.controllers[pcKey] = &ControllerSet{
		stopCh: negControllerStopCh,
	}

	pccm.logger.Info("Started controllers for provider config", "providerConfigId", pcKey)
	return nil
}

func (pccm *ProviderConfigControllersManager) StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) {
	pccm.mu.Lock()
	defer pccm.mu.Unlock()

	csKey := providerConfigKey(pc)
	_, exists := pccm.controllers[csKey]
	if !exists {
		pccm.logger.Info("Controllers for provider config do not exist", "providerConfigId", csKey)
		return
	}

	close(pccm.controllers[csKey].stopCh)

	delete(pccm.controllers, csKey)
	err := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, pccm.logger)
	if err != nil {
		pccm.logger.Error(err, "failed to delete NEG cleanup finalizer for project", "providerConfigId", csKey)
	}
	pccm.logger.Info("Stopped controllers for provider config", "providerConfigId", csKey)
}
