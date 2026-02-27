package namer

import (
    "k8s.io/klog/v2"
)

const (
    mtNamerPrefix = "gk3-mt"
    // maxNEGDescriptiveLabel is the max length for namespace, name and
	// port for neg name.  63 - 8 (Prefix and naming schema version prefix)
	// - 8 (truncated principal entity id) - 8 (suffix hash) - 5 (hyphen connector) = 35
	maxMtNEGDescriptiveLabel = 35

    mtSchemaVersionV1 = "1"
)

// NewMTNamer creates a new namer for a specific tenant
func NewMTNamer(tenantUID string, logger klog.Logger) *Namer {

    baseNamer := NewNamerWithPrefix(mtNamerPrefix, tenantUID, "", logger)

    baseNamer.maxNEGLabelLength = maxMtNEGDescriptiveLabel
    baseNamer.negSchemaVersion = mtSchemaVersionV1

    return baseNamer
}



