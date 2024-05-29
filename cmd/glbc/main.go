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
	firewallcrclient "k8s.io/cloud-provider-gcp/crd/client/gcpfirewall/clientset/versioned"
	networkclient "k8s.io/cloud-provider-gcp/crd/client/network/clientset/versioned"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/frontendconfig"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/ingparams"
	ingparamsclient "k8s.io/ingress-gce/pkg/ingparams/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/l4lb"
	"k8s.io/ingress-gce/pkg/psc"
	"k8s.io/ingress-gce/pkg/serviceattachment"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/servicemetrics"
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

	ingClassEnabled := flags.F.EnableIngressGAFields && app.IngressClassEnabled(kubeClient, rootLogger)
	var ingParamsClient ingparamsclient.Interface
	if ingClassEnabled {
		ingParamsCRDMeta := ingparams.CRDMeta()
		if _, err := crdHandler.EnsureCRD(ingParamsCRDMeta, false); err != nil {
			klog.Fatalf("Failed to ensure GCPIngressParams CRD: %v", err)
		}

		if ingParamsClient, err = ingparamsclient.NewForConfig(kubeConfig); err != nil {
			klog.Fatalf("Failed to create GCPIngressParams client: %v", err)
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
	}
	ctx := ingctx.NewControllerContext(kubeConfig, kubeClient, backendConfigClient, frontendConfigClient, firewallCRClient, svcNegClient, ingParamsClient, svcAttachmentClient, networkClient, cloud, namer, kubeSystemUID, ctxConfig, rootLogger)
	go app.RunHTTPServer(ctx.HealthCheck, rootLogger)

	if !flags.F.LeaderElection.LeaderElect {
		option := runOption{
			stopCh:      make(chan struct{}),
			wg:          &sync.WaitGroup{},
			leaderElect: false,
		}
		ctx.Init()
		if flags.F.EnableNEGController {
			// ID is only used during leader election.
			runNEGController(ctx, "", option, rootLogger)
		}
		runControllers(ctx, option, rootLogger)
		return
	}

	electionConfig, err := makeLeaderElectionConfig(ctx, runOption{
		client:      leaderElectKubeClient,
		recorder:    ctx.Recorder(flags.F.LeaderElection.LockObjectNamespace),
		stopCh:      make(chan struct{}),
		wg:          &sync.WaitGroup{},
		leaderElect: true,
	}, rootLogger)
	if err != nil {
		klog.Fatalf("%v", err)
	}
	leaderelection.RunOrDie(context.Background(), *electionConfig)
	rootLogger.Info("Ingress Controller exited.")
}

type runOption struct {
	client      clientset.Interface
	recorder    record.EventRecorder
	stopCh      chan struct{}
	wg          *sync.WaitGroup
	leaderElect bool
}

// makeLeaderElectionConfig builds a leader election configuration. It will
// create a new resource lock associated with the configuration.
func makeLeaderElectionConfig(ctx *ingctx.ControllerContext, option runOption, logger klog.Logger) (*leaderelection.LeaderElectionConfig, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("unable to get hostname: %v", err)
	}
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := fmt.Sprintf("%v_%x", hostname, rand.Intn(1e6))
	rl, err := resourcelock.New(resourcelock.LeasesResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		flags.F.LeaderElection.LockObjectName,
		option.client.CoreV1(),
		option.client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: option.recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	run := func() {
		ctx.Init()
		if flags.F.EnableNEGController {
			runNEGController(ctx, id, option, logger)
		}
		runControllers(ctx, option, logger)
		logger.Info("Shutting down leader election")
		os.Exit(0)
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
				run()
			},
			OnStoppedLeading: func() {
				logger.Info("lost master")
			},
		},
	}, nil
}

func runControllers(ctx *ingctx.ControllerContext, option runOption, logger klog.Logger) {
	stopCh := option.stopCh
	wg := option.wg
	var once sync.Once
	// This ensures that stopCh is only closed once.
	// Right now, we have two callers.
	// One is triggered when the ASM configmap changes, and the other one is
	// triggered by the SIGTERM handler.
	closeStopCh := func() {
		once.Do(func() { close(stopCh) })
	}

	if flags.F.RunIngressController {
		lbc := controller.NewLoadBalancerController(ctx, stopCh, logger)
		runWithWg(lbc.Run, wg)
		logger.V(0).Info("ingress controller started")

		if !flags.F.EnableFirewallCR && flags.F.DisableFWEnforcement {
			klog.Fatalf("We can only disable the ingress controller FW enforcement when enabling the FW CR")
		}
		fwc := firewalls.NewFirewallController(ctx, flags.F.NodePortRanges.Values(), flags.F.EnableFirewallCR, flags.F.DisableFWEnforcement, ctx.EnableIngressRegionalExternal, stopCh, logger)
		runWithWg(fwc.Run, wg)
		logger.V(0).Info("firewall controller started")
	}

	if ctx.EnableASMConfigMap {
		ctx.ASMConfigController.RegisterInformer(ctx.ConfigMapInformer, func() {
			// We want to trigger a restart.
			closeStopCh()
		})
	}
	if flags.F.RunL4Controller {
		l4Controller := l4lb.NewILBController(ctx, stopCh, logger)
		runWithWg(l4Controller.Run, wg)
		logger.V(0).Info("L4 controller started")
	}

	if flags.F.EnablePSC {
		pscController := psc.NewController(ctx, stopCh, logger)
		runWithWg(pscController.Run, wg)
		logger.V(0).Info("PSC Controller started")
	}

	if flags.F.EnableServiceMetrics {
		metricsController := servicemetrics.NewController(ctx, flags.F.MetricsExportInterval, stopCh, logger)
		runWithWg(metricsController.Run, wg)
		logger.V(0).Info("Service Metrics Controller started")
	}

	go app.RunSIGTERMHandler(closeStopCh, logger)

	ctx.Start(stopCh)

	if flags.F.EnableIGController {
		igControllerParams := &instancegroups.ControllerConfig{
			NodeInformer:             ctx.NodeInformer,
			ZoneGetter:               ctx.ZoneGetter,
			IGManager:                ctx.InstancePool,
			HasSynced:                ctx.HasSynced,
			EnableMultiSubnetCluster: flags.F.EnableIGMultiSubnetCluster,
			StopCh:                   stopCh,
		}
		igController := instancegroups.NewController(igControllerParams, logger)
		runWithWg(igController.Run, wg)
	}

	// The L4NetLbController will be run when RbsMode flag is Set
	if flags.F.RunL4NetLBController {
		l4netlbController := l4lb.NewL4NetLBController(ctx, stopCh, logger)

		runWithWg(l4netlbController.Run, wg)
		logger.V(0).Info("L4NetLB controller started")
	}
	// Keep the program running until TERM signal.
	<-stopCh
	logger.Info("Shutdown has been triggered")

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		// Wait until all controllers are done with cleanup.
		logger.Info("Starting to wait for controller cleanup")
		wg.Wait()
		logger.Info("Finished waiting for controller cleanup")
	}()

	select {
	case <-doneCh:
		logger.Info("Finished cleanup for all controllers, shutting down")
		return
	case <-time.After(30 * time.Second):
		logger.Info("Reached 30 seconds timeout limit, shutting down")
		return
	}
}

// runNEGController needs to be a non-blocking because runController is the
// main thread.
// If leader election is disabled, we only run NEG controller.
// Otherwise, we do leader election based on ingress-gce-neg-lock, run NEG
// controller and collect availability metrics on the lock.
// If GateNEGByLock is true, NEG controller is run in the leader election.
// Otherwise, it is run with other controllers together.
func runNEGController(ctx *ingctx.ControllerContext, id string, option runOption, logger klog.Logger) {
	negController := createNEGController(ctx, option.stopCh, logger)
	if !option.leaderElect {
		runWithWg(negController.Run, option.wg)
		logger.V(0).Info("negController started")
		return
	}

	// If GateNEGByLock is false, we run NEG controller with other controllers.
	// In this case, NEG controller is controlled by the combined lock/ingress-gce-lock.
	if !flags.F.GateNEGByLock {
		runWithWg(negController.Run, option.wg)
		logger.V(0).Info("negController started")
	}

	lockLogger := logger.WithValues("lockName", negLockName)
	lockLogger.Info("Attempting to grab lock", "lockName", negLockName)
	negRunFunc := func() {
		go collectLockAvailabilityMetrics(negLockName, flags.F.GKEClusterType, option.stopCh, lockLogger)
		// If GateNEGByLock is true, we run NEG controller with NEG leader election.
		// In this case, NEG controller is controlled by the ingres-gce-lock
		// and ingress-gce-neg-lock.
		if flags.F.GateNEGByLock {
			runWithWg(negController.Run, option.wg)
			logger.V(0).Info("Gated: negController started")
		}
		<-option.stopCh
		logger.Info("Shutting down NEG leader election")
		option.wg.Wait()
		os.Exit(0)
	}

	negElectConfig, err := makeNEGLeaderElectionConfig(ctx, id, option, negRunFunc, logger)
	if err != nil {
		logger.Error(err, "makeNEGLeaderElectionConfig() has an error")
	}
	// Run in goroutine since RunOrDie is a blocking call.
	go leaderelection.RunOrDie(context.Background(), *negElectConfig)
}

func makeNEGLeaderElectionConfig(ctx *ingctx.ControllerContext, id string, option runOption, runNEGFunc func(), logger klog.Logger) (*leaderelection.LeaderElectionConfig, error) {
	negLock, err := resourcelock.New(resourcelock.LeasesResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		negLockName,
		option.client.CoreV1(),
		option.client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
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
				runNEGFunc()
			},
			OnStoppedLeading: func() {
				logger.Info("Stop running NEG Leader election")
			},
		},
	}, nil
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
	// TODO: Refactor NEG to use cloud mocks so ctx.Cloud can be referenced within NewController.
	negController := neg.NewController(
		ctx.KubeClient,
		ctx.SvcNegClient,
		ctx.KubeSystemUID,
		ctx.IngressInformer,
		ctx.ServiceInformer,
		ctx.PodInformer,
		ctx.NodeInformer,
		ctx.EndpointSliceInformer,
		ctx.SvcNegInformer,
		ctx.NetworkInformer,
		ctx.GKENetworkParamsInformer,
		ctx.HasSynced,
		ctx.L4Namer,
		ctx.DefaultBackendSvcPort,
		negtypes.NewAdapterWithRateLimitSpecs(ctx.Cloud, flags.F.GCERateLimit.Values()),
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
			lockLogger.Info("StopCh is closed. Stop collecting metrics for resource lock", "lockName", lockName)
			return
		case <-ticker.C:
			app.PublishLockAvailabilityMetrics(lockName, clusterType)
			lockLogger.Info("Exported resource lock availability metrics", "lockName", lockName)
		}
	}
}
