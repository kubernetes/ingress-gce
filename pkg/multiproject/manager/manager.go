package manager

import (
	"context"
	"fmt"

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
	syncMetrics "k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

// startNEGController is a package-level variable to allow tests to stub the
// actual NEG controller startup routine.
var startNEGController = neg.StartNEGController

// ControllerSet holds controller-specific resources for a ProviderConfig.
type ControllerSet struct {
	stopCh chan<- struct{}
}

// ProviderConfigControllersManager coordinates lifecycle of controllers scoped to
// a single ProviderConfig. It ensures per-ProviderConfig controller startup is
// idempotent, adds/removes the NEG cleanup finalizer, and wires a stop channel
// for clean shutdown.
type ProviderConfigControllersManager struct {
	controllers *ControllerMap

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
	syncerMetrics       *syncMetrics.SyncerMetrics
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
	syncerMetrics *syncMetrics.SyncerMetrics,
) *ProviderConfigControllersManager {
	return &ProviderConfigControllersManager{
		controllers:          NewControllerMap(),
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
		syncerMetrics:        syncerMetrics,
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
	logger := pccm.logger.WithValues("providerConfigId", pc.Name)

	pcKey := providerConfigKey(pc)

	logger.Info("Starting controllers for provider config")

	// Check if controller already exists
	if _, exists := pccm.controllers.Get(pcKey); exists {
		logger.Info("Controllers for provider config already exist, skipping start")
		return nil
	}

	cloud, err := pccm.gceCreator.GCEForProviderConfig(pc, logger)
	if err != nil {
		return fmt.Errorf("failed to create GCE client for provider config %+v: %w", pc, err)
	}

	// Track if finalizer already exists so we only roll it back if we added it.
	// If the PC already had a finalizer (e.g., from a previous controller run),
	// we must not remove it on failure since NEGs from that run may still exist.
	hadFinalizer := finalizer.HasGivenFinalizer(pc.ObjectMeta, finalizer.ProviderConfigNEGCleanupFinalizer)

	// Add the cleanup finalizer right before starting the controller to minimize
	// the window where external resources could be created without a finalizer.
	// We roll this back on subsequent startup error only if we added it.
	err = finalizer.EnsureProviderConfigNEGCleanupFinalizer(pc, pccm.providerConfigClient, logger)
	if err != nil {
		return fmt.Errorf("failed to ensure NEG cleanup finalizer for project %s: %w", pcKey, err)
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
		pccm.syncerMetrics,
	)
	if err != nil {
		// If startup fails, make a best-effort to roll back the finalizer.
		if !hadFinalizer {
			pccm.rollbackFinalizerOnStartFailure(pc, logger, err)
		}
		return fmt.Errorf("failed to start NEG controller for project %s: %w", pcKey, err)
	}

	// Store the result
	pccm.controllers.Set(pcKey, &ControllerSet{
		stopCh: negControllerStopCh,
	})

	logger.Info("Started controllers for provider config")
	return nil
}

// StopControllersForProviderConfig stops controllers for the given ProviderConfig and removes the cleanup finalizer.
// Returns an error to allow callers to requeue when cleanup fails.
func (pccm *ProviderConfigControllersManager) StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	logger := pccm.logger.WithValues("providerConfigId", pc.Name)

	csKey := providerConfigKey(pc)

	// Remove controller from map using minimal lock scope
	cs, exists := pccm.controllers.Delete(csKey)
	if !exists {
		logger.Info("Controllers for provider config do not exist")
		return nil
	}

	// Perform cleanup operations without holding the lock
	close(cs.stopCh)

	// Fetch the latest ProviderConfig to ensure we have current finalizer state.
	pcLatest, err := pccm.providerConfigClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get latest ProviderConfig for finalizer removal: %w", err)
	}
	err = finalizer.DeleteProviderConfigNEGCleanupFinalizer(pcLatest, pccm.providerConfigClient, logger)
	if err != nil {
		return fmt.Errorf("failed to delete NEG cleanup finalizer for project: %w", err)
	}
	logger.Info("Stopped controllers for provider config")
	return nil
}

// rollbackFinalizerOnStartFailure removes the NEG cleanup finalizer after a
// start failure so that ProviderConfig deletion is not blocked.
func (pccm *ProviderConfigControllersManager) rollbackFinalizerOnStartFailure(pc *providerconfig.ProviderConfig, logger klog.Logger, cause error) {
	pcLatest, err := pccm.providerConfigClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		logger.Error(err, "failed to get latest ProviderConfig for finalizer rollback", "originalError", cause)
		return
	}
	if err := finalizer.DeleteProviderConfigNEGCleanupFinalizer(pcLatest, pccm.providerConfigClient, logger); err != nil {
		logger.Error(err, "failed to clean up NEG finalizer after start failure", "originalError", cause)
	}
}
