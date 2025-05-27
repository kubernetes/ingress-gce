package optimized

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"k8s.io/ingress-gce/pkg/composite"
)

// ForwardingRulesRepository is used to not create a direct dependency on forwardingrules.
type ForwardingRulesRepository interface {
	List(*filter.F) ([]*composite.ForwardingRule, error)
}

// Fetch gets all of the ForwardingRules used by the specified Regional Backend Service.
// Passed backendServiceLink needs to be a full resource URL.
func Fetch(frRepo ForwardingRulesRepository, backendServiceLink string) ([]*composite.ForwardingRule, error) {
	const backendServiceFieldName = "BackendService"

	exactMatch := fmt.Sprintf("^%s$", backendServiceLink)
	f := filter.Regexp(backendServiceFieldName, exactMatch)
	return frRepo.List(f)
}
