package cmconfig

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/utils/patch"
	"k8s.io/klog/v2"
)

// ConfigMapConfigController is the ConfigMap based config controller.
// If cmConfigModeEnabled set to true, it will load the config from configmap: configMapNamespace/configMapName and restart ingress controller if the config has any illegal changes.
// If cmConfigModeEnabled set to false, it will return the default values for the configs.
type ConfigMapConfigController struct {
	configMapNamespace     string
	configMapName          string
	currentConfig          *Config
	currentConfigMapObject *v1.ConfigMap
	configMapLister        corelisters.ConfigMapLister
	kubeClient             kubernetes.Interface
	recorder               record.EventRecorder

	logger klog.Logger
}

// NewConfigMapConfigController creates a new ConfigMapConfigController, it will load the config from the target configmap
func NewConfigMapConfigController(kubeClient kubernetes.Interface, recorder record.EventRecorder, configMapNamespace, configMapName string, logger klog.Logger) *ConfigMapConfigController {
	logger = logger.WithName("ConfigMapConfigController")

	currentConfig := NewConfig()
	cm, err := kubeClient.CoreV1().ConfigMaps(configMapNamespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ConfigMapConfigController: Not found the configmap based config, using default config", "defaultConfig", currentConfig)
		} else {
			logger.Info("ConfigMapConfigController failed to load config from api server, using the default config", "err", err)
		}
	} else {
		if err := currentConfig.LoadValue(cm.Data); err != nil {
			if recorder != nil {
				recorder.Event(cm, "Warning", "LoadValueError", err.Error())
			}
			logger.Info("LoadValue error", "err", err.Error())
		}
		logger.Info("ConfigMapConfigController: loaded config from configmap", "config", currentConfig)
	}

	c := &ConfigMapConfigController{
		configMapNamespace:     configMapNamespace,
		configMapName:          configMapName,
		currentConfig:          &currentConfig,
		kubeClient:             kubeClient,
		recorder:               recorder,
		currentConfigMapObject: cm,
		logger:                 logger,
	}
	return c
}

// GetConfig returns the internal Config
func (c *ConfigMapConfigController) GetConfig() Config {
	return *c.currentConfig
}

func (c *ConfigMapConfigController) updateASMReady(status string) {
	patchBytes, err := patch.StrategicMergePatchBytes(v1.ConfigMap{Data: map[string]string{}},
		v1.ConfigMap{Data: map[string]string{asmReady: status}}, v1.ConfigMap{})
	if err != nil {
		c.RecordEvent("Warning", "FailedToUpdateASMStatus", fmt.Sprintf("Failed to update ASM Status, failed to create patch for ASM ConfigMap, error: %s", err))
		return
	}
	cm, err := c.kubeClient.CoreV1().ConfigMaps(c.configMapNamespace).Patch(context.TODO(), c.configMapName, apimachinerytypes.MergePatchType, patchBytes, metav1.PatchOptions{}, "")
	if err != nil {
		if errors.IsNotFound(err) {
			return
		}
		c.RecordEvent("Warning", "FailedToUpdateASMStatus", fmt.Sprintf("Failed to patch ASM ConfigMap, error: %s", err))
		return
	}
	c.currentConfigMapObject = cm
}

// DisableASM sets the internal ASM mode to off and update the ASMReady to False.
func (c *ConfigMapConfigController) DisableASM() {
	c.currentConfig.EnableASM = false
	c.updateASMReady(falseValue)
}

// SetASMReadyTrue update the ASMReady to True.
func (c *ConfigMapConfigController) SetASMReadyTrue() {
	c.logger.V(0).Info("SetASMReadyTrue")
	c.updateASMReady(trueValue)
}

// SetASMReadyFalse update the ASMReady to False.
func (c *ConfigMapConfigController) SetASMReadyFalse() {
	c.logger.V(0).Info("SetASMReadyFalse")
	c.updateASMReady(falseValue)
}

// RecordEvent records a event to the ASMConfigmap
func (c *ConfigMapConfigController) RecordEvent(eventtype, reason, message string) bool {
	if c.recorder == nil || c.currentConfigMapObject == nil {
		return false
	}
	c.recorder.Event(c.currentConfigMapObject, eventtype, reason, message)
	return true
}

// RegisterInformer register the configmap based config controller handler to the configMapInformer which will watch the target
// configmap and send stop message to the stopCh if any valid change detected.
func (c *ConfigMapConfigController) RegisterInformer(configMapInformer cache.SharedIndexInformer, cancel func()) {
	c.configMapLister = corelisters.NewConfigMapLister(configMapInformer.GetIndexer())
	configMapInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.processItem(obj, cancel)
		},
		DeleteFunc: func(obj interface{}) {
			c.processItem(obj, cancel)
		},
		UpdateFunc: func(_, cur interface{}) {
			c.processItem(cur, cancel)
		},
	})

}

func (c *ConfigMapConfigController) processItem(obj interface{}, cancel func()) {
	c.logger.V(4).Info("ConfigMapConfigController.processItem()")

	configMap, ok := obj.(*v1.ConfigMap)
	if !ok {
		c.logger.Error(nil, "ConfigMapConfigController: failed to convert informer object to ConfigMap.")
	}

	c.logger.V(3).Info("ConfigMapConfigController.processItem", "itemKey", klog.KRef(configMap.Namespace, configMap.Name))

	if configMap.Namespace != c.configMapNamespace || configMap.Name != c.configMapName {
		return
	}

	config := NewConfig()
	cm, err := c.configMapLister.ConfigMaps(c.configMapNamespace).Get(c.configMapName)
	if err != nil {
		if errors.IsNotFound(err) {
			c.logger.Info("ConfigMapConfigController: Not found the configmap based config, using default config", "defaultConfig", config)
		} else {
			c.logger.Info("ConfigMapConfigController failed to load config from api server, using the default config", "err", err)
		}
	} else {
		c.currentConfigMapObject = cm
		if err := config.LoadValue(cm.Data); err != nil {
			c.RecordEvent("Warning", "LoadValueError", err.Error())
		}
	}

	if !config.Equals(c.currentConfig) {
		c.RecordEvent("Normal", "ASMConfigMapTiggerRestart", "ConfigMapConfigController: Get a update on the ConfigMapConfig, Restarting Ingress controller")
		cancel()
	} else {
		// If the config has no change, make sure the ASMReady is updated.
		if config.EnableASM {
			c.SetASMReadyTrue()
		} else {
			c.SetASMReadyFalse()
		}
	}
}
