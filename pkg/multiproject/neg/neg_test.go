package neg

import (
	"testing"
	"time"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	multiprojectgce "k8s.io/ingress-gce/pkg/multiproject/gce"
	multiprojectinformers "k8s.io/ingress-gce/pkg/multiproject/informerset"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	svcnegfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	klog "k8s.io/klog/v2"
	ktesting "k8s.io/klog/v2/ktesting"
)

// TestStartNEGController_StopJoin verifies that the stop channel passed to the controller
// closes when either the global stop channel or the per-ProviderConfig stop channel closes.
func TestStartNEGController_StopJoin(t *testing.T) {

	logger, _ := ktesting.NewTestContext(t)
	kubeClient := k8sfake.NewSimpleClientset()
	informers := multiprojectinformers.NewInformerSet(kubeClient, svcnegfake.NewSimpleClientset(), networkclient.Interface(nil), nodetopologyclient.Interface(nil), metav1.Duration{})

	// Start base informers; they are not strictly required by our stubbed controller,
	// but mirrors real startup flow and ensures CombinedHasSynced would be true if used.
	globalStop := make(chan struct{})
	t.Cleanup(func() { close(globalStop) })
	if err := informers.Start(globalStop, logger); err != nil {
		t.Fatalf("start informers: %v", err)
	}

	// Provide required inputs for StartNEGController
	pc := &providerconfig.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: "pc-1"}}
	kubeSystemUID := types.UID("uid")
	rootNamer := namer.NewNamer("clusteruid", "", logger)
	l4Namer := namer.NewL4Namer(string(kubeSystemUID), rootNamer)
	lpCfg := labels.PodLabelPropagationConfig{}
	// Create a fake cloud with a valid SubnetworkURL via multiproject helper.
	gceCreator := multiprojectgce.NewGCEFake()
	// Minimal provider config for the GCE fake
	pcForCloud := &providerconfig.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-1"},
		Spec: providerconfig.ProviderConfigSpec{
			ProjectID:     "test-project",
			ProjectNumber: 123,
			NetworkConfig: providerconfig.ProviderNetworkConfig{
				Network:    "net-1",
				SubnetInfo: providerconfig.ProviderConfigSubnetInfo{Subnetwork: "sub-1"},
			},
		},
	}
	cloud, err := gceCreator.GCEForProviderConfig(pcForCloud, logger)
	if err != nil {
		t.Fatalf("create fake cloud: %v", err)
	}

	// Stub newNEGController to capture the stopCh passed in and to construct a minimal controller
	// that can run without panics.
	var capturedStopCh <-chan struct{}
	orig := newNEGController
	newNEGController = func(kc kubernetes.Interface, sc svcnegclient.Interface, ec kubernetes.Interface, uid types.UID,
		ing cache.SharedIndexInformer, svc cache.SharedIndexInformer, pod cache.SharedIndexInformer, node cache.SharedIndexInformer,
		es cache.SharedIndexInformer, sn cache.SharedIndexInformer, netInf cache.SharedIndexInformer, gke cache.SharedIndexInformer, nt cache.SharedIndexInformer,
		synced func() bool, l4 namer.L4ResourcesNamer, defSP utils.ServicePort, cloud negtypes.NetworkEndpointGroupCloud, zg *zonegetter.ZoneGetter, nm negtypes.NetworkEndpointGroupNamer,
		resync time.Duration, gc time.Duration, workers int, enableRR bool, runL4 bool, nonGCP bool, dualStack bool, lp labels.PodLabelPropagationConfig,
		multiNetworking bool, ingressRegional bool, runNetLB bool, readOnly bool, enableNEGsForIngress bool,
		stopCh <-chan struct{}, l klog.Logger) (*neg.Controller, error) {
		capturedStopCh = stopCh
		return neg.NewController(kc, sc, ec, uid, ing, svc, pod, node, es, sn, netInf, gke, nt, synced, l4, defSP, cloud, zg, nm,
			resync, gc, workers, enableRR, runL4, nonGCP, dualStack, lp, multiNetworking, ingressRegional, runNetLB, readOnly, enableNEGsForIngress, stopCh, l)
	}
	t.Cleanup(func() { newNEGController = orig })

	testCases := []struct {
		name          string
		closeProvider bool
	}{
		{name: "provider-stop-closes-joined", closeProvider: true},
		{name: "global-stop-closes-joined", closeProvider: false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {

			var joinStop <-chan struct{}
			var providerStop chan<- struct{}

			if tc.closeProvider {
				// Wire the join to the real globalStop for this subcase.
				joinStop = globalStop
				var err error
				providerStop, err = StartNEGController(informers, kubeClient, kubeClient, svcnegfake.NewSimpleClientset(), networkclient.Interface(nil), nodetopologyclient.Interface(nil), kubeSystemUID, rootNamer, l4Namer, lpCfg, cloud, joinStop, logger, pc)
				if err != nil {
					t.Fatalf("StartNEGController: %v", err)
				}
				close(providerStop)
			} else {
				// Use a dedicated join channel so informers keep running.
				js := make(chan struct{})
				joinStop = js
				var err error
				providerStop, err = StartNEGController(informers, kubeClient, kubeClient, svcnegfake.NewSimpleClientset(), networkclient.Interface(nil), nodetopologyclient.Interface(nil), kubeSystemUID, rootNamer, l4Namer, lpCfg, cloud, joinStop, logger, &providerconfig.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: "pc-2"}})
				if err != nil {
					t.Fatalf("StartNEGController (2): %v", err)
				}
				close(js)
				defer close(providerStop) // safe if already closed
			}

			if capturedStopCh == nil {
				t.Fatalf("capturedStopCh is nil; stub did not run")
			}
			select {
			case <-capturedStopCh:
				// ok
			case <-time.After(2 * time.Second):
				t.Fatalf("joined stopCh did not close for case %q", tc.name)
			}
		})
	}
}

// TestStartNEGController_NilSvcNegClientErrors verifies StartNEGController returns an error
// when the svcneg client is nil (which makes controller construction fail).
func TestStartNEGController_NilSvcNegClientErrors(t *testing.T) {
	t.Parallel()

	logger, _ := ktesting.NewTestContext(t)
	kubeClient := k8sfake.NewSimpleClientset()
	informers := multiprojectinformers.NewInformerSet(kubeClient, nil, networkclient.Interface(nil), nodetopologyclient.Interface(nil), metav1.Duration{})
	globalStop := make(chan struct{})
	t.Cleanup(func() { close(globalStop) })
	if err := informers.Start(globalStop, logger); err != nil {
		t.Fatalf("start informers: %v", err)
	}

	pc := &providerconfig.ProviderConfig{ObjectMeta: metav1.ObjectMeta{Name: "pc-err"}}
	kubeSystemUID := types.UID("uid")
	rootNamer := namer.NewNamer("clusteruid", "", logger)
	l4Namer := namer.NewL4Namer(string(kubeSystemUID), rootNamer)
	lpCfg := labels.PodLabelPropagationConfig{}
	gceCreator := multiprojectgce.NewGCEFake()
	pcForCloud := &providerconfig.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "pc-err"},
		Spec: providerconfig.ProviderConfigSpec{
			ProjectID:     "test-project",
			ProjectNumber: 123,
			NetworkConfig: providerconfig.ProviderNetworkConfig{
				Network:    "net-1",
				SubnetInfo: providerconfig.ProviderConfigSubnetInfo{Subnetwork: "sub-1"},
			},
		},
	}
	cloud, err := gceCreator.GCEForProviderConfig(pcForCloud, logger)
	if err != nil {
		t.Fatalf("create fake cloud: %v", err)
	}

	// newNEGController remains default (neg.NewController), which errors when svcNegClient is nil
	ch, err := StartNEGController(informers, kubeClient, kubeClient, nil /* svcneg */, networkclient.Interface(nil), nodetopologyclient.Interface(nil), kubeSystemUID, rootNamer, l4Namer, lpCfg, cloud, globalStop, logger, pc)
	if err == nil {
		t.Fatalf("expected error from StartNEGController when svcNegClient is nil, got nil and channel=%v", ch)
	}
}
