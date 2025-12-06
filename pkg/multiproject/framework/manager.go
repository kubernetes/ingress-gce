package framework

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/common/finalizer"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	"k8s.io/klog/v2"
)

// manager coordinates lifecycle of controllers scoped to individual ProviderConfigs.
// It ensures per-ProviderConfig controller startup is idempotent, adds/removes
// finalizers, and wires stop channels for clean shutdown.
//
// This manager assumes it is invoked by a workqueue that guarantees
// the same ProviderConfig key is never processed concurrently.
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

// rollbackFinalizerOnStartFailure removes the finalizer after a start failure
// so that ProviderConfig deletion is not blocked.
func (m *manager) rollbackFinalizerOnStartFailure(pc *providerconfig.ProviderConfig, logger klog.Logger, cause error) {
	pcLatest, err := m.providerConfigClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		logger.Error(err, "failed to get latest ProviderConfig for finalizer rollback", "originalError", cause)
		return
	}
	if err := finalizer.DeleteProviderConfigFinalizer(pcLatest, m.finalizerName, m.providerConfigClient, logger); err != nil {
		logger.Error(err, "failed to clean up finalizer after start failure", "originalError", cause)
	}
}

// StartControllersForProviderConfig ensures finalizers are present and starts
// the controllers associated with the given ProviderConfig. The call is
// idempotent: repeated calls for the same ProviderConfig will only start
// controllers once.
func (m *manager) StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	logger := m.logger.WithValues("providerConfigName", pc.Name)

	if pc.DeletionTimestamp != nil {
		logger.V(3).Info("ProviderConfig is terminating; skipping start")
		return nil
	}

	pcKey := providerConfigKey(pc)

	cs, existed := m.controllers.GetOrCreate(pcKey)
	if existed && cs.stopCh != nil {
		logger.Info("Controllers for provider config already exist, skipping start")
		return nil
	}

	logger.Info("Starting controllers for provider config")

	hadFinalizer := finalizer.HasGivenFinalizer(pc.ObjectMeta, m.finalizerName)

	err := finalizer.EnsureProviderConfigFinalizer(pc, m.finalizerName, m.providerConfigClient, logger)
	if err != nil {
		if !existed {
			m.controllers.Delete(pcKey)
		}
		return fmt.Errorf("failed to ensure finalizer %s for provider config %s: %w", m.finalizerName, pcKey, err)
	}

	controllerStopCh, err := m.controllerStarter.StartController(pc)
	if err != nil {
		if !existed {
			m.controllers.Delete(pcKey)
		}
		if !hadFinalizer {
			m.rollbackFinalizerOnStartFailure(pc, logger, err)
		}
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
func (m *manager) StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) error {
	logger := m.logger.WithValues("providerConfigName", pc.Name)

	pcKey := providerConfigKey(pc)

	if cs, exists := m.controllers.Get(pcKey); exists {
		m.controllers.Delete(pcKey)
		if cs.stopCh != nil {
			close(cs.stopCh)
			logger.Info("Signaled controller stop")
		} else {
			logger.Info("Controllers for provider config already stopped")
		}
	} else {
		logger.Info("Controllers for provider config do not exist")
	}

	// Fetch the latest ProviderConfig to ensure we have current finalizer state.
	latestPC, err := m.providerConfigClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ProviderConfig not found while stopping controllers; skipping finalizer removal")
			return nil
		}
		return fmt.Errorf("failed to get latest ProviderConfig for finalizer removal: %w", err)
	}

	err = finalizer.DeleteProviderConfigFinalizer(latestPC, m.finalizerName, m.providerConfigClient, logger)
	if err != nil {
		return fmt.Errorf("failed to delete finalizer %s for provider config %s: %w", m.finalizerName, pcKey, err)
	}
	logger.Info("Stopped controllers for provider config")
	return nil
}
