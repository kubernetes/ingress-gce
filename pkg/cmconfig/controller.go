package cmconfig

import (
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// ConfigMapConfigController is the ConfigMap based config controller.
// If cmConfigModeEnabled set to true, it will load the config from configmap: configMapNamespace/configMapName and restart ingress controller if the config has any ligeal changes.
// If cmConfigModeEnabled set to false, it will return the default values for the configs.
type ConfigMapConfigController struct {
	configMapNamespace string
	configMapName      string
	currentConfig      *Config
	kubeClient         kubernetes.Interface
}

// NewConfigMapConfigController creates a new ConfigMapConfigController, it will load the config from the target configmap
func NewConfigMapConfigController(kubeClient kubernetes.Interface, configMapNamespace, configMapName string) *ConfigMapConfigController {

	currentConfig := NewConfig()
	cm, err := kubeClient.CoreV1().ConfigMaps(configMapNamespace).Get(configMapName, metav1.GetOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			klog.Warningf("ConfigMapConfigController failed to load config from api server, using the defualt config. Error: %v", err)
		} else {
			klog.Infof("ConfigMapConfigController: Not found the configmap based config, using default config: %v", currentConfig)
		}
	} else {
		currentConfig.LoadValue(cm.Data)
		klog.Infof("ConfigMapConfigController: loaded config from configmap, config %v", currentConfig)
	}

	c := &ConfigMapConfigController{
		configMapNamespace: configMapNamespace,
		configMapName:      configMapName,
		currentConfig:      &currentConfig,
		kubeClient:         kubeClient,
	}
	return c
}

// GetConfig returns the internal Config
func (c *ConfigMapConfigController) GetConfig() Config {
	return *c.currentConfig
}

// RegisterInformer regjister the configmap based config controller handler to the configapInformer which will watch the target
// configmap and send stop message to the stopCh if any valid change detected.
func (c *ConfigMapConfigController) RegisterInformer(configMapInformer cache.SharedIndexInformer, cancel func()) {
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
	configMap, ok := obj.(*v1.ConfigMap)
	if !ok {
		klog.Errorf("ConfigMapConfigController: failed to convert informer object to ConfigMap.")
	}
	if configMap.Namespace != c.configMapNamespace || configMap.Name != c.configMapName {
		return
	}

	config := NewConfig()
	cm, err := c.kubeClient.CoreV1().ConfigMaps(c.configMapNamespace).Get(c.configMapName, metav1.GetOptions{})
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			klog.Warningf("ConfigMapConfigController failed to load config from api server, using the defualt config. Error: %v", err)
		} else {
			klog.Infof("ConfigMapConfigController: Not found the configmap based config, using default config: %v", config)
		}
	} else {
		config.LoadValue(cm.Data)
	}

	if !config.Equals(c.currentConfig) {
		klog.Warningf("ConfigMapConfigController: Get a update on the ConfigMapConfig. Old config: %v, new config: %v. Reatarting Ingress controller...", *c.currentConfig, config)
		cancel()
	}
}
