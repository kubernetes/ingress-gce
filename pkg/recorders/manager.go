package recorders

import (
	"sync"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	sav1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/klog/v2"
)

type Namespace = string

type Manager struct {
	recorderClient kubernetes.Interface
	recorders      map[Namespace]record.EventRecorder
	lock           sync.Mutex
	logger         klog.Logger
	// scheme is shared by all recorders. It is not modified after NewManager
	// returns, so the recorders can read it concurrently.
	scheme *runtime.Scheme
}

func NewManager(recorderClient kubernetes.Interface, logger klog.Logger) *Manager {
	return &Manager{
		recorderClient: recorderClient,
		recorders:      make(map[Namespace]record.EventRecorder),
		lock:           sync.Mutex{},
		logger:         logger,
		scheme:         newScheme(flags.F.EnablePSC, logger),
	}
}

// Recorder return the event recorder for the given namespace.
func (m *Manager) Recorder(ns Namespace) record.EventRecorder {
	m.lock.Lock()
	defer m.lock.Unlock()
	if rec, ok := m.recorders[ns]; ok {
		return rec
	}

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{
		Interface: m.recorderClient.CoreV1().Events(ns),
	})
	rec := broadcaster.NewRecorder(m.scheme, apiv1.EventSource{Component: "loadbalancer-controller"})
	m.recorders[ns] = rec

	return rec
}

// newScheme returns a scheme with the types that events can be recorded for:
// the client-go types and, if enablePSC is set, the ServiceAttachment CRD types.
//
// The types are added to a new scheme instead of scheme.Scheme, because
// scheme.Scheme is shared by the whole process and is not safe to modify while
// other goroutines read it.
func newScheme(enablePSC bool, logger klog.Logger) *runtime.Scheme {
	s := runtime.NewScheme()

	if err := scheme.AddToScheme(s); err != nil {
		logger.Error(err, "Failed to add client-go scheme to event recorder")
	}
	if enablePSC {
		if err := sav1beta1.AddToScheme(s); err != nil {
			logger.Error(err, "Failed to add v1beta1 ServiceAttachment CRD scheme to event recorder")
		}
		if err := sav1.AddToScheme(s); err != nil {
			logger.Error(err, "Failed to add v1 ServiceAttachment CRD scheme to event recorder")
		}
	}
	return s
}
