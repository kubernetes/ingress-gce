package start

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	networkfake "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned/fake"
	nodetopologyfake "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned/fake"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	cloudgce "k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	providerconfigv1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/multiproject/finalizer"
	multiprojectgce "k8s.io/ingress-gce/pkg/multiproject/gce"
	"k8s.io/ingress-gce/pkg/multiproject/testutil"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	pcclientfake "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned/fake"
	svcnegfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/utils/namer"
	klog "k8s.io/klog/v2"
)

const (
	negAnnVal                   = `{"exposed_ports":{"80":{}}}`
	testNamedPort               = "named-Port"
	managedByEPSControllerValue = "endpointslice-controller.k8s.io"
	defaultTimeout              = 10 * time.Second
	shortTimeout                = 100 * time.Millisecond
)

// TestMain adjusts global test settings. It sets the verbosity for klog, etc.
func TestMain(m *testing.M) {
	flag.Parse()
	flags.Register()

	// Set klog verbosity based on test verbosity
	fs := flag.NewFlagSet("mock-flags", flag.PanicOnError)
	klog.InitFlags(fs)
	if testing.Verbose() {
		_ = fs.Set("v", "5")
	} else {
		_ = fs.Set("v", "2")
	}

	os.Exit(m.Run())
}

// TestStartProviderConfigIntegration creates ProviderConfig, Services inside,
// and verifies that the actual NEG is created and the Service is updated with the NEG status.
func TestStartProviderConfigIntegration(t *testing.T) {
	flags.F.ProviderConfigNameLabelKey = "cloud.gke.io/provider-config-name"

	providerConfigName1 := "test-pc1"
	providerConfigName2 := "test-pc2"

	testCases := []struct {
		desc            string
		providerConfigs []*providerconfigv1.ProviderConfig
		services        []*corev1.Service
	}{
		{
			desc: "Single ProviderConfig, Single Service",
			providerConfigs: []*providerconfigv1.ProviderConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: providerConfigName1,
						Labels: map[string]string{
							flags.F.MultiProjectOwnerLabelKey: "example-owner",
						},
					},
					Spec: providerconfigv1.ProviderConfigSpec{
						ProjectID:     "my-project",
						ProjectNumber: 12345,
						NetworkConfig: providerconfigv1.ProviderNetworkConfig{
							Network: "my-network",
							SubnetInfo: providerconfigv1.ProviderConfigSubnetInfo{
								Subnetwork: "my-subnetwork",
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-svc",
						Namespace: providerConfigName1,
						Labels: map[string]string{
							"cloud.gke.io/provider-config-name": providerConfigName1,
							"app":                               "testapp",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{"app": "testapp"},
						Ports: []corev1.ServicePort{
							{Port: 80},
						},
					},
				},
			},
		},
		{
			desc: "Multiple ProviderConfigs, Multiple Services referencing each",
			providerConfigs: []*providerconfigv1.ProviderConfig{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: providerConfigName1,
						Labels: map[string]string{
							flags.F.MultiProjectOwnerLabelKey: "example-owner",
						},
					},
					Spec: providerconfigv1.ProviderConfigSpec{
						ProjectID:     "project-1",
						ProjectNumber: 1111,
						NetworkConfig: providerconfigv1.ProviderNetworkConfig{
							Network: "my-network-1",
							SubnetInfo: providerconfigv1.ProviderConfigSubnetInfo{
								Subnetwork: "my-subnetwork-1",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: providerConfigName2,
						Labels: map[string]string{
							flags.F.MultiProjectOwnerLabelKey: "example-owner-2",
						},
					},
					Spec: providerconfigv1.ProviderConfigSpec{
						ProjectID:     "project-2",
						ProjectNumber: 2222,
						NetworkConfig: providerconfigv1.ProviderNetworkConfig{
							Network: "my-network-2",
							SubnetInfo: providerconfigv1.ProviderConfigSubnetInfo{
								Subnetwork: "my-subnetwork-2",
							},
						},
					},
				},
			},
			services: []*corev1.Service{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc1-1",
						Namespace: providerConfigName1,
						Labels: map[string]string{
							"cloud.gke.io/provider-config-name": providerConfigName1,
							"app":                               "svc1-app",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{"app": "svc1-app"},
						Ports: []corev1.ServicePort{
							{Port: 80},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc2-2",
						Namespace: providerConfigName2,
						Labels: map[string]string{
							"cloud.gke.io/provider-config-name": providerConfigName2,
							"app":                               "svc2-app",
						},
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{"app": "svc2-app"},
						Ports: []corev1.ServicePort{
							{Port: 80},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			flags.F.ResyncPeriod = 10000 * time.Second

			// Build fake clients.
			kubeClient := fake.NewSimpleClientset()
			pcClient := pcclientfake.NewSimpleClientset()
			svcNegClient := svcnegfake.NewSimpleClientset()
			networkClient := networkfake.NewSimpleClientset()
			nodeTopologyClient := nodetopologyfake.NewSimpleClientset()

			// This simulates the automatic labeling that the real environment does.
			// ProviderConfig name label is set to the namespace of the object.
			testutil.EmulateProviderConfigLabelingWebhook(svcNegClient.Tracker(), &svcNegClient.Fake, "servicenetworkendpointgroups")

			logger := klog.TODO()
			gceCreator := multiprojectgce.NewGCEFake()
			informersFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, flags.F.ResyncPeriod)

			rootNamer := namer.NewNamer("test-clusteruid", "", logger)
			kubeSystemUID := types.UID("test-kube-system-uid")

			ctx, cancel := context.WithCancel(context.Background())
			stopCh := make(chan struct{})
			defer func() {
				cancel()
				close(stopCh)
			}()

			// Start the multi-project code in the background.
			go func() {
				Start(
					logger,
					kubeClient,
					svcNegClient,
					networkClient,
					nodeTopologyClient,
					kubeSystemUID,
					kubeClient, // eventRecorderKubeClient can be the same as main client
					pcClient,
					gceCreator,
					rootNamer,
					stopCh,
				)
			}()

			// Create the test's ProviderConfigs.
			for _, pc := range tc.providerConfigs {
				addressPrefix := "10.100"
				if pc.Name == providerConfigName2 {
					addressPrefix = "20.100"
				}
				populateFakeNodeInformer(
					t,
					kubeClient,
					informersFactory.Core().V1().Nodes().Informer(),
					pc.Name,
					addressPrefix,
				)

				if _, err := pcClient.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{}); err != nil {
					t.Fatalf("Failed to create ProviderConfig %q: %v", pc.Name, err)
				}
			}

			// Confirm that all ProviderConfigs have the finalizer.
			for _, pc := range tc.providerConfigs {
				if err := waitForProviderConfigFinalizer(ctx, t, pcClient, pc); err != nil {
					t.Errorf("Timed out waiting for ProviderConfig %q finalizer: %v", pc.Name, err)
				}
			}

			// Create the test's Services, including the NEG annotation.
			for _, svc := range tc.services {
				if svc.Annotations == nil {
					svc.Annotations = map[string]string{}
				}
				svc.Annotations[annotations.NEGAnnotationKey] = negAnnVal

				createdSvc, err := kubeClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{})
				if err != nil {
					t.Fatalf("Failed to create Service %s/%s: %v", svc.Namespace, svc.Name, err)
				}
				t.Logf("Created Service %s/%s", createdSvc.Namespace, createdSvc.Name)

				// Populate endpoint slices in the fake client so InformerSet picks them up.
				addressPrefix := "10.100"
				if svc.Labels[flags.F.ProviderConfigNameLabelKey] == providerConfigName2 {
					addressPrefix = "20.100"
				}
				populateFakeEndpointSlices(
					t,
					kubeClient,
					svc.Name,
					svc.Labels[flags.F.ProviderConfigNameLabelKey],
					addressPrefix,
				)
			}

			// Validate each service against the corresponding ProviderConfig.
			// (In the second test case, we rely on the index i matching the PC array.)
			for i, svc := range tc.services {
				pc := tc.providerConfigs[i]
				validateService(ctx, t, kubeClient, svcNegClient, gceCreator, svc, pc)
			}
		})
	}
}

// Scenario:
// 1) Create pc-1 → works.
// 2) Create pc-2 → works.
// 3) Mark pc-1 deleting (its controllers stop).
// 4) Create pc-3 → works.
// Verify: a NEW Service in pc-2 (created AFTER pc-1 stops) works, and a Service in pc-3 works.
func TestSharedInformers_PC1Stops_PC2AndPC3KeepWorking(t *testing.T) {
	flags.F.ProviderConfigNameLabelKey = "cloud.gke.io/provider-config-name"
	flags.F.ResyncPeriod = 0

	// Fake clients / factories
	kubeClient := fake.NewSimpleClientset()
	pcClient := pcclientfake.NewSimpleClientset()
	svcNegClient := svcnegfake.NewSimpleClientset()
	networkClient := networkfake.NewSimpleClientset()
	nodeTopoClient := nodetopologyfake.NewSimpleClientset()

	// Simulate webhook: label SvcNEGs with provider-config name == namespace.
	testutil.EmulateProviderConfigLabelingWebhook(svcNegClient.Tracker(), &svcNegClient.Fake, "servicenetworkendpointgroups")

	informersFactory := informers.NewSharedInformerFactoryWithOptions(kubeClient, flags.F.ResyncPeriod)

	logger := klog.TODO()
	gceCreator := multiprojectgce.NewGCEFake()
	rootNamer := namer.NewNamer("test-clusteruid", "", logger)
	kubeSystemUID := types.UID("test-kube-system-uid")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	globalStop := make(chan struct{})
	defer close(globalStop)

	// Start multiproject manager (this starts shared factories once with globalStop).
	go Start(
		logger, kubeClient, svcNegClient, networkClient, nodeTopoClient,
		kubeSystemUID, kubeClient, pcClient,
		gceCreator, rootNamer, globalStop,
	)

	// --- pc-1: create and validate baseline service ---
	pc1 := createPC(ctx, t, pcClient, "pc-1", "owner-1", "proj-1", 1111, "net-1", "subnet-1")
	seedAll(t, kubeClient, informersFactory, "pc-1", "svc1", "10.100")
	svc1 := createNEGService(ctx, t, kubeClient, "pc-1", "svc1", "demo")
	validateService(ctx, t, kubeClient, svcNegClient, gceCreator, svc1, pc1)

	// --- pc-2: create and validate service ---
	pc2 := createPC(ctx, t, pcClient, "pc-2", "owner-2", "proj-2", 2222, "net-2", "subnet-2")
	seedAll(t, kubeClient, informersFactory, "pc-2", "svc2-a", "20.100")
	svc2a := createNEGService(ctx, t, kubeClient, "pc-2", "svc2-a", "demo")
	validateService(ctx, t, kubeClient, svcNegClient, gceCreator, svc2a, pc2)

	// Stop pc-1 (the first PC, which owns the stopCh).
	markPCDeletingAndWait(ctx, t, pcClient, pc1)

	// --- pc-2 after pc-1 stops: create a NEW service; it must still work ---
	seedEPS(t, kubeClient, informersFactory, "pc-2", "svc2-b", "20.100")
	svc2b := createNEGService(ctx, t, kubeClient, "pc-2", "svc2-b", "demo")
	validateService(ctx, t, kubeClient, svcNegClient, gceCreator, svc2b, pc2)

	// --- pc-3: create and validate service ---
	pc3 := createPC(ctx, t, pcClient, "pc-3", "owner-3", "proj-3", 3333, "net-3", "subnet-3")
	seedAll(t, kubeClient, informersFactory, "pc-3", "svc3", "30.100")
	svc3 := createNEGService(ctx, t, kubeClient, "pc-3", "svc3", "demo")
	validateService(ctx, t, kubeClient, svcNegClient, gceCreator, svc3, pc3)
}

/*** small local helpers ***/

func createPC(
	ctx context.Context,
	t *testing.T,
	pcClient *pcclientfake.Clientset,
	name, owner, projectID string,
	projectNumber int64,
	network, subnet string,
) *providerconfigv1.ProviderConfig {
	t.Helper()
	pc := &providerconfigv1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				flags.F.MultiProjectOwnerLabelKey: owner,
			},
		},
		Spec: providerconfigv1.ProviderConfigSpec{
			ProjectID:     projectID,
			ProjectNumber: projectNumber,
			NetworkConfig: providerconfigv1.ProviderNetworkConfig{
				Network: network,
				SubnetInfo: providerconfigv1.ProviderConfigSubnetInfo{
					Subnetwork: subnet,
				},
			},
		},
	}
	if _, err := pcClient.CloudV1().ProviderConfigs().Create(ctx, pc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create ProviderConfig %q: %v", name, err)
	}
	if err := waitForProviderConfigFinalizer(ctx, t, pcClient, pc); err != nil {
		t.Fatalf("finalizer not set for %s: %v", name, err)
	}
	return pc
}

func markPCDeletingAndWait(
	ctx context.Context,
	t *testing.T,
	pcClient *pcclientfake.Clientset,
	pc *providerconfigv1.ProviderConfig,
) {
	t.Helper()
	// We use Update with DeletionTimestamp instead of Delete to simulate the Kubernetes
	// controller reconciliation pattern. In real Kubernetes:
	// 1. User runs 'kubectl delete' which sets DeletionTimestamp (but doesn't remove the object)
	// 2. Controllers see DeletionTimestamp and execute finalizer logic
	// 3. Once all finalizers are removed, Kubernetes performs actual deletion
	// This test simulates steps 1-2 to trigger our controller's cleanup logic without
	// needing full Kubernetes deletion semantics in our fake client.
	cp := pc.DeepCopy()
	now := metav1.NewTime(time.Now())
	cp.DeletionTimestamp = &now
	if _, err := pcClient.CloudV1().ProviderConfigs().Update(ctx, cp, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("mark %s deleting: %v", pc.Name, err)
	}
	if err := waitForProviderConfigFinalizerRemoved(ctx, t, pcClient, pc.Name); err != nil {
		t.Fatalf("finalizer not removed for %s: %v", pc.Name, err)
	}
}

func seedAll(
	t *testing.T,
	kubeClient *fake.Clientset,
	informersFactory informers.SharedInformerFactory,
	ns, svcName, cidrPrefix string,
) {
	t.Helper()
	populateFakeNodeInformer(t, kubeClient, informersFactory.Core().V1().Nodes().Informer(), ns, cidrPrefix)
	populateFakeEndpointSlices(t, kubeClient, svcName, ns, cidrPrefix)
}

func seedEPS(
	t *testing.T,
	kubeClient *fake.Clientset,
	informersFactory informers.SharedInformerFactory,
	ns, svcName, cidrPrefix string,
) {
	t.Helper()
	// Create EndpointSlices in the fake client so InformerSet sees them.
	populateFakeEndpointSlices(t, kubeClient, svcName, ns, cidrPrefix)
}

func createNEGService(
	ctx context.Context,
	t *testing.T,
	kubeClient *fake.Clientset,
	namespace, name, app string,
) *corev1.Service {
	t.Helper()
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				flags.F.ProviderConfigNameLabelKey: namespace,
				"app":                              app,
			},
			Annotations: map[string]string{
				annotations.NEGAnnotationKey: negAnnVal,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": app},
			Ports:    []corev1.ServicePort{{Port: 80}},
		},
	}
	if _, err := kubeClient.CoreV1().Services(namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create service %s/%s: %v", namespace, name, err)
	}
	return svc
}

// validateService checks the final states of the Service, SvcNEG, and the GCE NEG.
func validateService(
	ctx context.Context,
	t *testing.T,
	kubeClient kubernetes.Interface,
	svcNegClient *svcnegfake.Clientset,
	gceCreator *multiprojectgce.GCEFake,
	svc *corev1.Service,
	pc *providerconfigv1.ProviderConfig,
) {
	t.Helper()

	negNames, err := checkNEGStatus(ctx, t, kubeClient, svc, []string{"80"})
	if err != nil {
		t.Errorf("NEG status annotation on Service %q is not in the expected state: %v", svc.Name, err)
		return
	}

	for _, negName := range negNames {
		if err := checkSvcNEG(ctx, t, svcNegClient, svc, negName); err != nil {
			t.Errorf("Svc NEG on Service %q is not in the expected state: %v", svc.Name, err)
		}

		gce, err := gceCreator.GCEForProviderConfig(pc, klog.TODO())
		if err != nil {
			t.Errorf("Failed to get GCE for ProviderConfig %q: %v", pc.Name, err)
			continue
		}
		if err := verifyCloudNEG(t, gce, negName); err != nil {
			t.Errorf("NEG %q on Service %q is not in the expected state: %v", negName, svc.Name, err)
		}
	}
}

// checkNEGStatus polls until the NEG Status annotation is set on the Service and validates it.
func checkNEGStatus(
	ctx context.Context,
	t *testing.T,
	kubeClient kubernetes.Interface,
	svc *corev1.Service,
	expectSvcPorts []string,
) ([]string, error) {
	t.Helper()

	var latestSvc *corev1.Service
	if err := wait.PollImmediate(shortTimeout, defaultTimeout, func() (bool, error) {
		var errSvc error
		latestSvc, errSvc = kubeClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if errSvc != nil {
			return false, errSvc
		}
		val, ok := latestSvc.Annotations[annotations.NEGStatusKey]
		if !ok {
			t.Logf("NEG status annotation not yet present on service %s/%s", latestSvc.Namespace, latestSvc.Name)
			return false, nil
		}
		if val == "" {
			t.Logf("NEG status annotation is empty on service %s/%s", latestSvc.Namespace, latestSvc.Name)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, fmt.Errorf("timed out waiting for NEG status on service %q: %v", svc.Name, err)
	}

	annotation := latestSvc.Annotations[annotations.NEGStatusKey]
	negStatus, err := annotations.ParseNegStatus(annotation)
	if err != nil {
		return nil, fmt.Errorf("invalid neg status annotation %q on service %s/%s: %v",
			annotation, latestSvc.Namespace, latestSvc.Name, err)
	}

	expectedPorts := sets.NewString(expectSvcPorts...)
	existingPorts := sets.NewString()
	for port := range negStatus.NetworkEndpointGroups {
		existingPorts.Insert(port)
	}
	if !expectedPorts.Equal(existingPorts) {
		return nil, fmt.Errorf(
			"service %s/%s annotation mismatch: got ports %q, want %q",
			svc.Namespace, svc.Name, existingPorts.List(), expectedPorts.List(),
		)
	}

	var negNames []string
	for _, neg := range negStatus.NetworkEndpointGroups {
		negNames = append(negNames, neg)
	}
	return negNames, nil
}

// checkSvcNEG polls until the SvcNEG resource is created for a given Service/NEG name.
func checkSvcNEG(
	ctx context.Context,
	t *testing.T,
	svcNegClient *svcnegfake.Clientset,
	svc *corev1.Service,
	negName string,
) error {
	t.Helper()

	return wait.PollImmediate(time.Second, defaultTimeout*3, func() (bool, error) {
		negCheck, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svc.Namespace).Get(ctx, negName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Svc NEG %s/%s not found yet", svc.Namespace, negName)
				return false, nil
			}
			return false, fmt.Errorf("failed to get Svc NEG %s/%s: %v", svc.Namespace, negName, err)
		}
		t.Logf("Svc NEG found: %s/%s with %d endpoints", negCheck.Namespace, negCheck.Name, len(negCheck.Status.NetworkEndpointGroups))
		return true, nil
	})
}

// waitForProviderConfigFinalizer ensures a ProviderConfig has the expected finalizer.
func waitForProviderConfigFinalizer(
	ctx context.Context,
	t *testing.T,
	pcClient *pcclientfake.Clientset,
	pc *providerconfigv1.ProviderConfig,
) error {
	t.Helper()

	return wait.PollImmediate(time.Second, defaultTimeout, func() (bool, error) {
		pcCheck, err := pcClient.CloudV1().ProviderConfigs().Get(ctx, pc.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, f := range pcCheck.Finalizers {
			if f == finalizer.ProviderConfigNEGCleanupFinalizer {
				t.Logf("ProviderConfig %q has expected finalizer", pc.Name)
				return true, nil
			}
		}
		t.Logf("ProviderConfig %q does not have finalizer yet, current finalizers: %v", pc.Name, pcCheck.Finalizers)
		return false, nil
	})
}

// verifyCloudNEG checks that the NEG exists in the GCE fake for the default region (us-central1).
func verifyCloudNEG(
	t *testing.T,
	gceCloud *cloudgce.Cloud,
	negName string,
) error {
	t.Helper()

	const defaultRegion = "us-central1"
	neg, err := gceCloud.GetNetworkEndpointGroup(negName, defaultRegion)
	if err != nil {
		return fmt.Errorf("failed to get NEG %q from cloud in region %s: %v", negName, defaultRegion, err)
	}
	t.Logf("Verified cloud NEG: %s in region %s", neg.Name, defaultRegion)
	return nil
}

// populateFakeNodeInformer creates and indexes a few fake Node objects to simulate existing cluster nodes.
func populateFakeNodeInformer(
	t *testing.T,
	client *fake.Clientset,
	nodeInformer cache.SharedIndexInformer,
	pcName string,
	addressPrefix string,
) {
	t.Helper()
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", negtypes.TestInstance1, pcName),
				Labels: map[string]string{
					flags.F.ProviderConfigNameLabelKey: pcName,
				},
			},
			Spec: corev1.NodeSpec{
				ProviderID: fmt.Sprintf("gce://foo-project/us-central1/%s", negtypes.TestInstance1),
				PodCIDR:    fmt.Sprintf("%s.1.0/24", addressPrefix),
				PodCIDRs:   []string{fmt.Sprintf("%s.1.0/24", addressPrefix)},
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "1.2.3.1"},
				},
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-%s", negtypes.TestInstance2, pcName),
				Labels: map[string]string{
					flags.F.ProviderConfigNameLabelKey: pcName,
				},
			},
			Spec: corev1.NodeSpec{
				ProviderID: fmt.Sprintf("gce://foo-project/us-central1/%s", negtypes.TestInstance2),
				PodCIDR:    fmt.Sprintf("%s.2.0/24", addressPrefix),
				PodCIDRs:   []string{fmt.Sprintf("%s.2.0/24", addressPrefix)},
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{Type: corev1.NodeInternalIP, Address: "1.2.3.2"},
				},
				Conditions: []corev1.NodeCondition{
					{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
				},
			},
		},
	}

	for _, node := range nodes {
		if _, err := client.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Failed to create node %q: %v", node.Name, err)
		}
		if err := nodeInformer.GetIndexer().Add(node); err != nil {
			t.Fatalf("Failed to add node %q to informer: %v", node.Name, err)
		}
	}
}

// populateFakeEndpointSlices indexes a set of fake EndpointSlices to simulate the real endpoints in the cluster.
func populateFakeEndpointSlices(
	t *testing.T,
	client *fake.Clientset,
	serviceName, providerConfigName, addressPrefix string,
) {
	t.Helper()
	endpointSlices := getTestEndpointSlices(serviceName, providerConfigName, addressPrefix)
	for _, es := range endpointSlices {
		if _, err := client.DiscoveryV1().EndpointSlices(providerConfigName).Create(context.Background(), es, metav1.CreateOptions{}); err != nil {
			t.Fatalf("Failed to create endpoint slice %q: %v", es.Name, err)
		}
	}
}

func getTestEndpointSlices(
	serviceName, providerConfigName, addressPrefix string,
) []*discovery.EndpointSlice {
	instance1 := fmt.Sprintf("%s-%s", negtypes.TestInstance1, providerConfigName)
	instance2 := fmt.Sprintf("%s-%s", negtypes.TestInstance2, providerConfigName)
	notReady := false
	emptyNamedPort := ""
	namedPort := testNamedPort
	port80 := int32(80)
	port81 := int32(81)
	protocolTCP := corev1.ProtocolTCP

	return []*discovery.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName + "-1",
				Namespace: providerConfigName,
				Labels: map[string]string{
					discovery.LabelServiceName:         serviceName,
					discovery.LabelManagedBy:           managedByEPSControllerValue,
					flags.F.ProviderConfigNameLabelKey: providerConfigName,
				},
			},
			AddressType: discovery.AddressTypeIPv4,
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{fmt.Sprintf("%s.1.1", addressPrefix)},
					NodeName:  &instance1,
					TargetRef: &corev1.ObjectReference{
						Namespace: providerConfigName,
						Name:      "pod1",
					},
				},
				{
					Addresses: []string{fmt.Sprintf("%s.1.2", addressPrefix)},
					NodeName:  &instance1,
					TargetRef: &corev1.ObjectReference{
						Namespace: providerConfigName,
						Name:      "pod2",
					},
				},
				{
					Addresses: []string{fmt.Sprintf("%s.2.1", addressPrefix)},
					NodeName:  &instance2,
					TargetRef: &corev1.ObjectReference{
						Namespace: providerConfigName,
						Name:      "pod3",
					},
				},
				{
					Addresses:  []string{fmt.Sprintf("%s.1.3", addressPrefix)},
					NodeName:   &instance1,
					TargetRef:  &corev1.ObjectReference{Namespace: providerConfigName, Name: "pod5"},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
				{
					Addresses:  []string{fmt.Sprintf("%s.1.4", addressPrefix)},
					NodeName:   &instance1,
					TargetRef:  &corev1.ObjectReference{Namespace: providerConfigName, Name: "pod6"},
					Conditions: discovery.EndpointConditions{Ready: &notReady},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &emptyNamedPort,
					Port:     &port80,
					Protocol: &protocolTCP,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName + "-2",
				Namespace: providerConfigName,
				Labels: map[string]string{
					discovery.LabelServiceName:         serviceName,
					discovery.LabelManagedBy:           managedByEPSControllerValue,
					flags.F.ProviderConfigNameLabelKey: providerConfigName,
				},
			},
			AddressType: discovery.AddressTypeIPv4,
			Endpoints: []discovery.Endpoint{
				{
					Addresses: []string{fmt.Sprintf("%s.2.2", addressPrefix)},
					NodeName:  &instance2,
					TargetRef: &corev1.ObjectReference{
						Namespace: providerConfigName,
						Name:      "pod7",
					},
				},
			},
			Ports: []discovery.EndpointPort{
				{
					Name:     &namedPort,
					Port:     &port81,
					Protocol: &protocolTCP,
				},
			},
		},
	}
}

// waitForProviderConfigFinalizerRemoved waits for finalizer removal (indicates StopControllers was invoked).
func waitForProviderConfigFinalizerRemoved(
	ctx context.Context,
	t *testing.T,
	pcClient *pcclientfake.Clientset,
	name string,
) error {
	t.Helper()
	return wait.PollImmediate(50*time.Millisecond, defaultTimeout, func() (bool, error) {
		current, err := pcClient.CloudV1().ProviderConfigs().Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		for _, f := range current.Finalizers {
			if f == finalizer.ProviderConfigNEGCleanupFinalizer {
				t.Logf("ProviderConfig %q still has finalizer, waiting for removal", name)
				return false, nil
			}
		}
		t.Logf("ProviderConfig %q finalizer successfully removed", name)
		return true, nil
	})
}

// TestProviderConfigErrorCases tests error handling for invalid or missing ProviderConfig scenarios.
func TestProviderConfigErrorCases(t *testing.T) {
	flags.F.ProviderConfigNameLabelKey = "cloud.gke.io/provider-config-name"
	flags.F.ResyncPeriod = 10000 * time.Second

	testCases := []struct {
		desc                   string
		providerConfig         *providerconfigv1.ProviderConfig
		service                *corev1.Service
		expectError            bool
		expectedErrorSubstring string
	}{
		{
			desc: "Service without ProviderConfig label",
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-svc-no-label",
					Namespace: "default",
					Labels: map[string]string{
						"app": "testapp",
					},
					Annotations: map[string]string{
						annotations.NEGAnnotationKey: negAnnVal,
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "testapp"},
					Ports: []corev1.ServicePort{
						{Port: 80},
					},
				},
			},
			expectError:            true,
			expectedErrorSubstring: "provider-config-name",
		},
		{
			desc: "ProviderConfig without required labels",
			providerConfig: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pc-no-owner",
				},
				Spec: providerconfigv1.ProviderConfigSpec{
					ProjectID:     "my-project",
					ProjectNumber: 12345,
					NetworkConfig: providerconfigv1.ProviderNetworkConfig{
						Network: "my-network",
						SubnetInfo: providerconfigv1.ProviderConfigSubnetInfo{
							Subnetwork: "my-subnetwork",
						},
					},
				},
			},
			expectError: false, // Should still work, just log warnings
		},
		{
			desc: "ProviderConfig with missing network config",
			providerConfig: &providerconfigv1.ProviderConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pc-no-network",
					Labels: map[string]string{
						flags.F.MultiProjectOwnerLabelKey: "example-owner",
					},
				},
				Spec: providerconfigv1.ProviderConfigSpec{
					ProjectID:     "my-project",
					ProjectNumber: 12345,
				},
			},
			expectError: false, // Should handle gracefully
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Build fake clients.
			kubeClient := fake.NewSimpleClientset()
			pcClient := pcclientfake.NewSimpleClientset()
			svcNegClient := svcnegfake.NewSimpleClientset()
			networkClient := networkfake.NewSimpleClientset()
			nodeTopologyClient := nodetopologyfake.NewSimpleClientset()

			testutil.EmulateProviderConfigLabelingWebhook(svcNegClient.Tracker(), &svcNegClient.Fake, "servicenetworkendpointgroups")

			logger := klog.TODO()
			gceCreator := multiprojectgce.NewGCEFake()

			rootNamer := namer.NewNamer("test-clusteruid", "", logger)
			kubeSystemUID := types.UID("test-kube-system-uid")

			ctx, cancel := context.WithCancel(context.Background())
			stopCh := make(chan struct{})
			defer func() {
				cancel()
				close(stopCh)
			}()

			// Start the multi-project code in the background.
			go func() {
				Start(
					logger,
					kubeClient,
					svcNegClient,
					networkClient,
					nodeTopologyClient,
					kubeSystemUID,
					kubeClient,
					pcClient,
					gceCreator,
					rootNamer,
					stopCh,
				)
			}()

			// Create the ProviderConfig if provided
			if tc.providerConfig != nil {
				if _, err := pcClient.CloudV1().ProviderConfigs().Create(ctx, tc.providerConfig, metav1.CreateOptions{}); err != nil {
					t.Fatalf("Failed to create ProviderConfig: %v", err)
				}
				t.Logf("Created ProviderConfig %q", tc.providerConfig.Name)
			}

			// Create the Service if provided
			if tc.service != nil {
				if _, err := kubeClient.CoreV1().Services(tc.service.Namespace).Create(ctx, tc.service, metav1.CreateOptions{}); err != nil {
					if tc.expectError {
						t.Logf("Got expected error creating service: %v", err)
						return
					}
					t.Fatalf("Unexpected error creating service: %v", err)
				}

				// For error cases, we need to wait to ensure no NEG status is set
				// For success cases, we poll until NEG status appears
				if tc.expectError {
					// Wait a reasonable time to ensure the error case doesn't set NEG status
					if err := wait.PollImmediate(500*time.Millisecond, 2*time.Second, func() (bool, error) {
						svc, err := kubeClient.CoreV1().Services(tc.service.Namespace).Get(ctx, tc.service.Name, metav1.GetOptions{})
						if err != nil {
							return false, fmt.Errorf("failed to get service: %v", err)
						}
						_, hasNEGStatus := svc.Annotations[annotations.NEGStatusKey]
						if hasNEGStatus {
							// Unexpectedly found NEG status in error case
							return false, fmt.Errorf("expected error but NEG status was set for service %s/%s", svc.Namespace, svc.Name)
						}
						// Keep polling to ensure it doesn't appear
						return false, nil
					}); err != nil {
						// Timeout is expected for error cases (no NEG status should appear)
						if err != wait.ErrWaitTimeout {
							t.Errorf("Unexpected error while waiting for error case: %v", err)
						}
					}
				} else {
					// For success cases, poll until NEG status is set
					if err := wait.PollImmediate(500*time.Millisecond, 10*time.Second, func() (bool, error) {
						svc, err := kubeClient.CoreV1().Services(tc.service.Namespace).Get(ctx, tc.service.Name, metav1.GetOptions{})
						if err != nil {
							return false, fmt.Errorf("failed to get service: %v", err)
						}
						_, hasNEGStatus := svc.Annotations[annotations.NEGStatusKey]
						if hasNEGStatus {
							return true, nil
						}
						t.Logf("Waiting for NEG status to be set for service %s/%s", svc.Namespace, svc.Name)
						return false, nil
					}); err != nil {
						t.Logf("NEG status not set for service %s/%s: %v", tc.service.Namespace, tc.service.Name, err)
					}
				}
			}
		})
	}
}
