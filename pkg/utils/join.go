package utils

import (
	"github.com/golang/glog"
	api_v1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/backendconfig"
)

// Joiner returns all Ingresses that are linked to another k8s resources
// by performing operations similar to a database "Join".
type Joiner struct {
	ingLister               StoreToIngressLister
	svcLister               cache.Indexer
	defaultBackendSvcPortID ServicePortID
}

func NewJoiner(ingLister StoreToIngressLister, svcLister cache.Indexer, defaultBackendSvcPortID ServicePortID) *Joiner {
	return &Joiner{ingLister, svcLister, defaultBackendSvcPortID}
}

// IngressesForService gets all the Ingresses that reference a Service.
func (j *Joiner) IngressesForService(svc *api_v1.Service) (ingList []*extensions.Ingress) {
	ings, err := j.ingLister.GetServiceIngress(svc, j.defaultBackendSvcPortID)
	if err != nil {
		glog.V(4).Infof("ignoring service %v: %v", svc.Name, err)
		return
	}
	for _, ing := range ings {
		if !IsGCEIngress(&ing) {
			continue
		}
		ingList = append(ingList, &ing)
	}
	return
}

// IngressesForBackendConfig gets all Ingresses that reference (indirectly) a BackendConfig.
// TODO(rramkumar): This can be optimized to remove nested loops.
func (j *Joiner) IngressesForBackendConfig(beConfig *backendconfigv1beta1.BackendConfig) (ingList []*extensions.Ingress) {
	// Get all the Services associated with this BackendConfig.
	linkedSvcs := backendconfig.GetServicesForBackendConfig(j.svcLister, beConfig)
	// Enqueue all the Ingresses associated with each Service.
	for _, svc := range linkedSvcs {
		ingsForSvc := j.IngressesForService(svc)
		ingList = append(ingList, ingsForSvc...)
	}
	return
}
