package test

import (
	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
)

// NewIngress returns an Ingress with the given spec.
func NewIngress(name types.NamespacedName, spec extensions.IngressSpec) *extensions.Ingress {
	return &extensions.Ingress{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "extensions/v1beta1",
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
func Backend(name string, port intstr.IntOrString) *extensions.IngressBackend {
	return &extensions.IngressBackend{
		ServiceName: name,
		ServicePort: port,
	}
}

// DecodeIngress deserializes an Ingress object.
func DecodeIngress(data []byte) (*extensions.Ingress, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(data, nil, nil)
	if err != nil {
		return nil, err
	}

	return obj.(*extensions.Ingress), nil
}
