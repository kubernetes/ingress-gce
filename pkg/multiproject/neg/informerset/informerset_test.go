package informerset

import (
	"testing"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	networkfake "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned/fake"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	nodetopologyfake "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	svcnegfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/utils/endpointslices"
	ktesting "k8s.io/klog/v2/ktesting"
)

// TestNewInformerSet_OptionalClients verifies that optional informers are created
// only when their corresponding CRD clients are provided.
func TestNewInformerSet_OptionalClients(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		withSvcNeg       bool
		withNetwork      bool
		withNodeTopology bool
		wantSvcNeg       bool
		wantNetwork      bool
		wantGKEParams    bool
		wantNodeTopology bool
	}{
		{
			name: "no-optional-clients",
		},
		{
			name:             "all-optional-clients",
			withSvcNeg:       true,
			withNetwork:      true,
			withNodeTopology: true,
			wantSvcNeg:       true,
			wantNetwork:      true,
			wantGKEParams:    true,
			wantNodeTopology: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			kubeClient := k8sfake.NewSimpleClientset()

			var svcNegClient svcnegclient.Interface
			if tc.withSvcNeg {
				svcNegClient = svcnegfake.NewSimpleClientset()
			}

			var netClient networkclient.Interface
			if tc.withNetwork {
				netClient = networkfake.NewSimpleClientset()
			}

			var topoClient nodetopologyclient.Interface
			if tc.withNodeTopology {
				topoClient = nodetopologyfake.NewSimpleClientset()
			}

			inf := NewInformerSet(kubeClient, svcNegClient, netClient, topoClient, metav1.Duration{Duration: 0})
			if inf == nil {
				t.Fatalf("NewInformerSet returned nil")
			}

			if got := inf.SvcNeg != nil; got != tc.wantSvcNeg {
				t.Errorf("SvcNeg: got %t, want %t", got, tc.wantSvcNeg)
			}
			if got := inf.Network != nil; got != tc.wantNetwork {
				t.Errorf("Network: got %t, want %t", got, tc.wantNetwork)
			}
			if got := inf.GkeNetworkParams != nil; got != tc.wantGKEParams {
				t.Errorf("GkeNetworkParams: got %t, want %t", got, tc.wantGKEParams)
			}
			if got := inf.NodeTopology != nil; got != tc.wantNodeTopology {
				t.Errorf("NodeTopology: got %t, want %t", got, tc.wantNodeTopology)
			}
		})
	}
}

// TestEndpointSlice_HasRequiredIndexers verifies that the EndpointSlice informer is initialized
// and includes both the namespace indexer and the NEG-specific service indexer.
func TestEndpointSlice_HasRequiredIndexers(t *testing.T) {
	t.Parallel()

	kubeClient := k8sfake.NewSimpleClientset()
	inf := NewInformerSet(kubeClient, nil, nil, nil, metav1.Duration{Duration: 0})
	if inf == nil {
		t.Fatalf("NewInformerSet returned nil")
	}

	if inf.EndpointSlice == nil {
		t.Fatalf("EndpointSlice informer must be initialized")
	}

	indexers := inf.EndpointSlice.GetIndexer().GetIndexers()
	if _, ok := indexers[cache.NamespaceIndex]; !ok {
		t.Errorf("EndpointSlice missing NamespaceIndex indexer")
	}
	if _, ok := indexers[endpointslices.EndpointSlicesByServiceIndex]; !ok {
		t.Errorf("EndpointSlice missing EndpointSlicesByServiceIndex indexer")
	}
}

// TestStart_Semantics verifies Start() idempotency, behavior with a closed stop channel,
// and that CombinedHasSynced reports true after caches sync.
func TestStart_Semantics(t *testing.T) {
	t.Parallel()

	logger, _ := ktesting.NewTestContext(t)

	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()

		kubeClient := k8sfake.NewSimpleClientset()
		inf := NewInformerSet(kubeClient, nil, nil, nil, metav1.Duration{Duration: 0})

		stop := make(chan struct{})
		defer close(stop)

		err := inf.Start(stop, logger)
		if err != nil {
			t.Fatalf("Start() returned error: %v", err)
		}
		err = inf.Start(stop, logger)
		if err != nil {
			t.Fatalf("Second Start() returned error: %v", err)
		}
	})

	t.Run("closed-stop-channel", func(t *testing.T) {
		t.Parallel()

		kubeClient := k8sfake.NewSimpleClientset()
		inf := NewInformerSet(kubeClient, nil, nil, nil, metav1.Duration{Duration: 0})

		stop := make(chan struct{})
		close(stop)

		err := inf.Start(stop, logger)
		if err == nil {
			t.Fatalf("expected error when stop channel is closed, got nil")
		}
		if inf.started {
			t.Fatalf("expected started=false when Start is called with a closed stop channel")
		}
	})

	t.Run("combined-has-synced", func(t *testing.T) {
		t.Parallel()

		kubeClient := k8sfake.NewSimpleClientset()
		inf := NewInformerSet(kubeClient, nil, nil, nil, metav1.Duration{Duration: 0})

		stop := make(chan struct{})
		defer close(stop)

		err := inf.Start(stop, logger)
		if err != nil {
			t.Fatalf("Start() returned error: %v", err)
		}
		if ok := cache.WaitForCacheSync(stop, inf.CombinedHasSynced()); !ok {
			t.Fatalf("timed out waiting for CombinedHasSynced to be true")
		}
	})
}

// TestFilterByProviderConfig_WrappingAndState verifies that FilterByProviderConfig wraps all
// non-nil informers, preserves nil optional informers, and propagates the 'started' state
// from the base set into the filtered view.
func TestFilterByProviderConfig_WrappingAndState(t *testing.T) {
	t.Parallel()

	kubeClient := k8sfake.NewSimpleClientset()
	svcClient := svcnegfake.NewSimpleClientset()

	inf := NewInformerSet(kubeClient, svcClient, nil, nil, metav1.Duration{Duration: 0})

	// Before starting, filtered should mirror started=false.
	filteredBefore := inf.FilterByProviderConfig("pc-1")
	if filteredBefore == nil {
		t.Fatalf("FilterByProviderConfig returned nil")
	}
	if filteredBefore.started {
		t.Fatalf("expected filtered InformerSet to have started=false before base Start")
	}
	if filteredBefore.Ingress == nil || filteredBefore.Service == nil || filteredBefore.Pod == nil || filteredBefore.Node == nil || filteredBefore.EndpointSlice == nil {
		t.Fatalf("expected core filtered informers to be non-nil before base Start")
	}
	if filteredBefore.SvcNeg == nil {
		t.Fatalf("expected SvcNeg filtered informer to be non-nil when present in base")
	}
	if filteredBefore.Network != nil || filteredBefore.GkeNetworkParams != nil || filteredBefore.NodeTopology != nil {
		t.Fatalf("expected absent optional informers to remain nil in filtered view")
	}

	// After starting, filtered should mirror started=true.
	logger, _ := ktesting.NewTestContext(t)
	stop := make(chan struct{})
	defer close(stop)

	err := inf.Start(stop, logger)
	if err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	filteredAfter := inf.FilterByProviderConfig("pc-1")
	if filteredAfter == nil {
		t.Fatalf("FilterByProviderConfig returned nil (after start)")
	}
	if !filteredAfter.started {
		t.Fatalf("expected filtered InformerSet to have started=true after base Start")
	}
}

// TestCombinedHasSynced_AllNil verifies that CombinedHasSynced returns true when
// all informers in the set are nil (no caches to sync).
func TestCombinedHasSynced_AllNil(t *testing.T) {
	t.Parallel()

	inf := &InformerSet{}
	syncFunc := inf.CombinedHasSynced()
	if !syncFunc() {
		t.Errorf("CombinedHasSynced should return true when all informers are nil")
	}
}

// TestFilterByProviderConfig_PreservesIndexers verifies that filtering preserves the
// indexers from the original EndpointSlice informer (critical for NEG controller behavior).
func TestFilterByProviderConfig_PreservesIndexers(t *testing.T) {
	t.Parallel()

	kubeClient := k8sfake.NewSimpleClientset()
	inf := NewInformerSet(kubeClient, nil, nil, nil, metav1.Duration{Duration: 0})

	original := inf.EndpointSlice.GetIndexer().GetIndexers()
	filtered := inf.FilterByProviderConfig("pc-1").EndpointSlice.GetIndexer().GetIndexers()

	if _, ok := filtered[cache.NamespaceIndex]; !ok {
		t.Errorf("filtered EndpointSlice missing NamespaceIndex")
	}
	if _, ok := filtered[endpointslices.EndpointSlicesByServiceIndex]; !ok {
		t.Errorf("filtered EndpointSlice missing EndpointSlicesByServiceIndex")
	}

	if len(filtered) != len(original) {
		t.Errorf("filtered indexers count mismatch: got=%d want=%d", len(filtered), len(original))
	}
}
