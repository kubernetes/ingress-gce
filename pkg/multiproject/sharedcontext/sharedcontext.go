package sharedcontext

import (
	"encoding/json"
	"os"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ingresscontext "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

type SharedContext struct {
	KubeClient              kubernetes.Interface
	KubeSystemUID           types.UID
	EventRecorderClient     kubernetes.Interface
	ClusterNamer            *namer.Namer
	ControllerContextConfig ingresscontext.ControllerContextConfig
	recorders               map[string]record.EventRecorder
	healthChecks            map[string]func() error
	Logger                  klog.Logger
	InformersFactory        informers.SharedInformerFactory
	LpConfig                labels.PodLabelPropagationConfig
	SvcNegClient            svcnegclient.Interface
	L4Namer                 *namer.L4Namer
	GlobalStopCh            <-chan struct{}
}

// NewSharedContext returns a new shared set of informers.
func NewSharedContext(
	kubeClient kubernetes.Interface,
	svcNegClient svcnegclient.Interface,
	kubeSystemUID types.UID,
	eventRecorderClient kubernetes.Interface,
	clusterNamer *namer.Namer,
	logger klog.Logger,
	stopCh <-chan struct{}) *SharedContext {

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

	context := &SharedContext{
		KubeClient:              kubeClient,
		KubeSystemUID:           kubeSystemUID,
		EventRecorderClient:     eventRecorderClient,
		ClusterNamer:            clusterNamer,
		ControllerContextConfig: ctxConfig,
		recorders:               map[string]record.EventRecorder{},
		healthChecks:            make(map[string]func() error),
		Logger:                  logger,
		InformersFactory:        informers.NewSharedInformerFactoryWithOptions(kubeClient, ctxConfig.ResyncPeriod),
		LpConfig:                lpConfig,
		SvcNegClient:            svcNegClient,
		GlobalStopCh:            stopCh,
		L4Namer:                 namer.NewL4Namer(string(kubeSystemUID), clusterNamer),
	}

	return context
}
