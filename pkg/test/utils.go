package test

import (
	"fmt"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/annotations"
	backendconfig "k8s.io/ingress-gce/pkg/apis/backendconfig/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	FinalizerAddFlag          = flag("enable-finalizer-add")
	FinalizerRemoveFlag       = flag("enable-finalizer-remove")
	EnableV2FrontendNamerFlag = flag("enable-v2-frontend-namer")
	testServiceName           = "ilbtest"
	testServiceNamespace      = "default"
)

var (
	BackendPort      = networkingv1.ServiceBackendPort{Number: 80}
	DefaultBeSvcPort = utils.ServicePort{
		ID:       utils.ServicePortID{Service: types.NamespacedName{Namespace: "system", Name: "default"}, Port: BackendPort},
		NodePort: 30000,
		Protocol: annotations.ProtocolHTTP,
	}
)

// NewIngress returns an Ingress with the given spec.
func NewIngress(name types.NamespacedName, spec networkingv1.IngressSpec) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		TypeMeta: meta_v1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: "networking/v1",
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

// NewL4ILBService creates a Service of type LoadBalancer with the Internal annotation.
func NewL4ILBService(onlyLocal bool, port int) *api_v1.Service {
	svc := &api_v1.Service{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:        testServiceName,
			Namespace:   testServiceNamespace,
			Annotations: map[string]string{gce.ServiceAnnotationLoadBalancerType: string(gce.LBTypeInternal)},
		},
		Spec: api_v1.ServiceSpec{
			Type:            api_v1.ServiceTypeLoadBalancer,
			SessionAffinity: api_v1.ServiceAffinityClientIP,
			Ports: []api_v1.ServicePort{
				{Name: "testport", Port: int32(port), Protocol: "TCP"},
			},
		},
	}
	if onlyLocal {
		svc.Spec.ExternalTrafficPolicy = api_v1.ServiceExternalTrafficPolicyTypeLocal
	}
	return svc
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
func Backend(name string, port networkingv1.ServiceBackendPort) *networkingv1.IngressBackend {
	return &networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: name,
			Port: port,
		},
	}
}

// DecodeIngress deserializes an Ingress object.
func DecodeIngress(data []byte) (*networkingv1.Ingress, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(data, nil, nil)
	if err != nil {
		return nil, err
	}

	return obj.(*networkingv1.Ingress), nil
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

// CreateAndInsertNodes adds the given nodeNames in the given zone as GCE instances, so they can be looked up in tests.
func CreateAndInsertNodes(gce *gce.Cloud, nodeNames []string, zoneName string) ([]*api_v1.Node, error) {
	nodes := []*api_v1.Node{}

	for _, name := range nodeNames {
		// Inserting the same node name twice causes an error - here we check if
		// the instance exists already before insertion.
		exists, err := GCEInstanceExists(name, gce)
		if err != nil {
			return nil, err
		}
		if !exists {
			err := gce.InsertInstance(
				gce.ProjectID(),
				zoneName,
				&compute.Instance{
					Name: name,
					Tags: &compute.Tags{
						Items: []string{name},
					},
				},
			)
			if err != nil {
				return nodes, err
			}
		}

		nodes = append(
			nodes,
			&api_v1.Node{
				ObjectMeta: meta_v1.ObjectMeta{
					Name: name,
					Labels: map[string]string{
						api_v1.LabelHostname:          name,
						api_v1.LabelZoneFailureDomain: zoneName,
					},
				},
				Status: api_v1.NodeStatus{
					NodeInfo: api_v1.NodeSystemInfo{
						KubeProxyVersion: "v1.7.2",
					},
					Conditions: []api_v1.NodeCondition{
						{Type: api_v1.NodeReady, Status: api_v1.ConditionTrue},
					},
				},
			},
		)

	}
	return nodes, nil
}

// GCEInstanceExists returns if a given instance name exists.
func GCEInstanceExists(name string, g *gce.Cloud) (bool, error) {
	zones, err := g.GetAllCurrentZones()
	if err != nil {
		return false, err
	}
	for _, zone := range zones.List() {
		ctx, cancel := cloud.ContextWithCallTimeout()
		defer cancel()
		if _, err := g.Compute().Instances().Get(ctx, meta.ZonalKey(name, zone)); err != nil {
			if utils.IsNotFoundError(err) {
				return false, nil
			} else {
				return false, err
			}
		} else {
			// instance has been found
			return true, nil
		}
	}
	return false, nil
}

// CheckEvent watches for events in the given FakeRecorder and checks if it matches the given string.
// It will be used in the l4 firewall XPN tests once TestEnsureLoadBalancerDeletedSucceedsOnXPN and others are
// uncommented.
func CheckEvent(recorder *record.FakeRecorder, expected string, shouldMatch bool) error {
	select {
	case received := <-recorder.Events:
		if strings.HasPrefix(received, expected) != shouldMatch {
			if shouldMatch {
				return fmt.Errorf("Should receive message \"%v\" but got \"%v\".", expected, received)
			} else {
				return fmt.Errorf("Unexpected event \"%v\".", received)
			}
		}
		return nil
	case <-time.After(2 * time.Second):
		if shouldMatch {
			return fmt.Errorf("Should receive message \"%v\" but got timed out.", expected)
		}
		return nil
	}
}

// Float64ToPtr returns float ptr for given float.
func Float64ToPtr(val float64) *float64 {
	return &val
}

// Int64ToPtr returns int ptr for given int.
func Int64ToPtr(val int64) *int64 {
	return &val
}

type FakeRecorderSource struct{}

func (_ *FakeRecorderSource) Recorder(ns string) record.EventRecorder {
	return record.NewFakeRecorder(100)
}

func GetPrometheusMetric(name string) (*dto.MetricFamily, error) {
	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil, err
	}
	for _, m := range metrics {
		if m.GetName() == name {
			return m, nil
		}
	}
	return nil, nil
}
