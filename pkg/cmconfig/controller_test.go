package cmconfig

import (
	"context"
	"flag"
	"strings"
	"testing"
	"time"

	"bytes"
	"os"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	testNamespace     = "kube-system"
	testConfigMapName = "test-configmap"
)

func TestNewConfigMapConfigControllerDefaultValue(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	var logBuf bytes.Buffer
	klog.SetOutput(&logBuf)
	defer func() {
		klog.SetOutput(os.Stderr)
	}()
	cmcController := NewConfigMapConfigController(kubeClient, nil, testNamespace, testConfigMapName)

	newConfig := NewConfig()
	config := cmcController.GetConfig()
	if !config.Equals(&newConfig) {
		t.Errorf("GetConfig should return the same config as NewConfig, got: %v, want: %v", cmcController.GetConfig(), NewConfig())
	}
}

func TestController(t *testing.T) {
	defaultConfig := NewConfig()
	klog.InitFlags(nil)
	flag.CommandLine.Parse([]string{"--logtostderr=false"})
	testcases := []struct {
		desc                 string
		defaultConfigMapData map[string]string
		updateConfigMapData  map[string]string
		wantConfig           *Config
		wantUpdateConfig     *Config
		wantStop             bool
		wantLog              string
		doNotWantLog         string
	}{
		{
			desc:                 "No configMap config exists, controller should return default config",
			defaultConfigMapData: nil,
			updateConfigMapData:  nil,
			wantConfig:           &defaultConfig,
			wantUpdateConfig:     nil,
			wantStop:             false,
			wantLog:              "Not found the configmap based config",
			doNotWantLog:         "",
		},
		{
			desc:                 "Update a default value shouldn't trigger restart",
			defaultConfigMapData: nil,
			updateConfigMapData:  map[string]string{"enable-asm": "false"},
			wantConfig:           &defaultConfig,
			wantUpdateConfig:     &defaultConfig,
			wantStop:             false,
			wantLog:              "Not found the configmap based config",
			doNotWantLog:         "",
		},
		{
			desc:                 "update the default config should trigger a restart",
			defaultConfigMapData: map[string]string{"enable-asm": "false"},
			updateConfigMapData:  map[string]string{"enable-asm": "true"},
			wantConfig:           &defaultConfig,
			wantUpdateConfig:     &Config{EnableASM: true, ASMServiceNEGSkipNamespaces: []string{"kube-system", "istio-system"}},
			wantStop:             true,
			wantLog:              "",
			doNotWantLog:         "Not found the configmap based config",
		},
		{
			desc:                 "invalid config should give the default config",
			defaultConfigMapData: map[string]string{"enable-asm": "TTTTT"},
			updateConfigMapData:  nil,
			wantConfig:           &defaultConfig,
			wantUpdateConfig:     nil,
			wantStop:             false,
			wantLog:              "unvalid value",
			doNotWantLog:         "",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			var logBuf bytes.Buffer
			stopped := false
			klog.SetOutput(&logBuf)
			defer func() {
				klog.SetOutput(os.Stderr)
			}()

			fakeClient := fake.NewSimpleClientset()
			cmInformer := informerv1.NewConfigMapInformer(fakeClient, "", 30*time.Second, utils.NewNamespaceIndexer())
			cmLister := cmInformer.GetIndexer()

			if tc.defaultConfigMapData != nil {
				fakeClient.CoreV1().ConfigMaps(testNamespace).Create(context.TODO(), &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testConfigMapName},
					Data:       tc.defaultConfigMapData}, metav1.CreateOptions{})
			}
			controller := NewConfigMapConfigController(fakeClient, nil, testNamespace, testConfigMapName)
			config := controller.GetConfig()
			if !config.Equals(tc.wantConfig) {
				t.Errorf("Default Config not equals to wantConfig, got: %v, want: %v", config, tc.wantConfig)
			}
			controller.RegisterInformer(cmInformer, func() {
				stopped = true
			})

			if tc.updateConfigMapData != nil {
				updateConfigMap := v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testConfigMapName},
					Data:       tc.updateConfigMapData}

				cmLister.Add(&updateConfigMap)
				fakeClient.CoreV1().ConfigMaps(testNamespace).Update(context.TODO(), &updateConfigMap, metav1.UpdateOptions{})
				controller.processItem(&updateConfigMap, func() {
					stopped = true
				})

			}
			if tc.wantStop && !stopped {
				t.Errorf("Controller should trigger the restart. stopped should be set to true, bug got: %v", stopped)
			}

			if tc.wantLog != "" && !strings.Contains(logBuf.String(), tc.wantLog) {
				t.Errorf("Missing log, got: %v, want: %v", logBuf.String(), tc.wantLog)
			}

			if tc.doNotWantLog != "" && strings.Contains(logBuf.String(), tc.doNotWantLog) {
				t.Errorf("Having not wanted log, got: %v, not want: %v", logBuf.String(), tc.doNotWantLog)
			}
		})

	}

}
