// Package framework implements the ProviderConfig controller that starts and stops controllers for each ProviderConfig.
package framework

import (
	"fmt"
	"math/rand"
	"runtime/debug"

	"k8s.io/client-go/tools/cache"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	providerConfigControllerName = "provider-config-controller"
	workersNum                   = 5
)

// providerConfigControllerManager implements the logic for starting and stopping controllers for each ProviderConfig.
type providerConfigControllerManager interface {
	StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error
	StopControllersForProviderConfig(pc *providerconfig.ProviderConfig)
}

// providerConfigController is a controller that manages the ProviderConfig resource.
// It is responsible for starting and stopping controllers for each ProviderConfig.
// Currently, it only manages the NEG controller using the providerConfigControllerManager.
type providerConfigController struct {
	manager providerConfigControllerManager

	providerConfigLister cache.Indexer
	providerConfigQueue  utils.TaskQueue
	numWorkers           int
	logger               klog.Logger
	stopCh               <-chan struct{}
	hasSynced            func() bool
}

// newProviderConfigController creates a new instance of the ProviderConfig controller.
func newProviderConfigController(manager providerConfigControllerManager, providerConfigInformer cache.SharedIndexInformer, stopCh <-chan struct{}, logger klog.Logger) *providerConfigController {
	logger = logger.WithName(providerConfigControllerName)
	pcc := &providerConfigController{
		providerConfigLister: providerConfigInformer.GetIndexer(),
		stopCh:               stopCh,
		numWorkers:           workersNum,
		logger:               logger,
		hasSynced:            providerConfigInformer.HasSynced,
		manager:              manager,
	}

	pcc.providerConfigQueue = utils.NewPeriodicTaskQueueWithMultipleWorkers(providerConfigControllerName, "provider-configs", pcc.numWorkers, pcc.syncWrapper, logger)

	providerConfigInformer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pcc.logger.V(4).Info("Enqueue add event", "object", obj)
				pcc.providerConfigQueue.Enqueue(obj)
			},
			UpdateFunc: func(old, cur interface{}) {
				pcc.logger.V(4).Info("Enqueue update event", "old", old, "new", cur)
				pcc.providerConfigQueue.Enqueue(cur)
			},
		})

	pcc.logger.Info("ProviderConfig controller created")
	return pcc
}

func (pcc *providerConfigController) Run() {
	defer pcc.shutdown()

	pcc.logger.Info("Starting ProviderConfig controller")

	pcc.logger.Info("Waiting for initial cache sync before starting ProviderConfig Controller")
	ok := cache.WaitForCacheSync(pcc.stopCh, pcc.hasSynced)
	if !ok {
		pcc.logger.Error(nil, "Failed to wait for initial cache sync before starting ProviderConfig Controller")
		return
	}

	pcc.logger.Info("Started ProviderConfig Controller", "numWorkers", pcc.numWorkers)
	pcc.providerConfigQueue.Run()

	<-pcc.stopCh
}

func (pcc *providerConfigController) shutdown() {
	pcc.logger.Info("Shutting down ProviderConfig Controller")
	pcc.providerConfigQueue.Shutdown()
}

func (pcc *providerConfigController) syncWrapper(key string) error {
	syncID := rand.Int31()
	svcLogger := pcc.logger.WithValues("providerConfigKey", key, "syncId", syncID)

	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			svcLogger.Error(fmt.Errorf("panic in ProviderConfig sync worker goroutine: %v, stack: %s", r, stack), "Recovered from panic")
		}
	}()
	err := pcc.sync(key, svcLogger)
	if err != nil {
		svcLogger.Error(err, "Error syncing providerConfig", "key", key)
	}
	return err
}

func (pcc *providerConfigController) sync(key string, logger klog.Logger) error {
	logger = logger.WithName("providerConfig.sync")

	providerConfig, exists, err := pcc.providerConfigLister.GetByKey(key)
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

		pcc.manager.StopControllersForProviderConfig(pc)
		return nil
	}

	logger.Info("Syncing providerConfig", "providerConfig", pc)
	err = pcc.manager.StartControllersForProviderConfig(pc)
	if err != nil {
		return fmt.Errorf("failed to start controllers for providerConfig %v: %w", pc, err)
	}

	logger.Info("Successfully synced providerConfig", "providerConfig", pc)
	return nil
}
