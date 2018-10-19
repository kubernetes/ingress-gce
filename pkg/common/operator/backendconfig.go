package operator

import (
	"github.com/golang/glog"

	"k8s.io/ingress-gce/pkg/annotations"
	backendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/backendconfig/v1beta1"

	api_v1 "k8s.io/api/core/v1"
)

// doesServiceReferenceBackendConfig returns true if the passed in Service directly references
// the passed in BackendConfig.
func doesServiceReferenceBackendConfig(svc *api_v1.Service, beConfig *backendconfigv1beta1.BackendConfig) bool {
	if svc.Namespace != beConfig.Namespace {
		return false
	}
	backendConfigNames, err := annotations.FromService(svc).GetBackendConfigs()
	if err != nil {
		// If the user did not provide the annotation at all, then we
		// do not want to log an error.
		if err != annotations.ErrBackendConfigAnnotationMissing {
			glog.Errorf("Failed to get BackendConfig names from service %s/%s: %v", svc.Namespace, svc.Name, err)
		}
		return false
	}
	if backendConfigNames != nil {
		if backendConfigNames.Default == beConfig.Name {
			return true
		}
		for _, backendConfigName := range backendConfigNames.Ports {
			if backendConfigName == beConfig.Name {
				return true
			}
		}
	}
	return false
}
