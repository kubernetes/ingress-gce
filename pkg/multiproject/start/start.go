package start

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"

	informernetwork "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions"
	informernodetopology "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/informers/externalversions"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	pccontroller "k8s.io/ingress-gce/pkg/multiproject/controller"
	"k8s.io/ingress-gce/pkg/multiproject/gce"
	"k8s.io/ingress-gce/pkg/multiproject/manager"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	providerconfiginformers "k8s.io/ingress-gce/pkg/providerconfig/client/informers/externalversions"
	"k8s.io/ingress-gce/pkg/recorders"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const multiProjectLeaderElectionLockName = "ingress-gce-multi-project-lock"

// StartWithLeaderElection starts the ProviderConfig controller with leader election.
func StartWithLeaderElection(
	parentCtx context.Context,
	leaderElectKubeClient kubernetes.Interface,
	hostname string,
	logger klog.Logger,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	providerConfigClient providerconfigclient.Interface,
	informersFactory informers.SharedInformerFactory,
	svcNegFactory informersvcneg.SharedInformerFactory,
	networkFactory informernetwork.SharedInformerFactory,
	nodeTopologyFactory informernodetopology.SharedInformerFactory,
	gceCreator gce.GCECreator,
	rootNamer *namer.Namer,
	stopCh <-chan struct{},
) error {
	logger.V(1).Info("Starting multi-project controller with leader election", "host", hostname)

	recordersManager := recorders.NewManager(eventRecorderKubeClient, logger)

	leConfig, err := makeLeaderElectionConfig(leaderElectKubeClient, hostname, recordersManager, logger, kubeClient, svcNegClient, kubeSystemUID, eventRecorderKubeClient, providerConfigClient, informersFactory, svcNegFactory, networkFactory, nodeTopologyFactory, gceCreator, rootNamer)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(parentCtx)
	go func() {
		<-stopCh
		logger.V(1).Info("Received stop signal; canceling leader election context")
		cancel()
	}()
	logger.V(1).Info("Starting leader election loop")
	leaderelection.RunOrDie(ctx, *leConfig)
	logger.V(1).Info("Multi-project controller exited.")

	return nil
}

func makeLeaderElectionConfig(
	leaderElectKubeClient kubernetes.Interface,
	hostname string,
	recordersManager *recorders.Manager,
	logger klog.Logger,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	providerConfigClient providerconfigclient.Interface,
	informersFactory informers.SharedInformerFactory,
	svcNegFactory informersvcneg.SharedInformerFactory,
	networkFactory informernetwork.SharedInformerFactory,
	nodeTopologyFactory informernodetopology.SharedInformerFactory,
	gceCreator gce.GCECreator,
	rootNamer *namer.Namer,
) (*leaderelection.LeaderElectionConfig, error) {
	recorder := recordersManager.Recorder(flags.F.LeaderElection.LockObjectNamespace)
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := fmt.Sprintf("%v_%x", hostname, rand.Intn(1e6))

	rl, err := resourcelock.New(resourcelock.LeasesResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		multiProjectLeaderElectionLockName,
		leaderElectKubeClient.CoreV1(),
		leaderElectKubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}
	logger.V(2).Info("Created resource lock for leader election", "id", id, "lockName", multiProjectLeaderElectionLockName)

	lec := &leaderelection.LeaderElectionConfig{
		Lock:            rl,
		LeaseDuration:   flags.F.LeaderElection.LeaseDuration.Duration,
		RenewDeadline:   flags.F.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:     flags.F.LeaderElection.RetryPeriod.Duration,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				logger.Info("Became leader, starting multi-project controller")
				Start(logger, kubeClient, svcNegClient, kubeSystemUID, eventRecorderKubeClient, providerConfigClient, informersFactory, svcNegFactory, networkFactory, nodeTopologyFactory, gceCreator, rootNamer, ctx.Done())
			},
			OnStoppedLeading: func() {
				logger.Info("Stop running multi-project leader election")
			},
		},
	}
	logger.V(2).Info("Initialized leader election config", "leaseDuration", lec.LeaseDuration, "renewDeadline", lec.RenewDeadline, "retryPeriod", lec.RetryPeriod)
	return lec, nil
}

// Start starts the ProviderConfig controller.
// It builds required context and starts the controller.
func Start(
	logger klog.Logger,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	providerConfigClient providerconfigclient.Interface,
	informersFactory informers.SharedInformerFactory,
	svcNegFactory informersvcneg.SharedInformerFactory,
	networkFactory informernetwork.SharedInformerFactory,
	nodeTopologyFactory informernodetopology.SharedInformerFactory,
	gceCreator gce.GCECreator,
	rootNamer *namer.Namer,
	stopCh <-chan struct{},
) {
	logger.V(1).Info("Starting ProviderConfig controller")
	lpConfig := labels.PodLabelPropagationConfig{}
	if flags.F.EnableNEGLabelPropagation {
		lpConfigEnvVar := os.Getenv("LABEL_PROPAGATION_CONFIG")
		if err := json.Unmarshal([]byte(lpConfigEnvVar), &lpConfig); err != nil {
			logger.Error(err, "Failed to retrieve pod label propagation config")
		}
	}

	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(providerConfigClient, flags.F.ResyncPeriod).Cloud().V1().ProviderConfigs().Informer()
	logger.V(2).Info("Starting ProviderConfig informer")
	go providerConfigInformer.Run(stopCh)

	manager := manager.NewProviderConfigControllerManager(
		kubeClient,
		informersFactory,
		svcNegFactory,
		networkFactory,
		nodeTopologyFactory,
		providerConfigClient,
		svcNegClient,
		eventRecorderKubeClient,
		kubeSystemUID,
		rootNamer,
		namer.NewL4Namer(string(kubeSystemUID), rootNamer),
		lpConfig,
		gceCreator,
		stopCh,
		logger,
	)
	logger.V(1).Info("Initialized ProviderConfig controller manager")

	pcController := pccontroller.NewProviderConfigController(manager, providerConfigInformer, stopCh, logger)
	logger.V(1).Info("Running ProviderConfig controller")
	pcController.Run()
	logger.V(1).Info("ProviderConfig controller stopped")
}
