package instances

import (
	compute "google.golang.org/api/compute/v1"

	"k8s.io/ingress-gce/utils"
)

// Helper method to create instance groups.
// This method exists to ensure that we are using the same logic at all places.
func EnsureInstanceGroupsAndPorts(nodePool NodePool, namer *utils.Namer, ports []int64) ([]*compute.InstanceGroup, error) {
	return nodePool.EnsureInstanceGroupsAndPorts(namer.IGName(), ports)
}
