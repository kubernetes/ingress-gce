package manager

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	cloudgce "k8s.io/cloud-provider-gcp/providers/gce"
	providerconfig "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/multiproject/finalizer"
	multiprojectgce "k8s.io/ingress-gce/pkg/multiproject/gce"
	multiprojectinformers "k8s.io/ingress-gce/pkg/multiproject/informerset"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	providerconfigclientfake "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned/fake"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	svcnegfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/utils/namer"
	klog "k8s.io/klog/v2"
	ktesting "k8s.io/klog/v2/ktesting"
)

// stubbed start function handle
type startCall struct {
	pcName string
}

// failingGCECreator is a test double that fails GCE client creation.
type failingGCECreator struct{}

func (f failingGCECreator) GCEForProviderConfig(_ *providerconfig.ProviderConfig, _ klog.Logger) (*cloudgce.Cloud, error) {
	return nil, errors.New("boom")
}

func makePC(name string) *providerconfig.ProviderConfig {
	return &providerconfig.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: providerconfig.ProviderConfigSpec{
			ProjectID:     "test-project",
			ProjectNumber: 123,
			NetworkConfig: providerconfig.ProviderNetworkConfig{
				Network:    "net-1",
				SubnetInfo: providerconfig.ProviderConfigSubnetInfo{Subnetwork: "sub-1"},
			},
		},
	}
}

func newManagerForTest(t *testing.T, svcNeg svcnegclient.Interface) (*ProviderConfigControllersManager, *providerconfigclientfake.Clientset) {
	t.Helper()
	kubeClient := fake.NewSimpleClientset()
	pcClient := providerconfigclientfake.NewSimpleClientset()
	informers := multiprojectinformers.NewInformerSet(kubeClient, svcNeg, networkclient.Interface(nil), nodetopologyclient.Interface(nil), metav1.Duration{})
	logger, _ := ktesting.NewTestContext(t)

	rootNamer := namer.NewNamer("clusteruid", "", logger)
	kubeSystemUID := types.UID("uid")
	gceCreator := multiprojectgce.NewGCEFake()
	lpCfg := labels.PodLabelPropagationConfig{}
	globalStop := make(chan struct{})
	t.Cleanup(func() { close(globalStop) })

	mgr := NewProviderConfigControllerManager(
		kubeClient,
		informers,
		pcClient,
		svcNeg,
		networkclient.Interface(nil),
		nodetopologyclient.Interface(nil),
		kubeClient, // event recorder
		kubeSystemUID,
		rootNamer,
		namer.NewL4Namer(string(kubeSystemUID), rootNamer),
		lpCfg,
		gceCreator,
		globalStop,
		logger,
	)
	return mgr, pcClient
}

func createProviderConfig(t *testing.T, pcClient *providerconfigclientfake.Clientset, name string) *providerconfig.ProviderConfig {
	t.Helper()
	pc := makePC(name)
	_, err := pcClient.CloudV1().ProviderConfigs().Create(context.Background(), pc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create pc: %v", err)
	}
	return pc
}

// TestStart_AddsFinalizer_AndIsIdempotent verifies that starting controllers
// for a ProviderConfig adds the NEG cleanup finalizer and that repeated starts
// are idempotent (the underlying controller is launched only once).
func TestStart_AddsFinalizer_AndIsIdempotent(t *testing.T) {
	var calls []startCall
	orig := startNEGController
	startNEGController = func(_ *multiprojectinformers.InformerSet, _ kubernetes.Interface, _ kubernetes.Interface, _ svcnegclient.Interface,
		_ networkclient.Interface, _ nodetopologyclient.Interface, _ types.UID, _ *namer.Namer, _ *namer.L4Namer,
		_ labels.PodLabelPropagationConfig, _ *cloudgce.Cloud, _ <-chan struct{}, _ klog.Logger, pc *providerconfig.ProviderConfig) (chan<- struct{}, error) {
		calls = append(calls, startCall{pcName: pc.Name})
		return make(chan struct{}), nil
	}
	t.Cleanup(func() { startNEGController = orig })

	mgr, pcClient := newManagerForTest(t, svcnegfake.NewSimpleClientset())
	pc := createProviderConfig(t, pcClient, "pc-1")

	err := mgr.StartControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("StartControllersForProviderConfig error: %v", err)
	}

	got, err := pcClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pc: %v", err)
	}
	if !slices.Contains(got.Finalizers, finalizer.ProviderConfigNEGCleanupFinalizer) {
		t.Fatalf("expected finalizer to be added, got %v", got.Finalizers)
	}

	err = mgr.StartControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("second start returned error: %v", err)
	}

	if len(calls) != 1 || calls[0].pcName != pc.Name {
		t.Fatalf("unexpected start calls: %#v", calls)
	}

}

// TestStart_FailureCleansFinalizer verifies that when starting controllers
// fails, the added finalizer is rolled back and removed from the object.
func TestStart_FailureCleansFinalizer(t *testing.T) {
	orig := startNEGController
	startNEGController = func(_ *multiprojectinformers.InformerSet, _ kubernetes.Interface, _ kubernetes.Interface, _ svcnegclient.Interface,
		_ networkclient.Interface, _ nodetopologyclient.Interface, _ types.UID, _ *namer.Namer, _ *namer.L4Namer,
		_ labels.PodLabelPropagationConfig, _ *cloudgce.Cloud, _ <-chan struct{}, _ klog.Logger, _ *providerconfig.ProviderConfig) (chan<- struct{}, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { startNEGController = orig })

	mgr, pcClient := newManagerForTest(t, svcnegfake.NewSimpleClientset())
	pc := createProviderConfig(t, pcClient, "pc-err")

	err := mgr.StartControllersForProviderConfig(pc)
	if err == nil {
		t.Fatalf("expected error from StartControllersForProviderConfig")
	}

	got, err := pcClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pc: %v", err)
	}
	if slices.Contains(got.Finalizers, finalizer.ProviderConfigNEGCleanupFinalizer) {
		t.Fatalf("expected finalizer removed on failure, got %v", got.Finalizers)
	}

}

// TestStart_GCEClientError_CleansFinalizer verifies that if the GCE client
// creation fails after adding the finalizer, the finalizer is rolled back so
// deletion is not blocked unnecessarily.
func TestStart_GCEClientError_CleansFinalizer(t *testing.T) {
	mgr, pcClient := newManagerForTest(t, svcnegfake.NewSimpleClientset())
	pc := createProviderConfig(t, pcClient, "pc-gce-err")

	// Inject failing GCE creator.
	mgr.gceCreator = failingGCECreator{}

	err := mgr.StartControllersForProviderConfig(pc)
	if err == nil {
		t.Fatalf("expected error from StartControllersForProviderConfig when GCEForProviderConfig fails")
	}

	got, err := pcClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pc: %v", err)
	}
	if slices.Contains(got.Finalizers, finalizer.ProviderConfigNEGCleanupFinalizer) {
		t.Fatalf("expected finalizer removed on GCE client failure, got %v", got.Finalizers)
	}
}

// TestStop_ClosesChannel_AndRemovesFinalizer verifies that stopping a
// ProviderConfig's controllers closes the stop channel and removes the
// cleanup finalizer from the ProviderConfig resource.
func TestStop_ClosesChannel_AndRemovesFinalizer(t *testing.T) {
	var ch chan struct{}
	orig := startNEGController
	startNEGController = func(_ *multiprojectinformers.InformerSet, _ kubernetes.Interface, _ kubernetes.Interface, _ svcnegclient.Interface,
		_ networkclient.Interface, _ nodetopologyclient.Interface, _ types.UID, _ *namer.Namer, _ *namer.L4Namer,
		_ labels.PodLabelPropagationConfig, _ *cloudgce.Cloud, _ <-chan struct{}, _ klog.Logger, _ *providerconfig.ProviderConfig) (chan<- struct{}, error) {
		ch = make(chan struct{})
		return ch, nil
	}
	t.Cleanup(func() { startNEGController = orig })

	mgr, pcClient := newManagerForTest(t, svcnegfake.NewSimpleClientset())
	pc := createProviderConfig(t, pcClient, "pc-stop")

	err := mgr.StartControllersForProviderConfig(pc)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	gotBefore, err := pcClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pc: %v", err)
	}
	if !slices.Contains(gotBefore.Finalizers, finalizer.ProviderConfigNEGCleanupFinalizer) {
		t.Fatalf("expected finalizer before stop")
	}

	mgr.StopControllersForProviderConfig(pc)

	// Channel should be closed (non-blocking read succeeds)
	select {
	case <-ch:
		// ok
	default:
		t.Fatalf("expected stop channel to be closed")
	}

	gotAfter, err := pcClient.CloudV1().ProviderConfigs().Get(context.Background(), pc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pc: %v", err)
	}
	if slices.Contains(gotAfter.Finalizers, finalizer.ProviderConfigNEGCleanupFinalizer) {
		t.Fatalf("expected finalizer removed after stop, got %v", gotAfter.Finalizers)
	}

	// Second stop should not panic
	mgr.StopControllersForProviderConfig(pc)

}

// TestConcurrentStart_IsSingleShot verifies that concurrent calls to start
// controllers for the same ProviderConfig result in a single start.
func TestConcurrentStart_IsSingleShot(t *testing.T) {
	var mu sync.Mutex
	var calls []startCall
	orig := startNEGController
	startNEGController = func(_ *multiprojectinformers.InformerSet, _ kubernetes.Interface, _ kubernetes.Interface, _ svcnegclient.Interface,
		_ networkclient.Interface, _ nodetopologyclient.Interface, _ types.UID, _ *namer.Namer, _ *namer.L4Namer,
		_ labels.PodLabelPropagationConfig, _ *cloudgce.Cloud, _ <-chan struct{}, _ klog.Logger, pc *providerconfig.ProviderConfig) (chan<- struct{}, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, startCall{pcName: pc.Name})
		return make(chan struct{}), nil
	}
	t.Cleanup(func() { startNEGController = orig })

	mgr, pcClient := newManagerForTest(t, svcnegfake.NewSimpleClientset())
	pc := createProviderConfig(t, pcClient, "pc-concurrent")

	var wg sync.WaitGroup
	const n = 10
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_ = mgr.StartControllersForProviderConfig(pc)
		}()
	}
	wg.Wait()

	if len(calls) != 1 {
		t.Fatalf("expected single start call, got %d", len(calls))
	}
}
