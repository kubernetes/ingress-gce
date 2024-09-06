package utils

// ResourceSyncStatus tracks the updates done to the GCE resources in the Ensure functions.
type ResourceSyncStatus bool

const (

	// ResourceResync when the existing resource was already present and already in a good state.
	ResourceResync ResourceSyncStatus = false

	// ResourceUpdate when the resource had to be created or updated (an update call to GCE was made).
	ResourceUpdate ResourceSyncStatus = true
)
