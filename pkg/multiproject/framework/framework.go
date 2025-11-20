package framework

import (
	"k8s.io/client-go/tools/cache"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	"k8s.io/klog/v2"
)

// ControllerStarter defines the interface for starting a controller for a ProviderConfig.
// Implementations encapsulate all controller-specific startup logic and dependencies.
type ControllerStarter interface {
	// StartController starts controller(s) for the given ProviderConfig.
	// Returns:
	//   - A channel that should be closed to stop the controller
	//   - An error if startup fails
	//
	// The returned stop channel will be closed by the framework when the
	// ProviderConfig is deleted or the controller needs to shut down.
	StartController(pc *providerconfig.ProviderConfig) (chan<- struct{}, error)
}

// Framework provides a single entry point for managing ProviderConfig-scoped controllers.
// It orchestrates the lifecycle of controllers, ensuring proper startup, shutdown, and
// finalizer management for each ProviderConfig resource.
type Framework struct {
	controller *providerConfigController
}

// New creates a new Framework instance that manages controllers for ProviderConfig resources.
// It requires:
//   - providerConfigClient: client for managing ProviderConfig resources
//   - providerConfigInformer: informer for watching ProviderConfig changes
//   - finalizerName: name of the finalizer to add/remove on ProviderConfigs
//   - controllerStarter: implementation that knows how to start controller for a ProviderConfig
//   - stopCh: channel to signal shutdown
//   - logger: logger for framework operations
//
// The framework will automatically:
//   - Add/remove finalizers on ProviderConfig resources
//   - Start controllers idempotently (only once per ProviderConfig)
//   - Stop controllers when ProviderConfig is deleted
//   - Handle errors and rollback finalizers if startup fails
func New(
	providerConfigClient providerconfigclient.Interface,
	providerConfigInformer cache.SharedIndexInformer,
	finalizerName string,
	controllerStarter ControllerStarter,
	stopCh <-chan struct{},
	logger klog.Logger,
) *Framework {
	mgr := newManager(
		providerConfigClient,
		finalizerName,
		controllerStarter,
		logger,
	)

	ctrl := newProviderConfigController(mgr, providerConfigInformer, stopCh, logger)

	return &Framework{
		controller: ctrl,
	}
}

// Run starts the framework and blocks until the stop channel is closed.
// It will:
//   - Wait for the ProviderConfig informer to sync
//   - Start processing ProviderConfig resources
//   - Handle Add/Update/Delete events for ProviderConfigs
//   - Block until stopCh is closed
//
// The stop channel (provided to New) is used for graceful shutdown coordination.
func (f *Framework) Run() {
	f.controller.Run()
}
