package manager

import (
	"fmt"
	"sync"

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
	controllers          map[string]*ControllerSet
	mu                   sync.Mutex
	logger               klog.Logger
	providerConfigClient providerconfigclient.Interface
	informersFactory     informers.SharedInformerFactory
	kubeClient           kubernetes.Interface
	svcNegClient         svcnegclient.Interface
	eventRecorderClient  kubernetes.Interface
	kubeSystemUID        types.UID
	clusterNamer         *namer.Namer
	l4Namer              *namer.L4Namer
	lpConfig             labels.PodLabelPropagationConfig
	defaultCloudConfig   string
	globalStopCh         <-chan struct{}
}

type ControllerSet struct {
	stopCh chan struct{}
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

func (cscm *ProviderConfigControllersManager) StartControllersForProviderConfig(cs *providerconfig.ProviderConfig) error {
	cscm.mu.Lock()
	defer cscm.mu.Unlock()

	csKey := providerConfigKey(cs)
	if _, exists := cscm.controllers[csKey]; exists {
		cscm.logger.Info("Controllers for provider config already exist, skipping start", "providerConfigId", csKey)
		return nil
	}

	err := finalizer.EnsureProviderConfigNEGCleanupFinalizer(cs, cscm.providerConfigClient, cscm.logger)
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

	cscm.logger.Info("Started controllers for provider config", "providerConfigId", csKey)
	return nil
}

func (cscm *ProviderConfigControllersManager) StopControllersForProviderConfig(cs *providerconfig.ProviderConfig) {
	cscm.mu.Lock()
	defer cscm.mu.Unlock()

	csKey := providerConfigKey(cs)
	_, exists := cscm.controllers[csKey]
	if !exists {
		cscm.logger.Info("Controllers for provider config do not exist", "providerConfigId", csKey)
		return
	}

	close(cscm.controllers[csKey].stopCh)

	delete(cscm.controllers, csKey)
	err := finalizer.DeleteProviderConfigNEGCleanupFinalizer(cs, cscm.providerConfigClient, cscm.logger)
	if err != nil {
		cscm.logger.Error(err, "failed to delete NEG cleanup finalizer for project", "providerConfigId", csKey)
	}
	cscm.logger.Info("Stopped controllers for provider config", "providerConfigId", csKey)
}
