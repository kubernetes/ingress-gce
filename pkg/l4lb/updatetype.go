package l4lb

import (
	"sync"

	"k8s.io/klog/v2"
)

// serviceVersions is a struct to track the versions of a service.
// An instance of this struct will be maintained for each service processed
// by the controller.
// lastSeenUpdateVersion should be set in the informer callback when the resource
// is about to be put in the processing queue.
// lastIgnoredVersion should be set in the informer callback when the resource is ignored.
// lastProcessedUpdateVersion and lastProcessingSuccess should be set at the end
// of the processing worker when the processing result is known.
//
// When the worker starts to process a service it will be now possible to tell if the operation is a resync or a real update.
// the logic is
// isResync = lastProcessingSuccess
// && (currentVersion == lastProcessedUpdateVersion || currentVersion == lastIgnoredVersion && lastSeenUpdateVersion == lastProcessedUpdateVersion)
type serviceVersions struct {
	lastSeenUpdateVersion      string
	lastProcessedUpdateVersion string
	lastIgnoredVersion         string
	lastProcessingSuccess      bool
}

// serviceVersionsTracker is a util to track the versions of a service and
// based on them determine if the resource to be processed is a resync or a full
// update operation.

type serviceVersionsTracker struct {
	lock     sync.Mutex
	versions map[string]*serviceVersions
}

// NewServiceVersionsTracker creates a new service versions tracker.
func NewServiceVersionsTracker() *serviceVersionsTracker {
	return &serviceVersionsTracker{versions: make(map[string]*serviceVersions)}
}

func (t *serviceVersionsTracker) getVersionsForSvc(svcKey string) *serviceVersions {
	v, ok := t.versions[svcKey]
	if ok {
		return v
	}
	v = &serviceVersions{}
	t.versions[svcKey] = v
	return v

}

func (t *serviceVersionsTracker) SetLastUpdateSeen(svcKey, version string, svcLogger klog.Logger) {
	t.lock.Lock()
	defer t.lock.Unlock()

	v := t.getVersionsForSvc(svcKey)
	v.lastSeenUpdateVersion = version
	svcLogger.V(3).Info("serviceVersionsTracker: SetLastUpdateSeen()", "version", version)
}

func (t *serviceVersionsTracker) SetLastIgnored(svcKey, version string, svcLogger klog.Logger) {
	t.lock.Lock()
	defer t.lock.Unlock()

	v := t.getVersionsForSvc(svcKey)
	v.lastIgnoredVersion = version
	svcLogger.V(3).Info("serviceVersionsTracker: SetLastIgnored()", "version", version)
}

func (t *serviceVersionsTracker) SetProcessed(svcKey, version string, success, wasResync bool, svcLogger klog.Logger) {
	if wasResync && success {
		return
	}
	t.lock.Lock()
	defer t.lock.Unlock()

	v := t.getVersionsForSvc(svcKey)
	v.lastProcessedUpdateVersion = version
	v.lastProcessingSuccess = success
	svcLogger.V(3).Info("serviceVersionsTracker: SetProcessed()", "version", version, "success", success, "wasResync", wasResync)
}

func (t *serviceVersionsTracker) IsResync(svcKey, currentVersion string, svcLogger klog.Logger) bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	v := t.getVersionsForSvc(svcKey)

	svcLogger.V(3).Info("serviceVersionsTracker: IsResync()", "currentVersion", currentVersion, "lastProcessedUpdateVersion", v.lastProcessedUpdateVersion, "lastProcessingSuccess", v.lastProcessingSuccess, "lastSeenUpdate", v.lastSeenUpdateVersion, "lastIgnored", v.lastIgnoredVersion)

	if !v.lastProcessingSuccess {
		return false
	}
	if currentVersion == v.lastProcessedUpdateVersion {
		return true
	}
	if currentVersion == v.lastIgnoredVersion && v.lastSeenUpdateVersion == v.lastProcessedUpdateVersion {
		return true
	}
	return false
}

func (t *serviceVersionsTracker) Delete(key string) {
	t.lock.Lock()
	defer t.lock.Unlock()

	delete(t.versions, key)
}
