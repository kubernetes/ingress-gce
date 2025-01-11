package start

import (
	"context"
	"encoding/json"
	"os"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/ingress-gce/cmd/glbc/app"
	ingresscontext "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	pccontroller "k8s.io/ingress-gce/pkg/multiproject/controller"
	"k8s.io/ingress-gce/pkg/multiproject/manager"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	providerconfiginformers "k8s.io/ingress-gce/pkg/providerconfig/client/informers/externalversions"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

func Start(
	ctx context.Context,
	kubeConfig *rest.Config,
	logger klog.Logger,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	rootNamer *namer.Namer,
	stopCh <-chan struct{},
) {
	providerConfigClient, err := providerconfigclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create ProviderConfig client: %v", err)
	}

	lpConfig := labels.PodLabelPropagationConfig{}
	if flags.F.EnableNEGLabelPropagation {
		lpConfigEnvVar := os.Getenv("LABEL_PROPAGATION_CONFIG")
		if err := json.Unmarshal([]byte(lpConfigEnvVar), &lpConfig); err != nil {
			logger.Error(err, "Failed to retrieve pod label propagation config")
		}
	}

	ctxConfig := ingresscontext.ControllerContextConfig{
		ResyncPeriod: flags.F.ResyncPeriod,
	}

	defaultGCEConfig, err := app.GCEConfString(logger)
	if err != nil {
		klog.Fatalf("Error getting default cluster GCE config: %v", err)
	}

	informersFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, ctxConfig.ResyncPeriod)

	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(providerConfigClient, ctxConfig.ResyncPeriod).Providerconfig().V1().ProviderConfigs().Informer()

	manager := manager.NewProviderConfigControllerManager(
		kubeClient,
		informersFactory,
		providerConfigClient,
		svcNegClient,
		eventRecorderKubeClient,
		kubeSystemUID,
		rootNamer,
		namer.NewL4Namer(string(kubeSystemUID), rootNamer),
		lpConfig,
		defaultGCEConfig,
		stopCh,
		logger,
	)

	pcController := pccontroller.NewProviderConfigController(manager, providerConfigInformer, stopCh, logger)

	pcController.Run()
}
