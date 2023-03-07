package v1alpha1

import "fmt"

// InterfaceName returns the expected host interface for this Network
// If vlanID is specified, the expected tagged interface Name is returned
// otherwise the user specified interfaceName is returned
func (n *Network) InterfaceName() (string, error) {
	if n.Spec.NodeInterfaceMatcher.InterfaceName == nil || *n.Spec.NodeInterfaceMatcher.InterfaceName == "" {
		return "", fmt.Errorf("invalid network %s: network.spec.nodeInterfaceMatcher.InterfaceName cannot be nil or empty", n.Name)
	}
	hostInterface := n.Spec.NodeInterfaceMatcher.InterfaceName

	if n.Spec.L2NetworkConfig == nil || n.Spec.L2NetworkConfig.VlanID == nil {
		return *hostInterface, nil
	}

	return fmt.Sprintf("%s.%d", *hostInterface, *n.Spec.L2NetworkConfig.VlanID), nil
}
