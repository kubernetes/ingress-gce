package manager

import (
	"context"
	"fmt"
	"sync"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// startNEGController is a package-level variable to allow tests to stub the
// actual NEG controller startup routine.
var startNEGController = neg.StartNEGController

// ProviderConfigControllersManager coordinates lifecycle of controllers scoped to
// a single ProviderConfig. It ensures per-ProviderConfig controller startup is
// idempotent, adds/removes the NEG cleanup finalizer, and wires a stop channel
// for clean shutdown.
type ProviderConfigControllersManager struct {
	mu          sync.Mutex
	controllers map[string]*controllerSet

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

// controllerSet holds controller-specific resources for a ProviderConfig.
// Unexported because it is an internal implementation detail.
type controllerSet struct {
	stopCh chan<- struct{}
}

// NewProviderConfigControllerManager constructs a new ProviderConfigControllersManager.
// It does not start any controllers until StartControllersForProviderConfig is invoked.
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
		controllers:          make(map[string]*controllerSet),
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

// StartControllersForProviderConfig ensures finalizers are present and starts
// the controllers associated with the given ProviderConfig. The call is
// idempotent per ProviderConfig: concurrent or repeated calls for the same
// ProviderConfig will only start controllers once.
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

	// Add the cleanup finalizer up front to avoid a window where controllers
	// may create external resources without a finalizer present. If deletion
	// happens in that window, cleanup could be skipped. We roll this back on
	// any subsequent startup error.
	err := finalizer.EnsureProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
	if err != nil {
		return fmt.Errorf("failed to ensure NEG cleanup finalizer for project %s: %w", pcKey, err)
	}

	cloud, err := pccm.gceCreator.GCEForProviderConfig(pc, logger)
	if err != nil {
		// If GCE client creation fails after finalizer was added, roll it back.
		pccm.rollbackFinalizerOnStartFailure(pc, logger, err)
		return fmt.Errorf("failed to create GCE client for provider config %+v: %w", pc, err)
	}

	negControllerStopCh, err := startNEGController(
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
		// If startup fails, make a best-effort to roll back the finalizer.
		pccm.rollbackFinalizerOnStartFailure(pc, logger, err)
		return fmt.Errorf("failed to start NEG controller for project %s: %w", pcKey, err)
	}

	pccm.controllers[pcKey] = &controllerSet{
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

	// Ensure we remove the finalizer from the latest object state.
	pcLatest := pccm.latestPCWithCleanupFinalizer(pc)
	err := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pcLatest, pccm.providerConfigClient, logger)
	if err != nil {
		logger.Error(err, "failed to delete NEG cleanup finalizer for project")
	}
	logger.Info("Stopped controllers for provider config")
}

// rollbackFinalizerOnStartFailure removes the NEG cleanup finalizer after a
// start failure so that ProviderConfig deletion is not blocked.
func (pccm *ProviderConfigControllersManager) rollbackFinalizerOnStartFailure(pc *providerconfig.ProviderConfig, logger klog.Logger, cause error) {
	pcLatest := pccm.latestPCWithCleanupFinalizer(pc)
	if err := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pcLatest, pccm.providerConfigClient, logger); err != nil {
		logger.Error(err, "failed to clean up NEG finalizer after start failure", "originalError", cause)
	}
}

// latestPCWithCleanupFinalizer returns the latest ProviderConfig from the API server.
// If the Get fails, it returns a local copy of the provided pc with the cleanup
// finalizer appended to ensure a subsequent delete attempt is not a no-op.
func (pccm *ProviderConfigControllersManager) latestPCWithCleanupFinalizer(pc *providerconfig.ProviderConfig) *providerconfig.ProviderConfig {
	pcLatest, err := pccm.providerConfigClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		pcCopy := pc.DeepCopy()
		pcCopy.Finalizers = append(pcCopy.Finalizers, finalizer.ProviderConfigNEGCleanupFinalizer)
		return pcCopy
	}
	return pcLatest
}
