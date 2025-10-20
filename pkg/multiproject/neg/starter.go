package neg

import (
	"fmt"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/common/gce"
	multiprojectinformers "k8s.io/ingress-gce/pkg/multiproject/neg/informerset"
	syncMetrics "k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

// NEGControllerStarter implements framework.ControllerStarter for NEG controllers.
// It encapsulates all NEG-specific dependencies and startup logic.
type NEGControllerStarter struct {
	informers           *multiprojectinformers.InformerSet
	kubeClient          kubernetes.Interface
	svcNegClient        svcnegclient.Interface
	networkClient       networkclient.Interface
	nodetopologyClient  nodetopologyclient.Interface
	eventRecorderClient kubernetes.Interface
	kubeSystemUID       types.UID
	clusterNamer        *namer.Namer
	l4Namer             *namer.L4Namer
	lpConfig            labels.PodLabelPropagationConfig
	gceCreator          gce.GCECreator
	globalStopCh        <-chan struct{}
	logger              klog.Logger
	syncerMetrics       *syncMetrics.SyncerMetrics
}

// NewNEGControllerStarter creates a new NEG controller starter with the given dependencies.
func NewNEGControllerStarter(
	informers *multiprojectinformers.InformerSet,
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	networkClient networkclient.Interface,
	nodetopologyClient nodetopologyclient.Interface,
	eventRecorderClient kubernetes.Interface,
	kubeSystemUID types.UID,
	clusterNamer *namer.Namer,
	l4Namer *namer.L4Namer,
	lpConfig labels.PodLabelPropagationConfig,
	gceCreator gce.GCECreator,
	globalStopCh <-chan struct{},
	logger klog.Logger,
	syncerMetrics *syncMetrics.SyncerMetrics,
) *NEGControllerStarter {
	return &NEGControllerStarter{
		informers:           informers,
		kubeClient:          kubeClient,
		svcNegClient:        svcNegClient,
		networkClient:       networkClient,
		nodetopologyClient:  nodetopologyClient,
		eventRecorderClient: eventRecorderClient,
		kubeSystemUID:       kubeSystemUID,
		clusterNamer:        clusterNamer,
		l4Namer:             l4Namer,
		lpConfig:            lpConfig,
		gceCreator:          gceCreator,
		globalStopCh:        globalStopCh,
		logger:              logger,
		syncerMetrics:       syncerMetrics,
	}
}

// StartController implements framework.ControllerStarter.
// It creates a GCE client for the ProviderConfig and starts the NEG controller.
func (s *NEGControllerStarter) StartController(pc *providerconfig.ProviderConfig) (chan<- struct{}, error) {
	logger := s.logger.WithValues("providerConfigId", pc.Name)

	cloud, err := s.gceCreator.GCEForProviderConfig(pc, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCE client for provider config %+v: %w", pc, err)
	}

	negControllerStopCh, err := StartNEGController(
		s.informers,
		s.kubeClient,
		s.eventRecorderClient,
		s.svcNegClient,
		s.networkClient,
		s.nodetopologyClient,
		s.kubeSystemUID,
		s.clusterNamer,
		s.l4Namer,
		s.lpConfig,
		cloud,
		s.globalStopCh,
		logger,
		pc,
		s.syncerMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start NEG controller: %w", err)
	}

	return negControllerStopCh, nil
}
