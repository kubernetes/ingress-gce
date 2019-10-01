package test

import (
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	FinalizerAddFlag          = flag("enable-finalizer-add")
	FinalizerRemoveFlag       = flag("enable-finalizer-remove")
	EnableV2FrontendNamerFlag = flag("enable-v2-frontend-namer")
)

var (
	BackendPort      = intstr.IntOrString{Type: intstr.Int, IntVal: 80}
	DefaultBeSvcPort = utils.ServicePort{
		ID:       utils.ServicePortID{Service: types.NamespacedName{Namespace: "system", Name: "default"}, Port: BackendPort},
		NodePort: 30000,
		Protocol: annotations.ProtocolHTTP,
	}
)

// NewIngress returns an Ingress with the given spec.
func NewIngress(name types.NamespacedName, spec v1beta1.IngressSpec) *v1beta1.Ingress {
	return &v1beta1.Ingress{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking/v1beta1",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Spec: spec,
	}
}

// NewService returns a Service with the given spec.
func NewService(name types.NamespacedName, spec api_v1.ServiceSpec) *api_v1.Service {
	return &api_v1.Service{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Spec: spec,
	}
}

// NewBackendConfig returns a BackendConfig with the given spec.
func NewBackendConfig(name types.NamespacedName, spec backendconfig.BackendConfigSpec) *backendconfig.BackendConfig {
	return &backendconfig.BackendConfig{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
		Spec: spec,
	}
}

// Backend returns an IngressBackend with the given service name/port.
func Backend(name string, port intstr.IntOrString) *v1beta1.IngressBackend {
	return &v1beta1.IngressBackend{
		ServiceName: name,
		ServicePort: port,
	}
}

// DecodeIngress deserializes an Ingress object.
func DecodeIngress(data []byte) (*v1beta1.Ingress, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(data, nil, nil)
	if err != nil {
		return nil, err
	}

	return obj.(*v1beta1.Ingress), nil
}

// flag is a type representing controller flag.
type flag string

// FlagSaver is an utility type to capture the value of a flag and reset back to the saved value.
type FlagSaver struct{ flags map[flag]bool }

// NewFlagSaver returns a flag saver by initializing the map.
func NewFlagSaver() FlagSaver {
	return FlagSaver{make(map[flag]bool)}
}

// Save captures the value of given flag.
func (s *FlagSaver) Save(key flag, flagPointer *bool) {
	s.flags[key] = *flagPointer
}

// Reset resets the value of given flag to a previously saved value.
// This does nothing if the flag value was not captured.
func (s *FlagSaver) Reset(key flag, flagPointer *bool) {
	if val, ok := s.flags[key]; ok {
		*flagPointer = val
	}
}
