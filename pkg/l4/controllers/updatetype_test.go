package controllers

import (
	"testing"

	"k8s.io/klog/v2"
)

const (
	svcKey = "test/testSvc"
)

func TestIsResync(t *testing.T) {
	tests := []struct {
		desc                 string
		lastProcessedVersion string
		lastProcessedSuccess bool
		lastUpdate           string
		lastIgnored          string
		currentVersion       string
		expectResync         bool
	}{
		{
			desc:           "first version",
			lastUpdate:     "1",
			currentVersion: "1",
			expectResync:   false,
		},
		{
			desc:                 "real update",
			lastProcessedVersion: "1",
			lastProcessedSuccess: true,
			lastUpdate:           "2",
			currentVersion:       "2",
			expectResync:         false,
		},
		{
			desc:                 "simple resync",
			lastProcessedVersion: "2",
			lastProcessedSuccess: true,
			lastUpdate:           "2",
			currentVersion:       "2",
			expectResync:         true,
		},
		{
			desc:                 "resync after ignored versions",
			lastProcessedVersion: "2",
			lastProcessedSuccess: true,
			lastUpdate:           "2",
			lastIgnored:          "4",
			currentVersion:       "4",
			expectResync:         true,
		},
		{
			desc:                 "update coming after ignored versions",
			lastProcessedVersion: "1",
			lastProcessedSuccess: true,
			lastUpdate:           "4",
			lastIgnored:          "3",
			currentVersion:       "4",
			expectResync:         false,
		},
		{
			desc:                 "retry after failure is not resync",
			lastProcessedVersion: "2",
			lastProcessedSuccess: false,
			lastUpdate:           "2",
			currentVersion:       "2",
			expectResync:         false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			versionsTracker := NewServiceVersionsTracker()
			versionsTracker.SetLastUpdateSeen(svcKey, tc.lastUpdate, klog.TODO())
			versionsTracker.SetLastIgnored(svcKey, tc.lastIgnored, klog.TODO())
			versionsTracker.SetProcessed(svcKey, tc.lastProcessedVersion, tc.lastProcessedSuccess, false, klog.TODO())

			if resync := versionsTracker.IsResync(svcKey, tc.currentVersion, klog.TODO()); resync != tc.expectResync {
				t.Errorf("unexpected result, want resync=%v, got=%v", tc.expectResync, resync)
			}
		})
	}
}

func TestSetProcessedDoesNotChangeLastProcessedUpdateIfOperationWasResync(t *testing.T) {
	versionsTracker := NewServiceVersionsTracker()
	versions := versionsTracker.getVersionsForSvc(svcKey)
	versions.lastProcessingSuccess = true
	versions.lastProcessedUpdateVersion = "1"

	versionsTracker.SetProcessed(svcKey, "2", true, true, klog.TODO())

	versions = versionsTracker.getVersionsForSvc(svcKey)
	if versions.lastProcessedUpdateVersion != "1" {
		t.Errorf("Expected the version to remain 1 but got=%s", versions.lastProcessedUpdateVersion)
	}
}

func TestSetProcessedChangesLastProcessedUpdateIfOperationWasUpdate(t *testing.T) {
	versionsTracker := NewServiceVersionsTracker()
	versions := versionsTracker.getVersionsForSvc(svcKey)
	versions.lastProcessingSuccess = true
	versions.lastProcessedUpdateVersion = "1"

	versionsTracker.SetProcessed(svcKey, "2", true, false, klog.TODO())

	versions = versionsTracker.getVersionsForSvc(svcKey)
	if versions.lastProcessedUpdateVersion != "2" {
		t.Errorf("Expected the version to be updated to 2 but got=%s", versions.lastProcessedUpdateVersion)
	}
}
