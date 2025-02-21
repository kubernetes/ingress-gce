package start

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

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
)

// TestMain adjusts global test settings. It sets the verbosity for klog, etc.
func TestMain(m *testing.M) {
	flag.Parse()

	// Set klog verbosity, for example to 5 for debugging output
	fs := flag.NewFlagSet("mock-flags", flag.PanicOnError)
	klog.InitFlags(fs)
	_ = fs.Set("v", "5")

	os.Exit(m.Run())
}

// TestStartProviderConfigIntegration creates ProviderConfig, Services inside,
// and verifies that the actual NEG is created and the Service is updated with the NEG status.
func TestStartProviderConfigIntegration(t *testing.T) {
	flags.Register()
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
						Name:      providerConfigName1,
						Namespace: providerConfigName1,
					},
					Spec: providerconfigv1.ProviderConfigSpec{
						ProjectID:     "my-project",
						ProjectNumber: 12345,
						NetworkConfig: &providerconfigv1.NetworkConfig{
							Network:           "my-network",
							DefaultSubnetwork: "my-subnetwork",
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
						Name:      providerConfigName1,
						Namespace: providerConfigName1,
					},
					Spec: providerconfigv1.ProviderConfigSpec{
						ProjectID:     "project-1",
						ProjectNumber: 1111,
						NetworkConfig: &providerconfigv1.NetworkConfig{
							Network:           "my-network-1",
							DefaultSubnetwork: "my-subnetwork-1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      providerConfigName2,
						Namespace: providerConfigName2,
					},
					Spec: providerconfigv1.ProviderConfigSpec{
						ProjectID:     "project-2",
						ProjectNumber: 2222,
						NetworkConfig: &providerconfigv1.NetworkConfig{
							Network:           "my-network-2",
							DefaultSubnetwork: "my-subnetwork-2",
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
					kubeSystemUID,
					kubeClient, // eventRecorderKubeClient can be the same as main client
					pcClient,
					informersFactory,
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
					kubeClient,
					informersFactory.Core().V1().Nodes().Informer(),
					pc.Name,
					addressPrefix,
				)

				if _, err := pcClient.CloudV1().ProviderConfigs(pc.Namespace).Create(ctx, pc, metav1.CreateOptions{}); err != nil {
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

				if _, err := kubeClient.CoreV1().Services(svc.Namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
					t.Fatalf("Failed to create Service %q: %v", svc.Name, err)
				}

				// Populate endpoint slices in the fake informer.
				addressPrefix := "10.100"
				if svc.Labels[flags.F.ProviderConfigNameLabelKey] == providerConfigName2 {
					addressPrefix = "20.100"
				}
				populateFakeEndpointSlices(
					informersFactory.Discovery().V1().EndpointSlices().Informer(),
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
	if err := wait.PollImmediate(time.Second, 10*time.Second, func() (bool, error) {
		var errSvc error
		latestSvc, errSvc = kubeClient.CoreV1().Services(svc.Namespace).Get(ctx, svc.Name, metav1.GetOptions{})
		if errSvc != nil {
			return false, errSvc
		}
		val, ok := latestSvc.Annotations[annotations.NEGStatusKey]
		return ok && val != "", nil
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

	return wait.PollImmediate(time.Second, 30*time.Second, func() (bool, error) {
		negCheck, err := svcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svc.Namespace).Get(ctx, negName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		t.Logf("Svc NEG found: %s/%s", negCheck.Namespace, negCheck.Name)
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

	return wait.PollImmediate(time.Second, 10*time.Second, func() (bool, error) {
		pcCheck, err := pcClient.CloudV1().ProviderConfigs(pc.Namespace).Get(ctx, pc.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, f := range pcCheck.Finalizers {
			if f == "multiproject.networking.gke.io/neg-cleanup" {
				t.Logf("ProviderConfig %q has expected finalizer", pc.Name)
				return true, nil
			}
		}
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

	neg, err := gceCloud.GetNetworkEndpointGroup(negName, "us-central1")
	if err != nil {
		return fmt.Errorf("failed to get NEG %q from cloud: %v", negName, err)
	}
	t.Logf("Verified cloud NEG: %s", neg.Name)
	return nil
}

// populateFakeNodeInformer creates and indexes a few fake Node objects to simulate existing cluster nodes.
func populateFakeNodeInformer(
	client *fake.Clientset,
	nodeInformer cache.SharedIndexInformer,
	pcName string,
	addressPrefix string,
) {
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
			klog.Warningf("Failed to create node %q: %v", node.Name, err)
			continue
		}
		if err := nodeInformer.GetIndexer().Add(node); err != nil {
			klog.Warningf("Failed to add node %q to informer: %v", node.Name, err)
		}
	}
}

// populateFakeEndpointSlices indexes a set of fake EndpointSlices to simulate the real endpoints in the cluster.
func populateFakeEndpointSlices(
	endpointSliceInformer cache.SharedIndexInformer,
	serviceName, providerConfigName, addressPrefix string,
) {
	endpointSlices := getTestEndpointSlices(serviceName, providerConfigName, addressPrefix)
	for _, es := range endpointSlices {
		if err := endpointSliceInformer.GetIndexer().Add(es); err != nil {
			klog.Warningf("Failed to add endpoint slice %q: %v", es.Name, err)
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
