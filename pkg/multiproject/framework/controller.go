// Package framework implements the ProviderConfig controller that starts and stops controllers for each ProviderConfig.
package framework

import (
	"fmt"
	"math/rand"
	"runtime/debug"

	"k8s.io/client-go/tools/cache"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
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

const (
	providerConfigControllerName = "provider-config-controller"
	workersNum                   = 5
)

// controllerManager implements the logic for starting and stopping controllers for each ProviderConfig.
type controllerManager interface {
	StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error
	StopControllersForProviderConfig(pc *providerconfig.ProviderConfig) error
}

// Controller manages the ProviderConfig resource lifecycle.
// It watches for ProviderConfig changes and delegates to the manager to start/stop
// controllers for each ProviderConfig.
type Controller struct {
	manager controllerManager

	providerConfigLister cache.Indexer
	providerConfigQueue  utils.TaskQueue
	numWorkers           int
	logger               klog.Logger
	stopCh               <-chan struct{}
	hasSynced            func() bool
}

// New creates a new Controller that manages ProviderConfig resources.
func New(
	providerConfigClient providerconfigclient.Interface,
	providerConfigInformer cache.SharedIndexInformer,
	finalizerName string,
	controllerStarter ControllerStarter,
	stopCh <-chan struct{},
	logger klog.Logger,
) *Controller {
	manager := newManager(
		providerConfigClient,
		finalizerName,
		controllerStarter,
		logger,
	)
	return newController(manager, providerConfigInformer, stopCh, logger)
}

// newController creates a Controller with the given manager. Used for testing.
func newController(manager controllerManager, providerConfigInformer cache.SharedIndexInformer, stopCh <-chan struct{}, logger klog.Logger) *Controller {
	logger = logger.WithName(providerConfigControllerName)
	c := &Controller{
		providerConfigLister: providerConfigInformer.GetIndexer(),
		stopCh:               stopCh,
		numWorkers:           workersNum,
		logger:               logger,
		hasSynced:            providerConfigInformer.HasSynced,
		manager:              manager,
	}

	c.providerConfigQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers(providerConfigControllerName, "provider-configs", c.numWorkers, c.syncWrapper, logger)

	providerConfigInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.logger.V(4).Info("Enqueue add event", "object", obj)
				c.providerConfigQueue.Enqueue(obj)
			},
			UpdateFunc: func(old, cur interface{}) {
				c.logger.V(4).Info("Enqueue update event", "old", old, "new", cur)
				c.providerConfigQueue.Enqueue(cur)
			},
		})

	c.logger.Info("ProviderConfig controller created")
	return c
}

// Run starts the controller and blocks until the stop channel is closed.
func (c *Controller) Run() {
	defer c.shutdown()

	c.logger.Info("Starting ProviderConfig controller")

	c.logger.Info("Waiting for initial cache sync before starting ProviderConfig Controller")
	ok := cache.WaitForCacheSync(c.stopCh, c.hasSynced)
	if !ok {
		c.logger.Error(nil, "Failed to wait for initial cache sync before starting ProviderConfig Controller")
		return
	}

	c.logger.Info("Started ProviderConfig Controller", "numWorkers", c.numWorkers)
	c.providerConfigQueue.Run()

	<-c.stopCh
	c.logger.Info("ProviderConfig Controller exited")
}

func (c *Controller) shutdown() {
	c.logger.Info("Shutting down ProviderConfig Controller")
	c.providerConfigQueue.Shutdown()
}

func (c *Controller) syncWrapper(key string) error {
	syncID := rand.Int31()
	svcLogger := c.logger.WithValues("providerConfigKey", key, "syncId", syncID)

	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			svcLogger.Error(fmt.Errorf("panic in ProviderConfig sync worker goroutine: %v, stack: %s", r, stack), "Recovered from panic")
		}
	}()
	err := c.sync(key, svcLogger)
	if err != nil {
		svcLogger.Error(err, "Error syncing providerConfig", "key", key)
	}
	return err
}

func (c *Controller) sync(key string, logger klog.Logger) error {
	logger = logger.WithName("providerConfig.sync")

	providerConfig, exists, err := c.providerConfigLister.GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to lookup providerConfig for key %s: %w", key, err)
	}
	if !exists || providerConfig == nil {
		logger.V(3).Info("ProviderConfig does not exist anymore")
		return nil
	}
	pc, ok := providerConfig.(*providerconfig.ProviderConfig)
	if !ok {
		return fmt.Errorf("unexpected type for providerConfig, expected *ProviderConfig but got %T", providerConfig)
	}

	if pc.DeletionTimestamp != nil {
		logger.Info("ProviderConfig is being deleted, stopping controllers", "providerConfig", pc)

		err := c.manager.StopControllersForProviderConfig(pc)
		if err != nil {
			return fmt.Errorf("failed to stop controllers for providerConfig %v: %w", pc, err)
		}
		return nil
	}

	logger.Info("Syncing providerConfig", "providerConfig", pc)
	err = c.manager.StartControllersForProviderConfig(pc)
	if err != nil {
		return fmt.Errorf("failed to start controllers for providerConfig %v: %w", pc, err)
	}

	logger.Info("Successfully synced providerConfig", "providerConfig", pc)
	return nil
}
