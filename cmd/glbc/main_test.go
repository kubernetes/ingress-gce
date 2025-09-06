package main

import (
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"

	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/systemhealth"
	"k8s.io/klog/v2"
)

// TestOnStoppedLeadingClosesRootStop verifies that both electors invoke a
// process-wide graceful shutdown by calling closeStopCh.
func TestOnStoppedLeadingClosesRootStop(t *testing.T) {
	t.Parallel()

	client := kubefake.NewSimpleClientset()
	broadcaster := record.NewBroadcaster()
	t.Cleanup(broadcaster.Shutdown)
	rec := broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "glbc-test"})

	le := leaderElectionOption{
		client:   client,
		recorder: rec,
		id:       "test-id",
	}

	baseRunOption := runOption{
		stopCh:      make(chan struct{}),
		wg:          &sync.WaitGroup{},
		closeStopCh: func() {},
	}

	cases := []struct {
		name        string
		buildRunner func(runOption) (*leaderelection.LeaderElectionConfig, error)
	}{
		{
			name: "neg_elector",
			buildRunner: func(ro runOption) (*leaderelection.LeaderElectionConfig, error) {
				var ctx *ingctx.ControllerContext
				var sh *systemhealth.SystemHealth
				return makeNEGRunnerWithLeaderElection(ctx, sh, ro, le, klog.TODO())
			},
		},
		{
			name: "ingress_elector",
			buildRunner: func(ro runOption) (*leaderelection.LeaderElectionConfig, error) {
				var ctx *ingctx.ControllerContext
				var sh *systemhealth.SystemHealth
				return makeIngressRunnerWithLeaderElection(ctx, sh, ro, le, klog.TODO())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runOption := baseRunOption
			closed := make(chan struct{})
			runOption.closeStopCh = func() {
				select {
				case <-closed:
				default:
					close(closed)
				}
			}

			cfg, err := tc.buildRunner(runOption)
			if err != nil {
				t.Fatalf("build leader election config: %v", err)
			}
			if cfg == nil || cfg.Callbacks.OnStoppedLeading == nil {
				t.Fatalf("invalid cfg or nil OnStoppedLeading")
			}
			if !cfg.ReleaseOnCancel {
				t.Fatalf("ReleaseOnCancel = false, want true")
			}

			// Simulate loss of leadership.
			cfg.Callbacks.OnStoppedLeading()

			select {
			case <-closed:
				// ok
			case <-time.After(500 * time.Millisecond):
				t.Fatalf("expected closeStopCh to be called by OnStoppedLeading")
			}
		})
	}
}
