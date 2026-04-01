package namer

import (
	"k8s.io/klog/v2"
)

const (
	mtNamerPrefix = "gk3-mt"
	// maxMTNegDescriptiveLabel is the max length for namespace, name and
	// port for neg name.  63 - 7 (Prefix and naming schema version prefix)
	// - 8 (truncated principal entity id) - 8 (suffix hash) - 5 (hyphen connector) = 35
	maxMTNegDescriptiveLabel = 35

	mtSchemaVersionV1 = "1"
)

// NewMTNamer creates a new namer for a specific tenant
func NewMTNamer(tenantUID string, firewallName string, logger klog.Logger) *Namer {

	baseNamer := NewNamerWithOptions(mtNamerPrefix, tenantUID, firewallName, maxMTNegDescriptiveLabel, mtSchemaVersionV1, logger)

	return baseNamer
}
