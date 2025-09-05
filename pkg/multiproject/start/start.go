package start

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	pccontroller "k8s.io/ingress-gce/pkg/multiproject/controller"
	"k8s.io/ingress-gce/pkg/multiproject/gce"
	multiprojectinformers "k8s.io/ingress-gce/pkg/multiproject/informerset"
	"k8s.io/ingress-gce/pkg/multiproject/manager"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	providerconfiginformers "k8s.io/ingress-gce/pkg/providerconfig/client/informers/externalversions"
	"k8s.io/ingress-gce/pkg/recorders"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const multiProjectLeaderElectionLockName = "ingress-gce-multi-project-lock"

// StartWithLeaderElection starts the ProviderConfig controller with leader election.
func StartWithLeaderElection(
	ctx context.Context,
	leaderElectKubeClient kubernetes.Interface,
	hostname string,
	logger klog.Logger,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	providerConfigClient providerconfigclient.Interface,
	gceCreator gce.GCECreator,
	rootNamer *namer.Namer,
	stopCh <-chan struct{},
) error {
	recordersManager := recorders.NewManager(eventRecorderKubeClient, logger)

	leConfig, err := makeLeaderElectionConfig(leaderElectKubeClient, hostname, recordersManager, logger, kubeClient, svcNegClient, networkClient, nodeTopologyClient, kubeSystemUID, eventRecorderKubeClient, providerConfigClient, gceCreator, rootNamer, stopCh)
	if err != nil {
		return err
	}

	leaderelection.RunOrDie(ctx, *leConfig)
	logger.Info("Multi-project controller exited.")

	return nil
}

func makeLeaderElectionConfig(
	leaderElectKubeClient kubernetes.Interface,
	hostname string,
	recordersManager *recorders.Manager,
	logger klog.Logger,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	providerConfigClient providerconfigclient.Interface,
	gceCreator gce.GCECreator,
	rootNamer *namer.Namer,
	stopCh <-chan struct{},
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

	return &leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: flags.F.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: flags.F.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   flags.F.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				Start(logger, kubeClient, svcNegClient, networkClient, nodeTopologyClient, kubeSystemUID, eventRecorderKubeClient, providerConfigClient, gceCreator, rootNamer, stopCh)
			},
			OnStoppedLeading: func() {
				logger.Info("Stop running multi-project leader election")
			},
		},
	}, nil
}

// Start starts the ProviderConfig controller.
// It creates SharedIndexInformers directly and starts the controller.
func Start(
	logger klog.Logger,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	providerConfigClient providerconfigclient.Interface,
	gceCreator gce.GCECreator,
	rootNamer *namer.Namer,
	stopCh <-chan struct{},
) {
	lpConfig := labels.PodLabelPropagationConfig{}
	if flags.F.EnableNEGLabelPropagation {
		lpConfigEnvVar := os.Getenv("LABEL_PROPAGATION_CONFIG")
		if err := json.Unmarshal([]byte(lpConfigEnvVar), &lpConfig); err != nil {
			logger.Error(err, "Failed to retrieve pod label propagation config")
		}
	}

	// Create and start all informers
	informers := multiprojectinformers.NewInformerSet(
		kubeClient,
		svcNegClient,
		networkClient,
		nodeTopologyClient,
		metav1.Duration{Duration: flags.F.ResyncPeriod},
	)

	// Start all informers
	err := informers.Start(stopCh, logger)
	if err != nil {
		logger.Error(err, "Failed to start informers")
		return
	}

	manager := manager.NewProviderConfigControllerManager(
		kubeClient,
		informers,
		providerConfigClient,
		svcNegClient,
		networkClient,
		nodeTopologyClient,
		eventRecorderKubeClient,
		kubeSystemUID,
		rootNamer,
		namer.NewL4Namer(string(kubeSystemUID), rootNamer),
		lpConfig,
		gceCreator,
		stopCh,
		logger,
	)

	// Create ProviderConfig informer
	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(providerConfigClient, flags.F.ResyncPeriod).Cloud().V1().ProviderConfigs().Informer()
	go providerConfigInformer.Run(stopCh)

	// Wait for provider config informer to sync
	logger.Info("Waiting for provider config informer to sync")
	if !cache.WaitForCacheSync(stopCh, providerConfigInformer.HasSynced) {
		err := fmt.Errorf("failed to sync provider config informer")
		logger.Error(err, "Failed to sync provider config informer")
		return
	}
	logger.Info("Provider config informer synced successfully")

	pcController := pccontroller.NewProviderConfigController(manager, providerConfigInformer, stopCh, logger)

	pcController.Run()
}
