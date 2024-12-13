package nodetopology

import (
	"fmt"

	nodetopologyv1 "github.com/GoogleCloudPlatform/gke-networking-api/apis/nodetopology/v1"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
)

// SubnetConfigFromSubnetURL parses a GCE subnetURL into a SubnetConfig struct.
//
//   - subnetURL can have formats accepted by the ParseResourceURL() function of
//     [github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud]
func SubnetConfigFromSubnetURL(subnetURL string) (nodetopologyv1.SubnetConfig, error) {
	resourceID, err := cloud.ParseResourceURL(subnetURL)
	if err != nil {
		return nodetopologyv1.SubnetConfig{}, fmt.Errorf("failed to parse subnetURL %q: %v", subnetURL, err)
	}
	subnetConfig := nodetopologyv1.SubnetConfig{
		Name:       resourceID.Key.Name,
		SubnetPath: cloud.RelativeResourceName(resourceID.ProjectID, resourceID.Resource, resourceID.Key),
	}
	return subnetConfig, nil
}
