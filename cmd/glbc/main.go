/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	firewallcrclient "github.com/GoogleCloudPlatform/gke-networking-api/client/gcpfirewall/clientset/versioned"
	networkclient "github.com/GoogleCloudPlatform/gke-networking-api/client/network/clientset/versioned"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	k8scp "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	flag "github.com/spf13/pflag"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/frontendconfig"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/l4lb"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/psc"
	"k8s.io/ingress-gce/pkg/serviceattachment"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/svcneg"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"

	"k8s.io/ingress-gce/cmd/glbc/app"
	"k8s.io/ingress-gce/pkg/backendconfig"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/ingress-gce/pkg/version"
)

const negLockName = "ingress-gce-neg-lock"

func main() {
	flags.Register()
	rand.Seed(time.Now().UTC().UnixNano())
	flag.Parse()
	flags.Validate()

	if flags.F.Version {
		fmt.Printf("Controller version: %s\n", version.Version)
		os.Exit(0)
	}

	rootLogger := klog.TODO()
	rootLogger.V(0).Info("Starting GLBC image", "version", version.Version, "clusterName", flags.F.ClusterName)
	rootLogger.V(0).Info(fmt.Sprintf("Latest commit hash: %q", version.GitCommit))
	for i, a := range os.Args {
		rootLogger.V(0).Info(fmt.Sprintf("argv[%d]: %q", i, a))
	}

	rootLogger.V(2).Info(fmt.Sprintf("Flags = %+v", flags.F))
	defer klog.Flush()
	// Create kube-config that uses protobufs to communicate with API server.
	kubeConfigForProtobuf, err := app.NewKubeConfigForProtobuf(rootLogger)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client config for protobuf: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfigForProtobuf)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client: %v", err)
	}

	// Due to scaling issues, leader election must be configured with a separate k8s client.
	leaderElectKubeClient, err := kubernetes.NewForConfig(restclient.AddUserAgent(kubeConfigForProtobuf, "leader-election"))
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client for leader election: %v", err)
	}

	// To ensure that events do not use up Kube QPS and burst, use a separate k8s client
	eventRecorderKubeClient, err := kubernetes.NewForConfig(restclient.AddUserAgent(kubeConfigForProtobuf, "l7lb-events"))
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client for event recording: %v", err)
	}

	// Create kube-config for CRDs.
	// TODO(smatti): Migrate to use protobuf once CRD supports.
	kubeConfig, err := app.NewKubeConfig(rootLogger)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client config: %v", err)
	}

	var backendConfigClient backendconfigclient.Interface
	crdClient, err := crdclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes CRD client: %v", err)
	}
	// TODO(rramkumar): Reuse this CRD handler for other CRD's coming.
	crdHandler := crd.NewCRDHandler(crdClient, rootLogger)
	backendConfigCRDMeta := backendconfig.CRDMeta()
	if _, err := crdHandler.EnsureCRD(backendConfigCRDMeta, true); err != nil {
		klog.Fatalf("Failed to ensure BackendConfig CRD: %v", err)
	}

	backendConfigClient, err = backendconfigclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create BackendConfig client: %v", err)
	}

	var frontendConfigClient frontendconfigclient.Interface
	if flags.F.EnableFrontendConfig {
		frontendConfigCRDMeta := frontendconfig.CRDMeta()
		if _, err := crdHandler.EnsureCRD(frontendConfigCRDMeta, true); err != nil {
			klog.Fatalf("Failed to ensure FrontendConfig CRD: %v", err)
		}

		frontendConfigClient, err = frontendconfigclient.NewForConfig(kubeConfig)
		if err != nil {
			klog.Fatalf("Failed to create FrontendConfig client: %v", err)
		}
	}

	var firewallCRClient firewallcrclient.Interface
	if flags.F.EnableFirewallCR {
		firewallCRClient, err = firewallcrclient.NewForConfig(kubeConfig)
		if err != nil {
			klog.Fatalf("Failed to create Firewall client: %v", err)
		}
	}

	if flags.F.EnableNEGController {
		negCRDMeta := svcneg.CRDMeta()
		if _, err := crdHandler.EnsureCRD(negCRDMeta, true); err != nil {
			klog.Fatalf("Failed to ensure ServiceNetworkEndpointGroup CRD: %v", err)
		}
	}
	svcNegClient, err := svcnegclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create NetworkEndpointGroup client: %v", err)
	}

	var svcAttachmentClient serviceattachmentclient.Interface
	if flags.F.EnablePSC {
		serviceAttachmentCRDMeta := serviceattachment.CRDMeta()
		if _, err := crdHandler.EnsureCRD(serviceAttachmentCRDMeta, true); err != nil {
			klog.Fatalf("Failed to ensure ServiceAttachment CRD: %v", err)
		}

		svcAttachmentClient, err = serviceattachmentclient.NewForConfig(kubeConfig)
		if err != nil {
			klog.Fatalf("Failed to create ServiceAttachment client: %v", err)
		}
	}

	var networkClient networkclient.Interface
	if flags.F.EnableMultiNetworking {
		networkClient, err = networkclient.NewForConfig(kubeConfig)
		if err != nil {
			klog.Fatalf("Failed to create Network client: %v", err)
		}
	}

	var nodeTopologyClient nodetopologyclient.Interface
	if flags.F.EnableMultiSubnetClusterPhase1 {
		nodeTopologyClient, err = nodetopologyclient.NewForConfig(kubeConfig)
		if err != nil {
			klog.Fatalf("Failed to create Node Topology Client: %v", err)
		}
	}

	namer, err := app.NewNamer(kubeClient, flags.F.ClusterName, firewalls.DefaultFirewallName, rootLogger)
	if err != nil {
		klog.Fatalf("app.NewNamer(ctx.KubeClient, %q, %q) = %v", flags.F.ClusterName, firewalls.DefaultFirewallName, err)
	}
	if namer.UID() != "" {
		rootLogger.V(0).Info(fmt.Sprintf("Cluster name: %+v", namer.UID()))
	}

	// Get kube-system UID that will be used for v2 frontend naming scheme.
	kubeSystemNS, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), "kube-system", metav1.GetOptions{})
	if err != nil {
		klog.Fatalf("Error getting kube-system namespace: %v", err)
	}
	kubeSystemUID := kubeSystemNS.GetUID()

	cloud := app.NewGCEClient(rootLogger)

	if flags.F.EnableMultiProjectMode {
		rootLogger.Info("Multi-project mode is enabled, starting project-syncer")
	}

	if flags.F.OverrideComputeAPIEndpoint != "" {
		// Globally set the domain for all urls generated by GoogleCloudPlatform/k8s-cloud-provider.
		// The cloud object is configured by the gce.conf file and parsed in app.NewGCEClient().
		// basePath will be of the form <URL>/v1
		domain := utils.GetDomainFromGABasePath(flags.F.OverrideComputeAPIEndpoint)
		rootLogger.Info("Overriding k8s-cloud-provider API Domain", "domain", domain)
		k8scp.SetAPIDomain(domain)
	}

	defaultBackendServicePort := app.DefaultBackendServicePort(kubeClient, rootLogger)
	ctxConfig := ingctx.ControllerContextConfig{
		Namespace:                     flags.F.WatchNamespace,
		ResyncPeriod:                  flags.F.ResyncPeriod,
		NumL4Workers:                  flags.F.NumL4Workers,
		NumL4NetLBWorkers:             flags.F.NumL4NetLBWorkers,
		DefaultBackendSvcPort:         defaultBackendServicePort,
		HealthCheckPath:               flags.F.HealthCheckPath,
		FrontendConfigEnabled:         flags.F.EnableFrontendConfig,
		EnableASMConfigMap:            flags.F.EnableASMConfigMapBasedConfig,
		ASMConfigMapNamespace:         flags.F.ASMConfigMapBasedConfigNamespace,
		ASMConfigMapName:              flags.F.ASMConfigMapBasedConfigCMName,
		MaxIGSize:                     flags.F.MaxIGSize,
		EnableL4ILBDualStack:          flags.F.EnableL4ILBDualStack,
		EnableL4NetLBDualStack:        flags.F.EnableL4NetLBDualStack,
		EnableL4StrongSessionAffinity: flags.F.EnableL4StrongSessionAffinity,
		EnableMultinetworking:         flags.F.EnableMultiNetworking,
		EnableIngressRegionalExternal: flags.F.EnableIngressRegionalExternal,
		EnableWeightedL4ILB:           flags.F.EnableWeightedL4ILB,
		EnableWeightedL4NetLB:         flags.F.EnableWeightedL4NetLB,
		EnableZonalAffinity:           flags.F.EnableZonalAffinity,
		DisableL4LBFirewall:           flags.F.DisableL4LBFirewall,
		EnableL4NetLBNEGs:             flags.F.EnableL4NetLBNEG,
		EnableL4NetLBNEGsDefault:      flags.F.EnableL4NetLBNEGDefault,
	}
	ctx := ingctx.NewControllerContext(kubeClient, backendConfigClient, frontendConfigClient, firewallCRClient, svcNegClient, svcAttachmentClient, networkClient, nodeTopologyClient, eventRecorderKubeClient, cloud, namer, kubeSystemUID, ctxConfig, rootLogger)
	go app.RunHTTPServer(ctx.HealthCheck, rootLogger)

	var once sync.Once
	// This ensures that stopCh is only closed once.
	// Right now, we have three callers.
	// One is triggered when the ASM configmap changes, and the other two are
	// triggered by the SIGTERM handler.
	stopCh := make(chan struct{})

	hostname, err := os.Hostname()
	if err != nil {
		klog.Fatalf("unable to get hostname: %v", err)
	}
	option := runOption{
		client:   leaderElectKubeClient,
		recorder: ctx.Recorder(flags.F.LeaderElection.LockObjectNamespace),
		wg:       &sync.WaitGroup{},
		stopCh:   stopCh,
		closeStopCh: func() {
			once.Do(func() { close(stopCh) })
		},
		// add a uniquifier so that two processes on the same host don't accidentally both become active
		id: fmt.Sprintf("%v_%x", hostname, rand.Intn(1e6)),
	}
	ctx.Init()

	enableOtherControllers := flags.F.RunIngressController || flags.F.RunL4Controller || flags.F.RunL4NetLBController || flags.F.EnableIGController || flags.F.EnablePSC
	runNEG := func() {
		logger := rootLogger.WithName("NEG Controller")
		logger.Info("Start running the enabled controllers",
			"NEG controller", flags.F.EnableNEGController,
		)
		runNEGController(ctx, option, logger)
	}
	runIngress := func() {
		logger := rootLogger.WithName("Other controllers")
		logger.Info("Start running the enabled controllers",
			"Ingress controller", flags.F.RunIngressController,
			"L4 controller", flags.F.RunL4Controller,
			"L4 NetLB controller", flags.F.RunL4NetLBController,
			"InstanceGroup controller", flags.F.EnableIGController,
			"PSC controller", flags.F.EnablePSC,
		)
		runControllers(ctx, option, logger)
	}

	if flags.F.LeaderElection.LeaderElect {
		runNEG = func() {
			logger := rootLogger.WithName("NEG Controller")
			logger.Info("Start running NEG leader election",
				"NEG controller", flags.F.EnableNEGController,
			)
			negElectionConfig, err := makeNEGLeaderElectionConfig(ctx, option, logger)
			if err != nil {
				klog.Fatalf("makeNEGLeaderElectionConfig()=%v, want nil", err)
			}
			leaderelection.RunOrDie(context.Background(), *negElectionConfig)
			logger.Info("NEG Controller exited.")
		}
		runIngress = func() {
			logger := rootLogger.WithName("Other controllers")
			logger.Info("Start running Ingress leader election",
				"Ingress controller", flags.F.RunIngressController,
				"L4 controller", flags.F.RunL4Controller,
				"L4 NetLB controller", flags.F.RunL4NetLBController,
				"InstanceGroup controller", flags.F.EnableIGController,
				"PSC controller", flags.F.EnablePSC,
			)
			electionConfig, err := makeLeaderElectionConfig(ctx, option, logger)
			if err != nil {
				klog.Fatalf("makeLeaderElectionConfig()=%v, want nil", err)
			}
			leaderelection.RunOrDie(context.Background(), *electionConfig)
		}
	}

	if flags.F.EnableNEGController {
		go runNEG()
	}
	if enableOtherControllers {
		go runIngress()
	}

	<-option.stopCh
	waitWithTimeout(option.wg, rootLogger)
}

type runOption struct {
	client      clientset.Interface
	recorder    record.EventRecorder
	stopCh      chan struct{}
	wg          *sync.WaitGroup
	closeStopCh func()
	id          string
}

// makeLeaderElectionConfig builds a leader election configuration. It will
// create a new resource lock associated with the configuration.
func makeLeaderElectionConfig(ctx *ingctx.ControllerContext, option runOption, logger klog.Logger) (*leaderelection.LeaderElectionConfig, error) {
	rl, err := resourcelock.New(resourcelock.LeasesResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		flags.F.LeaderElection.LockObjectName,
		option.client.CoreV1(),
		option.client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      option.id,
			EventRecorder: option.recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	return &leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: flags.F.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: flags.F.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   flags.F.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				// Since we are committing a suicide after losing
				// mastership, we can safely ignore the argument.
				runControllers(ctx, option, logger)
			},
			OnStoppedLeading: func() {
				logger.Info("lost master")
			},
		},
	}, nil
}

func runControllers(ctx *ingctx.ControllerContext, option runOption, logger klog.Logger) {
	if flags.F.RunIngressController {
		lbc := controller.NewLoadBalancerController(ctx, option.stopCh, logger)
		runWithWg(lbc.Run, option.wg)
		logger.V(0).Info("ingress controller started")

		if !flags.F.EnableFirewallCR && flags.F.DisableFWEnforcement {
			klog.Fatalf("We can only disable the ingress controller FW enforcement when enabling the FW CR")
		}
		fwc := firewalls.NewFirewallController(ctx, flags.F.NodePortRanges.Values(), flags.F.EnableFirewallCR, flags.F.DisableFWEnforcement, ctx.EnableIngressRegionalExternal, option.stopCh, logger)
		runWithWg(fwc.Run, option.wg)
		logger.V(0).Info("firewall controller started")
	}

	if flags.F.RunL4Controller {
		l4Controller := l4lb.NewILBController(ctx, option.stopCh, logger)
		runWithWg(l4Controller.Run, option.wg)
		logger.V(0).Info("L4 controller started")
	}

	if flags.F.EnablePSC {
		pscController := psc.NewController(ctx, option.stopCh, logger)
		runWithWg(pscController.Run, option.wg)
		logger.V(0).Info("PSC Controller started")
	}

	go app.RunSIGTERMHandler(option.closeStopCh, logger)

	ctx.Start(option.stopCh)

	if flags.F.EnableIGController {
		igControllerParams := &instancegroups.ControllerConfig{
			NodeInformer:             ctx.NodeInformer,
			ZoneGetter:               ctx.ZoneGetter,
			IGManager:                ctx.InstancePool,
			HasSynced:                ctx.HasSynced,
			EnableMultiSubnetCluster: flags.F.EnableIGMultiSubnetCluster,
			StopCh:                   option.stopCh,
		}
		igController := instancegroups.NewController(igControllerParams, logger)
		runWithWg(igController.Run, option.wg)
	}

	// The L4NetLbController will be run when RbsMode flag is Set
	if flags.F.RunL4NetLBController {
		l4netlbController := l4lb.NewL4NetLBController(ctx, option.stopCh, logger)

		runWithWg(l4netlbController.Run, option.wg)
		logger.V(0).Info("L4NetLB controller started")
	}
}

func makeNEGLeaderElectionConfig(ctx *ingctx.ControllerContext, option runOption, logger klog.Logger) (*leaderelection.LeaderElectionConfig, error) {
	negLock, err := resourcelock.New(resourcelock.LeasesResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		negLockName,
		option.client.CoreV1(),
		option.client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      option.id,
			EventRecorder: option.recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	return &leaderelection.LeaderElectionConfig{
		Lock:          negLock,
		LeaseDuration: flags.F.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: flags.F.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   flags.F.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				runNEGController(ctx, option, logger)
			},
			OnStoppedLeading: func() {
				logger.Info("Stop running NEG Leader election")
			},
		},
	}, nil
}

func runNEGController(ctx *ingctx.ControllerContext, option runOption, logger klog.Logger) {
	lockLogger := logger.WithValues("lockName", negLockName)
	lockLogger.Info("Attempting to grab lock", "lockName", negLockName)
	go collectLockAvailabilityMetrics(negLockName, flags.F.GKEClusterType, option.stopCh, logger)

	if ctx.EnableASMConfigMap {
		ctx.ASMConfigController.RegisterInformer(ctx.ConfigMapInformer, func() {
			// We want to trigger a restart.
			option.closeStopCh()
		})
	}

	if flags.F.EnableNEGController {
		negController := createNEGController(ctx, option.stopCh, logger)
		go runWithWg(negController.Run, option.wg)
		logger.V(0).Info("negController started")
	}

	go app.RunSIGTERMHandler(option.closeStopCh, logger)

	// TODO(sawsa307): Find a better approach to start informers.
	// If Ingress and NEG controller run together, since they share the same
	// informers, they will be started twice, but on the second call, Run()
	// will be skipped with the following warnings:
	//    The sharedIndexInformer has started, run more than once is not allowed
	ctx.Start(option.stopCh)
}

func createNEGController(ctx *ingctx.ControllerContext, stopCh <-chan struct{}, logger klog.Logger) *neg.Controller {
	zoneGetter := ctx.ZoneGetter

	// In NonGCP mode, use the zone specified in gce.conf directly.
	// This overrides the zone/fault-domain label on nodes for NEG controller.
	if flags.F.EnableNonGCPMode {
		zoneGetter = zonegetter.NewNonGCPZoneGetter(ctx.Cloud.LocalZone())
	}

	enableAsm := false
	asmServiceNEGSkipNamespaces := []string{}
	if ctx.EnableASMConfigMap {
		cmconfig := ctx.ASMConfigController.GetConfig()
		enableAsm = cmconfig.EnableASM
		asmServiceNEGSkipNamespaces = cmconfig.ASMServiceNEGSkipNamespaces
	}

	lpConfig := labels.PodLabelPropagationConfig{}
	if flags.F.EnableNEGLabelPropagation {
		lpConfigEnvVar := os.Getenv("LABEL_PROPAGATION_CONFIG")
		if err := json.Unmarshal([]byte(lpConfigEnvVar), &lpConfig); err != nil {
			logger.Error(err, "Failed to retrieve pod label propagation config")
		}
	}

	// The following adapter will use Network Selflink as Network Url instead of the NetworkUrl itself.
	// Network Selflink is always composed by the network name even if the cluster was initialized with Network Id.
	// All the components created from it will be consistent and always use the Url with network name and not the url with netowork Id
	adapter, err := network.NewAdapterNetworkSelfLink(ctx.Cloud)
	if err != nil {
		logger.Error(err, "Failed to create network adapter with SelfLink")
		// if it was not possible to retrieve network information use standard context as cloud network provider
		adapter = ctx.Cloud
	}

	// TODO: Refactor NEG to use cloud mocks so ctx.Cloud can be referenced within NewController.
	negController := neg.NewController(
		ctx.KubeClient,
		ctx.SvcNegClient,
		ctx.EventRecorderClient,
		ctx.KubeSystemUID,
		ctx.IngressInformer,
		ctx.ServiceInformer,
		ctx.PodInformer,
		ctx.NodeInformer,
		ctx.EndpointSliceInformer,
		ctx.SvcNegInformer,
		ctx.NetworkInformer,
		ctx.GKENetworkParamsInformer,
		ctx.NodeTopologyInformer,
		ctx.HasSynced,
		ctx.L4Namer,
		ctx.DefaultBackendSvcPort,
		negtypes.NewAdapterWithRateLimitSpecs(ctx.Cloud, flags.F.GCERateLimit.Values(), adapter),
		zoneGetter,
		ctx.ClusterNamer,
		flags.F.ResyncPeriod,
		flags.F.NegGCPeriod,
		flags.F.NumNegGCWorkers,
		flags.F.EnableReadinessReflector,
		flags.F.EnableL4NEG,
		flags.F.EnableNonGCPMode,
		flags.F.EnableDualStackNEG,
		enableAsm,
		asmServiceNEGSkipNamespaces,
		lpConfig,
		flags.F.EnableMultiNetworking,
		ctx.EnableIngressRegionalExternal,
		flags.F.EnableL4NetLBNEG,
		stopCh,
		logger,
	)

	ctx.AddHealthCheck("neg-controller", negController.IsHealthy)
	return negController
}

// runWithWg is a convenience wrapper that do a wg.Add(1), and runs the given
// function in a goroutine with a deferred wg.Done().
// We need to make sure wg.Add(1) when the counter is zero is executed before
// wg.Wait().
func runWithWg(runFunc func(), wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		runFunc()
	}()
}

func collectLockAvailabilityMetrics(lockName, clusterType string, stopCh <-chan struct{}, lockLogger klog.Logger) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			lockLogger.Info("StopCh is closed. Stop collecting metrics for resource lock")
			return
		case <-ticker.C:
			app.PublishLockAvailabilityMetrics(lockName, clusterType)
			lockLogger.Info("Exported resource lock availability metrics")
		}
	}
}

func waitWithTimeout(wg *sync.WaitGroup, logger klog.Logger) {
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		// Wait until all controllers are done with cleanup.
		logger.Info("Starting to wait for cleanup")
		wg.Wait()
		logger.Info("Finished waiting for cleanup")
	}()

	select {
	case <-doneCh:
		logger.Info("Finished cleanup, shutting down")
		return
	case <-time.After(30 * time.Second):
		logger.Info("Reached 30 seconds timeout limit for cleanup, shutting down")
		return
	}
}
