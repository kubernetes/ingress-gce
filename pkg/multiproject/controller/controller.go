package controller

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	providerConfigControllerName = "provider-config-controller"
	workersNum                   = 5
)

type ProviderConfigControllerManager interface {
	StartControllersForProviderConfig(pc *providerconfig.ProviderConfig) error
	StopControllersForProviderConfig(pc *providerconfig.ProviderConfig)
}

type ProviderConfigController struct {
	manager ProviderConfigControllerManager

	providerConfigLister cache.Indexer
	providerConfigQueue  utils.TaskQueue
	numWorkers           int
	logger               klog.Logger
	stopCh               <-chan struct{}
	hasSynced            func() bool
}

// NewProviderConfigController creates a new instance of the ProviderConfig controller.
func NewProviderConfigController(manager ProviderConfigControllerManager, providerConfigInformer cache.SharedIndexInformer, stopCh <-chan struct{}, logger klog.Logger) *ProviderConfigController {
	logger = logger.WithName(providerConfigControllerName)
	pcc := &ProviderConfigController{
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
			AddFunc:    func(obj interface{}) { pcc.providerConfigQueue.Enqueue(obj) },
			UpdateFunc: func(old, cur interface{}) { pcc.providerConfigQueue.Enqueue(cur) },
		})

	pcc.logger.Info("ProviderConfig controller created")
	return pcc
}

func (pcc *ProviderConfigController) Run() {
	defer pcc.shutdown()

	pcc.logger.Info("Starting ProviderConfig controller")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-pcc.stopCh
		cancel()
	}()

	err := wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
		pcc.logger.Info("Waiting for initial cache sync before starting ProviderConfig Controller")
		return pcc.hasSynced(), nil
	})
	if err != nil {
		pcc.logger.Error(err, "Failed to wait for initial cache sync before starting ProviderConfig Controller")
	}

	pcc.logger.Info("Started ProviderConfig Controller", "numWorkers", pcc.numWorkers)
	pcc.providerConfigQueue.Run()

	<-pcc.stopCh
}

func (pcc *ProviderConfigController) shutdown() {
	pcc.logger.Info("Shutting down ProviderConfig Controller")
	pcc.providerConfigQueue.Shutdown()
}

func (pcc *ProviderConfigController) syncWrapper(key string) error {
	syncID := rand.Int31()
	svcLogger := pcc.logger.WithValues("providerConfigKey", key, "syncId", syncID)

	defer func() {
		if r := recover(); r != nil {
			svcLogger.Error(fmt.Errorf("panic in ProviderConfig sync worker goroutine: %v", r), "Recovered from panic")
		}
	}()
	err := pcc.sync(key, svcLogger)
	if err != nil {
		svcLogger.Error(err, "Error syncing providerConfig", "key", key)
	}
	return err
}

func (pcc *ProviderConfigController) sync(key string, logger klog.Logger) error {
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

	logger.V(2).Info("Syncing providerConfig", "providerConfig", pc)
	err = pcc.manager.StartControllersForProviderConfig(pc)
	if err != nil {
		return fmt.Errorf("failed to start controllers for providerConfig %v: %w", pc, err)
	}

	logger.V(2).Info("Successfully synced providerConfig", "providerConfig", pc)
	return nil
}
