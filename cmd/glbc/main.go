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

	flag "github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/frontendconfig"
	"k8s.io/ingress-gce/pkg/ingparams"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/l4lb"
	"k8s.io/ingress-gce/pkg/psc"
	"k8s.io/ingress-gce/pkg/serviceattachment"
	"k8s.io/ingress-gce/pkg/servicemetrics"
	"k8s.io/ingress-gce/pkg/svcneg"
	"k8s.io/klog/v2"

	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	firewallcrclient "k8s.io/cloud-provider-gcp/crd/client/gcpfirewall/clientset/versioned"
	networkclient "k8s.io/cloud-provider-gcp/crd/client/network/clientset/versioned"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	ingparamsclient "k8s.io/ingress-gce/pkg/ingparams/client/clientset/versioned"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"

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

	klog.V(0).Infof("Starting GLBC image: %q, cluster name %q", version.Version, flags.F.ClusterName)
	klog.V(0).Infof("Latest commit hash: %q", version.GitCommit)
	for i, a := range os.Args {
		klog.V(0).Infof("argv[%d]: %q", i, a)
	}

	klog.V(2).Infof("Flags = %+v", flags.F)
	defer klog.Flush()
	// Create kube-config that uses protobufs to communicate with API server.
	kubeConfigForProtobuf, err := app.NewKubeConfigForProtobuf()
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
	kubeConfig, err := app.NewKubeConfig()
	if err != nil {
		klog.Fatalf("Failed to create kubernetes client config: %v", err)
	}

	var backendConfigClient backendconfigclient.Interface
	crdClient, err := crdclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes CRD client: %v", err)
	}
	// TODO(rramkumar): Reuse this CRD handler for other CRD's coming.
	crdHandler := crd.NewCRDHandler(crdClient)
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

	ingClassEnabled := flags.F.EnableIngressGAFields && app.IngressClassEnabled(kubeClient)
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

	namer, err := app.NewNamer(kubeClient, flags.F.ClusterName, firewalls.DefaultFirewallName)
	if err != nil {
		klog.Fatalf("app.NewNamer(ctx.KubeClient, %q, %q) = %v", flags.F.ClusterName, firewalls.DefaultFirewallName, err)
	}
	if namer.UID() != "" {
		klog.V(0).Infof("Cluster name: %+v", namer.UID())
	}

	// Get kube-system UID that will be used for v2 frontend naming scheme.
	kubeSystemNS, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), "kube-system", metav1.GetOptions{})
	if err != nil {
		klog.Fatalf("Error getting kube-system namespace: %v", err)
	}
	kubeSystemUID := kubeSystemNS.GetUID()

	cloud := app.NewGCEClient()
	defaultBackendServicePort := app.DefaultBackendServicePort(kubeClient)
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
	}
	ctx := ingctx.NewControllerContext(kubeConfig, kubeClient, backendConfigClient, frontendConfigClient, firewallCRClient, svcNegClient, ingParamsClient, svcAttachmentClient, networkClient, cloud, namer, kubeSystemUID, ctxConfig)
	go app.RunHTTPServer(ctx.HealthCheck)

	if !flags.F.LeaderElection.LeaderElect {
		option := runOption{
			stopCh:      make(chan struct{}),
			wg:          &sync.WaitGroup{},
			leaderElect: false,
		}
		ctx.Init()
		if flags.F.EnableNEGController {
			// ID is only used during leader election.
			runNEGController(ctx, "", option)
		}
		runControllers(ctx, option)
		return
	}

	electionConfig, err := makeLeaderElectionConfig(ctx, runOption{
		client:      leaderElectKubeClient,
		recorder:    ctx.Recorder(flags.F.LeaderElection.LockObjectNamespace),
		stopCh:      make(chan struct{}),
		wg:          &sync.WaitGroup{},
		leaderElect: true,
	})
	if err != nil {
		klog.Fatalf("%v", err)
	}
	leaderelection.RunOrDie(context.Background(), *electionConfig)
	klog.Warning("Ingress Controller exited.")
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
func makeLeaderElectionConfig(ctx *ingctx.ControllerContext, option runOption) (*leaderelection.LeaderElectionConfig, error) {
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
			runNEGController(ctx, id, option)
		}
		runControllers(ctx, option)
		klog.Info("Shutting down leader election")
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
				klog.Warning("lost master")
			},
		},
	}, nil
}

func runControllers(ctx *ingctx.ControllerContext, option runOption) {
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
		lbc := controller.NewLoadBalancerController(ctx, stopCh, klog.TODO())
		go runWithWg(lbc.Run, wg)
		klog.V(0).Infof("ingress controller started")

		if !flags.F.EnableFirewallCR && flags.F.DisableFWEnforcement {
			klog.Fatalf("We can only disable the ingress controller FW enforcement when enabling the FW CR")
		}
		fwc := firewalls.NewFirewallController(ctx, flags.F.NodePortRanges.Values(), flags.F.EnableFirewallCR, flags.F.DisableFWEnforcement, ctx.EnableIngressRegionalExternal, stopCh)
		go runWithWg(fwc.Run, wg)
		klog.V(0).Infof("firewall controller started")
	}

	if ctx.EnableASMConfigMap {
		ctx.ASMConfigController.RegisterInformer(ctx.ConfigMapInformer, func() {
			// We want to trigger a restart.
			closeStopCh()
		})
	}
	if flags.F.RunL4Controller {
		l4Controller := l4lb.NewILBController(ctx, stopCh)
		go runWithWg(l4Controller.Run, wg)
		klog.V(0).Infof("L4 controller started")
	}

	if flags.F.EnablePSC {
		pscController := psc.NewController(ctx, stopCh)
		go runWithWg(pscController.Run, wg)
		klog.V(0).Infof("PSC Controller started")
	}

	if flags.F.EnableServiceMetrics {
		metricsController := servicemetrics.NewController(ctx, flags.F.MetricsExportInterval, stopCh)
		go runWithWg(metricsController.Run, wg)
		klog.V(0).Infof("Service Metrics Controller started")
	}

	go app.RunSIGTERMHandler(closeStopCh)

	ctx.Start(stopCh)

	if flags.F.EnableIGController {
		igControllerParams := &instancegroups.ControllerConfig{
			NodeInformer: ctx.NodeInformer,
			IGManager:    ctx.InstancePool,
			HasSynced:    ctx.HasSynced,
			StopCh:       stopCh,
		}
		igController := instancegroups.NewController(igControllerParams)
		go runWithWg(igController.Run, wg)
	}

	// The L4NetLbController will be run when RbsMode flag is Set
	if flags.F.RunL4NetLBController {
		l4netlbController := l4lb.NewL4NetLBController(ctx, stopCh)

		go runWithWg(l4netlbController.Run, wg)
		klog.V(0).Infof("L4NetLB controller started")
	}
	// Keep the program running until TERM signal.
	<-stopCh
	klog.Warning("Shutdown has been triggered")

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		// Wait until all controllers are done with cleanup.
		klog.Info("Starting to wait for controller cleanup")
		wg.Wait()
		klog.Info("Finished waiting for controller cleanup")
	}()

	select {
	case <-doneCh:
		klog.Info("Finished cleanup for all controllers, shutting down")
		return
	case <-time.After(30 * time.Second):
		klog.Warning("Reached 30 seconds timeout limit, shutting down")
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
func runNEGController(ctx *ingctx.ControllerContext, id string, option runOption) {
	negController := createNEGController(ctx, option.stopCh)
	if !option.leaderElect {
		go runWithWg(negController.Run, option.wg)
		klog.V(0).Info("negController started")
		return
	}

	// If GateNEGByLock is false, we run NEG controller with other controllers.
	// In this case, NEG controller is controlled by the combined lock/ingress-gce-lock.
	if !flags.F.GateNEGByLock {
		go runWithWg(negController.Run, option.wg)
		klog.V(0).Info("negController started")
	}

	klog.Infof("Attempting to grab %s", negLockName)
	negRunFunc := func() {
		go collectLockAvailabilityMetrics(negLockName, flags.F.GKEClusterType, option.stopCh)
		// If GateNEGByLock is true, we run NEG controller with NEG leader election.
		// In this case, NEG controller is controlled by the ingres-gce-lock
		// and ingress-gce-neg-lock.
		if flags.F.GateNEGByLock {
			go runWithWg(negController.Run, option.wg)
			klog.V(0).Info("Gated: negController started")
		}
		<-option.stopCh
		klog.Info("Shutting down NEG leader election")
		option.wg.Wait()
		os.Exit(0)
	}

	negElectConfig, err := makeNEGLeaderElectionConfig(ctx, id, option, negRunFunc)
	if err != nil {
		klog.Errorf("makeNEGLeaderElectionConfig()=%v, want nil", err)
	}
	// Run in goroutine since RunOrDie is a blocking call.
	go leaderelection.RunOrDie(context.Background(), *negElectConfig)
}

func makeNEGLeaderElectionConfig(ctx *ingctx.ControllerContext, id string, option runOption, runNEGFunc func()) (*leaderelection.LeaderElectionConfig, error) {
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
				klog.Warning("Stop running NEG Leader election")
			},
		},
	}, nil
}

func createNEGController(ctx *ingctx.ControllerContext, stopCh <-chan struct{}) *neg.Controller {
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
			klog.Errorf("Failed tp retrieve pod label propagation config: %v", err)
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
		klog.TODO(), // TODO(#1761): Replace this with a top level logger configuration once one is available.
	)

	ctx.AddHealthCheck("neg-controller", negController.IsHealthy)
	return negController
}

// runWithWg is a convenience wrapper that do a wg.Add(1) and runs the given
// function with a deferred wg.Done()
func runWithWg(runFunc func(), wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()
	runFunc()
}

func collectLockAvailabilityMetrics(lockName, clusterType string, stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			klog.Infof("StopCh is closed. Stop collecting metrics for %s.", lockName)
			return
		case <-time.Tick(time.Second):
			app.PublishLockAvailabilityMetrics(lockName, clusterType)
			klog.Infof("Exported %s availability metrics.", lockName)
		}
	}
}
