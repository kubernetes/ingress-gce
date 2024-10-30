package projectinformer

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/ingress-gce/pkg/flags"
)

// TestProjectInformer_AddEventHandler verifies that the
// projectinformer.AddEventHandler method does not return an error.
func TestProjectInformer_AddEventHandler(t *testing.T) {
	flags.F.MultiProjectCRDProjectNameLabel = "project-name-label"

	sharedInformer := cache.NewSharedIndexInformer(nil, &corev1.Pod{}, 0, nil)
	projectInformer := NewProjectInformer(sharedInformer, "test-project")

	handler := cache.ResourceEventHandlerFuncs{}

	_, err := projectInformer.AddEventHandler(handler)
	if err != nil {
		t.Fatalf("Failed to add event handler: %v", err)
	}
}

// TestNamespacedInformer_AddEventHandlerWithResyncPeriod verifies that the
// namespacedinformer.AddEventHandlerWithResyncPeriod method does not return an
// error.
func TestProjectInformer_AddEventHandlerWithResyncPeriod(t *testing.T) {
	flags.F.MultiProjectCRDProjectNameLabel = "project-name-label"

	testCases := []struct {
		desc         string
		projectName  string
		resyncPeriod time.Duration
	}{
		{
			desc:         "Add event handler with resync period",
			projectName:  "test-project",
			resyncPeriod: time.Minute,
		},
		{
			desc:         "Add event handler with zero resync period",
			projectName:  "test-project",
			resyncPeriod: 0,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			sharedInformer := cache.NewSharedIndexInformer(nil, &corev1.Pod{}, 0, nil)
			projectInformer := NewProjectInformer(sharedInformer, tc.projectName)

			handler := cache.ResourceEventHandlerFuncs{}
			_, err := projectInformer.AddEventHandlerWithResyncPeriod(handler, tc.resyncPeriod)
			if err != nil {
				t.Fatalf("Failed to add event handler with resync period: %v", err)
			}
		})
	}
}
