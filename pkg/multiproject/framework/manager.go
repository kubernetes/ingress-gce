package framework

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/common/finalizer"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	"k8s.io/klog/v2"
)

// manager coordinates lifecycle of controllers scoped to individual ProviderConfigs.
// It ensures per-ProviderConfig controller startup is idempotent, adds/removes
// finalizers, and wires stop channels for clean shutdown.
type manager struct {
	controllers *ControllerMap

	logger               klog.Logger
	providerConfigClient providerconfigclient.Interface
	finalizerName        string
	controllerStarter    ControllerStarter
}

// newManager constructs a new generic ProviderConfig controller manager.
// It does not start any controllers until StartControllersForProviderConfig is invoked.
func newManager(
	providerConfigClient providerconfigclient.Interface,
	finalizerName string,
	controllerStarter ControllerStarter,
	logger klog.Logger,
) *manager {
	return &manager{
		controllers:          NewControllerMap(),
		logger:               logger,
		providerConfigClient: providerConfigClient,
		finalizerName:        finalizerName,
		controllerStarter:    controllerStarter,
	}
}

// providerConfigKey returns the key for a ProviderConfig in the controller map.
func providerConfigKey(pc *providerconfig.ProviderConfig) string {
	return pc.Name
}

// latestPCWithCleanupFinalizer returns the latest ProviderConfig from the API server.
// If the Get fails, it returns a local copy of the provided pc with the cleanup
// finalizer appended to ensure a subsequent delete attempt is not a no-op.
func (m *manager) latestPCWithCleanupFinalizer(pc *providerconfig.ProviderConfig) *providerconfig.ProviderConfig {
	pcLatest, err := m.providerConfigClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		pcCopy := pc.DeepCopy()
		if !finalizer.HasGivenFinalizer(pcCopy.ObjectMeta, m.finalizerName) {
			pcCopy.Finalizers = append(pcCopy.Finalizers, m.finalizerName)
		}
		return pcCopy
	}
	return pcLatest
}

// rollbackFinalizerOnStartFailure removes the finalizer after a start failure
// so that ProviderConfig deletion is not blocked.
func (m *manager) rollbackFinalizerOnStartFailure(pc *providerconfig.ProviderConfig, logger klog.Logger, cause error) {
	pcLatest := m.latestPCWithCleanupFinalizer(pc)
	if err := finalizer.DeleteProviderConfigFinalizer(pcLatest, m.finalizerName, m.providerConfigClient, logger); err != nil {
		logger.Error(err, "failed to clean up finalizer after start failure", "finalizer", m.finalizerName, "originalError", cause)
	}
}

// StartControllersForProviderConfig ensures finalizers are present and starts
// the controllers associated with the given ProviderConfig. The call is
// idempotent per ProviderConfig: concurrent or repeated calls for the same
// ProviderConfig will only start controllers once.
func (m *manager) StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	logger := m.logger.WithValues("providerConfigName", pc.Name)

	// Don't start controllers for ProviderConfigs that are being deleted.
	if pc.DeletionTimestamp != nil {
		logger.V(3).Info("ProviderConfig is terminating; skipping start")
		return nil
	}

	pcKey := providerConfigKey(pc)

	logger.Info("Starting controllers for provider config")

	cs, existed := m.controllers.GetOrCreate(pcKey)

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Re-validate: ensure the map still points to this cs before we proceed.
	if cur, ok := m.controllers.Get(pcKey); !ok || cur != cs {
		logger.V(3).Info("ControllerSet is no longer current; aborting start")
		return nil
	}

	// Idempotent start: already running.
	if cs.stopCh != nil {
		logger.Info("Controllers for provider config already exist, skipping start")
		return nil
	}

	// Ensure finalizer before starting.
	err := finalizer.EnsureProviderConfigFinalizer(pc, m.finalizerName, m.providerConfigClient, logger)
	if err != nil {
		// If we created this cs entry, remove it while still holding cs.mu,
		// but only if the map still points to this cs.
		if !existed {
			_ = m.controllers.DeleteIfSame(pcKey, cs)
		}
		// No more access to cs after this point from other goroutines due to revalidation.
		return fmt.Errorf("failed to ensure finalizer %s for provider config %s: %w", m.finalizerName, pcKey, err)
	}

	controllerStopCh, err := m.controllerStarter.StartController(pc)
	if err != nil {
		if !existed {
			_ = m.controllers.DeleteIfSame(pcKey, cs)
		}
		// If startup fails, make a best-effort to roll back the finalizer.
		m.rollbackFinalizerOnStartFailure(pc, logger, err)
		return fmt.Errorf("failed to start controller for provider config %s: %w", pcKey, err)
	}

	cs.stopCh = controllerStopCh

	logger.Info("Started controllers for provider config")
	return nil
}

// StopControllersForProviderConfig stops the controllers for the given ProviderConfig
// and removes the associated finalizer. Finalizer removal is attempted even if no
// controller mapping exists, ensuring deletion can proceed after process restarts
// or when controllers were previously stopped.
func (m *manager) StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) {
	logger := m.logger.WithValues("providerConfigName", pc.Name)

	csKey := providerConfigKey(pc)

	// Try to stop any running controller if we still track one.
	if cs, exists := m.controllers.Get(csKey); exists {
		cs.mu.Lock()
		stopCh := cs.stopCh

		// Already stopped; clean up the mapping if it still points to this cs.
		if stopCh == nil {
			_ = m.controllers.DeleteIfSame(csKey, cs)
			cs.mu.Unlock()
			logger.Info("Controllers for provider config already stopped")
		} else {
			// Transition to 'stopped' state under lock and remove mapping.
			cs.stopCh = nil
			_ = m.controllers.DeleteIfSame(csKey, cs)
			cs.mu.Unlock()
			// Now it is safe to close the stop channel.
			close(stopCh)
			logger.Info("Signaled controller stop")
		}
	} else {
		logger.Info("Controllers for provider config do not exist")
	}

	// Ensure we remove the finalizer from the latest object state.
	// This happens regardless of whether a controller was running, ensuring
	// deletion can proceed even if the controller mapping is gone.
	pcLatest := m.latestPCWithCleanupFinalizer(pc)
	err := finalizer.DeleteProviderConfigFinalizer(pcLatest, m.finalizerName, m.providerConfigClient, logger)
	if err != nil {
		logger.Error(err, "failed to delete finalizer for provider config", "finalizer", m.finalizerName)
	}
	logger.Info("Stopped controllers for provider config")
}
