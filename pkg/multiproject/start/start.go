package start

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/ingress-gce/pkg/flags"
	pccontroller "k8s.io/ingress-gce/pkg/multiproject/controller"
	"k8s.io/ingress-gce/pkg/multiproject/manager"
	"k8s.io/ingress-gce/pkg/multiproject/sharedcontext"
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
	namer *namer.Namer,
	stopCh <-chan struct{},
) {
	providerConfigClient, err := providerconfigclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create ProviderConfig client: %v", err)
	}
	sharedContext := sharedcontext.NewSharedContext(
		kubeClient,
		svcNegClient,
		kubeSystemUID,
		eventRecorderKubeClient,
		namer,
		logger,
		stopCh,
	)
	manager := manager.NewProviderConfigControllerManager(
		kubeClient,
		sharedContext.InformersFactory,
		providerConfigClient,
		sharedContext.SvcNegClient,
		sharedContext.EventRecorderClient,
		sharedContext.KubeSystemUID,
		sharedContext.ClusterNamer,
		sharedContext.L4Namer,
		sharedContext.LpConfig,
		sharedContext.DefaultCloudConfig,
		sharedContext.GlobalStopCh,
		logger,
	)
	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(providerConfigClient, flags.F.ResyncPeriod).Providerconfig().V1().ProviderConfigs().Informer()

	pcController := pccontroller.NewProviderConfigController(manager, providerConfigInformer, stopCh, logger)

	pcController.Run()
}
