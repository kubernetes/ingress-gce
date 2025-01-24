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
}

func NewManager(recorderClient kubernetes.Interface, logger klog.Logger) *Manager {
	return &Manager{
		recorderClient: recorderClient,
		recorders:      make(map[Namespace]record.EventRecorder),
		lock:           sync.Mutex{},
		logger:         logger,
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
	rec := broadcaster.NewRecorder(m.generateScheme(), apiv1.EventSource{Component: "loadbalancer-controller"})
	m.recorders[ns] = rec

	return rec
}

// generateScheme creates a scheme and adds relevant CRD schemes that will be used
// for events
func (m *Manager) generateScheme() *runtime.Scheme {
	controllerScheme := scheme.Scheme

	if flags.F.EnablePSC {
		if err := sav1beta1.AddToScheme(controllerScheme); err != nil {
			m.logger.Error(err, "Failed to add v1beta1 ServiceAttachment CRD scheme to event recorder")
		}
		if err := sav1.AddToScheme(controllerScheme); err != nil {
			m.logger.Error(err, "Failed to add v1 ServiceAttachment CRD scheme to event recorder: %s")
		}
	}
	return controllerScheme
}
