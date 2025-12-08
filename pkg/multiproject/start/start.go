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
	"k8s.io/ingress-gce/pkg/multiproject/common/finalizer"
	"k8s.io/ingress-gce/pkg/multiproject/common/gce"
	"k8s.io/ingress-gce/pkg/multiproject/framework"
	"k8s.io/ingress-gce/pkg/multiproject/neg"
	multiprojectinformers "k8s.io/ingress-gce/pkg/multiproject/neg/informerset"
	syncMetrics "k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
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
	parentCtx context.Context,
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
	syncerMetrics *syncMetrics.SyncerMetrics,
) error {
	logger.V(1).Info("Starting multi-project controller with leader election", "host", hostname)

	recordersManager := recorders.NewManager(eventRecorderKubeClient, logger)

	leConfig, err := makeLeaderElectionConfig(leaderElectKubeClient, hostname, recordersManager, logger, kubeClient, svcNegClient, networkClient, nodeTopologyClient, kubeSystemUID, eventRecorderKubeClient, providerConfigClient, gceCreator, rootNamer, syncerMetrics)
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
	networkClient networkclient.Interface,
	nodeTopologyClient nodetopologyclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderKubeClient kubernetes.Interface,
	providerConfigClient providerconfigclient.Interface,
	gceCreator gce.GCECreator,
	rootNamer *namer.Namer,
	syncerMetrics *syncMetrics.SyncerMetrics,
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
				Start(logger, kubeClient, svcNegClient, networkClient, nodeTopologyClient, kubeSystemUID, eventRecorderKubeClient, providerConfigClient, gceCreator, rootNamer, ctx.Done(), syncerMetrics)
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
	syncerMetrics *syncMetrics.SyncerMetrics,
) {
	logger.V(1).Info("Starting ProviderConfig controller")
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

	negStarter := neg.NewNEGControllerStarter(
		informers,
		kubeClient,
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
		syncerMetrics,
	)

	// Create ProviderConfig informer
	providerConfigInformer := providerconfiginformers.NewSharedInformerFactory(providerConfigClient, flags.F.ResyncPeriod).Cloud().V1().ProviderConfigs().Informer()
	logger.V(2).Info("Starting ProviderConfig informer")
	go providerConfigInformer.Run(stopCh)

	// Wait for provider config informer to sync
	logger.Info("Waiting for provider config informer to sync")
	if !cache.WaitForCacheSync(stopCh, providerConfigInformer.HasSynced) {
		err := fmt.Errorf("failed to sync provider config informer")
		logger.Error(err, "Failed to sync provider config informer")
		return
	}
	logger.Info("Provider config informer synced successfully")

	ctrl := framework.New(
		providerConfigClient,
		providerConfigInformer,
		finalizer.ProviderConfigNEGCleanupFinalizer,
		negStarter,
		stopCh,
		logger,
	)

	logger.V(1).Info("Running ProviderConfig controller")
	ctrl.Run()
	logger.V(1).Info("ProviderConfig controller stopped")
}
