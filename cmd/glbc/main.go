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
	informernetwork "github.com/GoogleCloudPlatform/gke-networking-api/client/network/informers/externalversions"
	nodetopologyclient "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/clientset/versioned"
	informernodetopology "github.com/GoogleCloudPlatform/gke-networking-api/client/nodetopology/informers/externalversions"
	k8scp "github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	flag "github.com/spf13/pflag"
	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	informers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/frontendconfig"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/instancegroups"
	"k8s.io/ingress-gce/pkg/l4lb"
	multiprojectgce "k8s.io/ingress-gce/pkg/multiproject/gce"
	multiprojectstart "k8s.io/ingress-gce/pkg/multiproject/start"
	"k8s.io/ingress-gce/pkg/network"
	providerconfigclient "k8s.io/ingress-gce/pkg/providerconfig/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/psc"
	"k8s.io/ingress-gce/pkg/serviceattachment"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	"k8s.io/ingress-gce/pkg/svcneg"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"
	informersvcneg "k8s.io/ingress-gce/pkg/svcneg/client/informers/externalversions"
	"k8s.io/ingress-gce/pkg/systemhealth"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	"k8s.io/ingress-gce/cmd/glbc/app"
	"k8s.io/ingress-gce/pkg/backendconfig"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	"k8s.io/ingress-gce/pkg/neg"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	syncMetrics "k8s.io/ingress-gce/pkg/neg/metrics/metricscollector"
	"k8s.io/ingress-gce/pkg/neg/syncers/labels"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/ingress-gce/pkg/version"
)

const negLockName = "ingress-gce-neg-lock"
const l4LockName = "l4-lb-controller-gce-lock"

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

	crdClient, err := crdclient.NewForConfig(kubeConfig)
	if err != nil {
		klog.Fatalf("Failed to create kubernetes CRD client: %v", err)
	}
	// TODO(rramkumar): Reuse this CRD handler for other CRD's coming.
	crdHandler := crd.NewCRDHandler(crdClient, rootLogger)
	var frontendConfigClient frontendconfigclient.Interface
	var backendConfigClient backendconfigclient.Interface
	if flags.F.RunIngressController {
		backendConfigCRDMeta := backendconfig.CRDMeta()
		if _, err := crdHandler.EnsureCRD(backendConfigCRDMeta, true); err != nil {
			klog.Fatalf("Failed to ensure BackendConfig CRD: %v", err)
		}

		backendConfigClient, err = backendconfigclient.NewForConfig(kubeConfig)
		if err != nil {
			klog.Fatalf("Failed to create BackendConfig client: %v", err)
		}

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

	// register NEG prometheus metrics
	metrics.RegisterMetrics()
	syncMetrics.RegisterMetrics()

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

	var once sync.Once
	stopCh := make(chan struct{})

	rOption := runOption{
		wg:     &sync.WaitGroup{},
		stopCh: stopCh,
		// This ensures that stopCh is only closed once.
		closeStopCh: func() {
			once.Do(func() { close(stopCh) })
		},
	}
	go app.RunSIGTERMHandler(rOption.closeStopCh, rootLogger)

	systemHealth := systemhealth.NewSystemHealth(rootLogger)
	go app.RunHTTPServer(systemHealth.HealthCheck, rootLogger)

	hostname, err := os.Hostname()
	if err != nil {
		klog.Fatalf("unable to get hostname: %v", err)
	}

	if flags.F.EnableMultiProjectMode {
		rootLogger.Info("Multi-project mode is enabled, starting project-syncer")

		runWithWg(func() {
			gceCreator, err := multiprojectgce.NewDefaultGCECreator(rootLogger)
			if err != nil {
				klog.Fatalf("Failed to create GCE creator: %v", err)
			}
			providerConfigClient, err := providerconfigclient.NewForConfig(kubeConfig)
			if err != nil {
				klog.Fatalf("Failed to create ProviderConfig client: %v", err)
			}
			informersFactory := informers.NewSharedInformerFactory(kubeClient, flags.F.ResyncPeriod)
			var svcNegFactory informersvcneg.SharedInformerFactory
			if svcNegClient != nil {
				svcNegFactory = informersvcneg.NewSharedInformerFactory(svcNegClient, flags.F.ResyncPeriod)
			}
			var networkFactory informernetwork.SharedInformerFactory
			if networkClient != nil {
				networkFactory = informernetwork.NewSharedInformerFactory(networkClient, flags.F.ResyncPeriod)
			}
			var nodeTopologyFactory informernodetopology.SharedInformerFactory
			if nodeTopologyClient != nil {
				nodeTopologyFactory = informernodetopology.NewSharedInformerFactory(nodeTopologyClient, flags.F.ResyncPeriod)
			}
			ctx := context.Background()
			syncerMetrics := syncMetrics.NewNegMetricsCollector(flags.F.NegMetricsExportInterval, rootLogger)
			go syncerMetrics.Run(stopCh)

			if flags.F.LeaderElection.LeaderElect {
				err := multiprojectstart.StartWithLeaderElection(
					ctx,
					leaderElectKubeClient,
					hostname,
					rootLogger,
					kubeClient,
					svcNegClient,
					kubeSystemUID,
					eventRecorderKubeClient,
					providerConfigClient,
					informersFactory,
					svcNegFactory,
					networkFactory,
					nodeTopologyFactory,
					gceCreator,
					namer,
					stopCh,
					syncerMetrics,
				)
				if err != nil {
					rootLogger.Error(err, "Failed to start multi-project syncer with leader election")
				}
				rOption.closeStopCh()
			} else {
				multiprojectstart.Start(
					rootLogger,
					kubeClient,
					svcNegClient,
					kubeSystemUID,
					eventRecorderKubeClient,
					providerConfigClient,
					informersFactory,
					svcNegFactory,
					networkFactory,
					nodeTopologyFactory,
					gceCreator,
					namer,
					stopCh,
					syncerMetrics,
				)
			}
		}, rOption.wg)

		// Wait for the multi-project syncer to finish.
		<-rOption.stopCh
		waitWithTimeout(rOption.wg, rootLogger)

		// Since we only want multi-project mode functionality, exit here
		return
	}

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
		Namespace:                                 flags.F.WatchNamespace,
		ResyncPeriod:                              flags.F.ResyncPeriod,
		NumL4Workers:                              flags.F.NumL4Workers,
		NumL4NetLBWorkers:                         flags.F.NumL4NetLBWorkers,
		DefaultBackendSvcPort:                     defaultBackendServicePort,
		HealthCheckPath:                           flags.F.HealthCheckPath,
		MaxIGSize:                                 flags.F.MaxIGSize,
		RunL4ILBController:                        flags.F.RunL4Controller,
		RunL4NetLBController:                      flags.F.RunL4NetLBController,
		EnableL4ILBDualStack:                      flags.F.EnableL4ILBDualStack,
		EnableL4NetLBDualStack:                    flags.F.EnableL4NetLBDualStack,
		EnableL4StrongSessionAffinity:             flags.F.EnableL4StrongSessionAffinity,
		EnableMultinetworking:                     flags.F.EnableMultiNetworking,
		EnableIngressRegionalExternal:             flags.F.EnableIngressRegionalExternal,
		EnableWeightedL4ILB:                       flags.F.EnableWeightedL4ILB,
		EnableWeightedL4NetLB:                     flags.F.EnableWeightedL4NetLB,
		DisableL4LBFirewall:                       flags.F.DisableL4LBFirewall,
		EnableL4NetLBNEGs:                         flags.F.EnableL4NetLBNEG,
		EnableL4NetLBNEGsDefault:                  flags.F.EnableL4NetLBNEGDefault,
		EnableL4ILBMixedProtocol:                  flags.F.EnableL4ILBMixedProtocol,
		EnableL4NetLBMixedProtocol:                flags.F.EnableL4NetLBMixedProtocol,
		EnableL4ILBZonalAffinity:                  flags.F.EnableL4ILBZonalAffinity,
		EnableL4NetLBForwardingRulesOptimizations: flags.F.EnableL4NetLBForwardingRulesOptimizations,
		ReadOnlyMode:                              flags.F.ReadOnlyMode,
		EnableL4LBConditions:                      flags.F.EnableL4LBConditions,
	}
	ctx, err := ingctx.NewControllerContext(kubeClient, backendConfigClient, frontendConfigClient, firewallCRClient, svcNegClient, svcAttachmentClient, networkClient, nodeTopologyClient, eventRecorderKubeClient, cloud, namer, kubeSystemUID, ctxConfig, rootLogger)
	if err != nil {
		klog.Fatalf("unable to set up controller context: %v", err)
	}

	leOption := leaderElectionOption{
		client:   leaderElectKubeClient,
		recorder: ctx.Recorder(flags.F.LeaderElection.LockObjectNamespace),
		// add a uniquifier so that two processes on the same host don't accidentally both become active
		id: fmt.Sprintf("%v_%x", hostname, rand.Intn(1e6)),
	}

	enableL4Controllers := flags.F.RunL4Controller || flags.F.RunL4NetLBController || flags.F.EnableIGController || flags.F.EnablePSC
	runNEG := func() {
		logger := rootLogger.WithName("NEGController")
		logger.Info("Start running the enabled controllers",
			"NEG controller", flags.F.EnableNEGController,
		)
		err := runNEGController(ctx, systemHealth, rOption, logger)
		if err != nil {
			klog.Fatalf("failed to run NEG controller: %s", err)
		}
	}
	runIngress := func() {
		logger := rootLogger.WithName("Ingress controller")
		logger.Info("Start running the enabled controllers",
			"Ingress controller", flags.F.RunIngressController,
		)
		runIngressControllers(ctx, systemHealth, rOption, leOption, logger)
	}

	runL4 := func() {
		logger := rootLogger.WithName("L4 controllers")
		logger.Info("Start running the enabled controllers",
			"L4 controller", flags.F.RunL4Controller,
			"L4 NetLB controller", flags.F.RunL4NetLBController,
			"InstanceGroup controller", flags.F.EnableIGController,
			"PSC controller", flags.F.EnablePSC,
		)
		runL4Controllers(ctx, systemHealth, rOption, leOption, logger)
	}

	if flags.F.LeaderElection.LeaderElect {
		runNEG = func() {
			logger := rootLogger.WithName("NEGController")
			logger.Info("Start running NEG leader election",
				"NEG controller", flags.F.EnableNEGController,
			)
			negRunner, err := makeNEGRunnerWithLeaderElection(ctx, systemHealth, rOption, leOption, logger)
			if err != nil {
				klog.Fatalf("makeNEGLeaderElectionConfig()=%v, want nil", err)
			}
			// Use a cancelable context so the lease is released on shutdown.
			leCtx, cancel := context.WithCancel(context.Background())
			go func() {
				<-rOption.stopCh
				cancel()
			}()
			leaderelection.RunOrDie(leCtx, *negRunner)
			logger.Info("NEG Controller exited.")
		}
		runIngress = func() {
			logger := rootLogger.WithName("Ingress controller")
			logger.Info("Start running Ingress leader election",
				"Ingress controller", flags.F.RunIngressController,
			)
			ingressRunner, err := makeIngressRunnerWithLeaderElection(ctx, systemHealth, rOption, leOption, logger)
			if err != nil {
				klog.Fatalf("makeLeaderElectionConfig()=%v, want nil", err)
			}
			// Use a cancelable context so the lease is released on shutdown.
			leCtx, cancel := context.WithCancel(context.Background())
			go func() {
				<-rOption.stopCh
				cancel()
			}()
			leaderelection.RunOrDie(leCtx, *ingressRunner)
		}
		if !flags.F.GateL4ByLock {
			klog.Fatalf("--gate-l4-by-lock must be true when --leader-elect=true")
		}
		runL4 = func() {
			logger := rootLogger.WithName("L4 controller")
			logger.Info("Start running L4 leader election",
				"L4 controller", flags.F.RunL4Controller,
				"L4 NetLB controller", flags.F.RunL4NetLBController,
				"InstanceGroup controller", flags.F.EnableIGController,
				"PSC controller", flags.F.EnablePSC,
			)
			l4Runner, err := makeL4RunnerWithLeaderElection(ctx, systemHealth, rOption, leOption, logger)
			if err != nil {
				klog.Fatalf("makeLeaderElectionConfig()=%v, want nil", err)
			}
			// Use a cancelable context so the lease is released on shutdown.
			leCtx, cancel := context.WithCancel(context.Background())
			go func() {
				<-rOption.stopCh
				cancel()
			}()
			leaderelection.RunOrDie(leCtx, *l4Runner)
		}

	}

	if flags.F.EnableNEGController {
		go runNEG()
	}
	if flags.F.RunIngressController {
		go runIngress()
	}
	if enableL4Controllers {
		go runL4()
	}

	<-rOption.stopCh
	waitWithTimeout(rOption.wg, rootLogger)
}

type runOption struct {
	stopCh      chan struct{}
	wg          *sync.WaitGroup
	closeStopCh func()
}

type leaderElectionOption struct {
	// client is the Kubernetes client used for creating and removing resource locks,
	// facilitating ownership of the resource for leader election.
	client kubernetes.Interface
	// recorder is used to record events (e.g., leader election transitions)
	// in the Kubernetes cluster.
	recorder record.EventRecorder
	// id is the unique identifier for this particular leader election instance,
	// distinguishing it from other processes that might be competing for leadership.
	id string
}

func makeNEGRunnerWithLeaderElection(
	ctx *ingctx.ControllerContext,
	systemHealth *systemhealth.SystemHealth,
	runOption runOption,
	leOption leaderElectionOption,
	logger klog.Logger,
) (*leaderelection.LeaderElectionConfig, error) {
	return makeRunnerWithLeaderElection(
		leOption,
		negLockName,
		func(context.Context) {
			err := runNEGController(ctx, systemHealth, runOption, logger)
			if err != nil {
				klog.Fatalf("failed to run NEG controller: %s", err)
			}
		},
		func() {
			// When leadership is lost, initiate a graceful shutdown of all controllers.
			logger.Info("NEG controller: Leadership lost, initiating process-wide shutdown")
			runOption.closeStopCh()
		},
	)
}

func makeIngressRunnerWithLeaderElection(
	ctx *ingctx.ControllerContext,
	systemHealth *systemhealth.SystemHealth,
	runOption runOption,
	leOption leaderElectionOption,
	logger klog.Logger,
) (*leaderelection.LeaderElectionConfig, error) {
	return makeRunnerWithLeaderElection(
		leOption,
		flags.F.LeaderElection.LockObjectName,
		func(context.Context) {
			runIngressControllers(ctx, systemHealth, runOption, leOption, logger)
		},
		func() {
			// When leadership is lost, initiate a graceful shutdown of all controllers.
			logger.Info("Ingress controller: Leadership lost, initiating process-wide shutdown")
			runOption.closeStopCh()
		},
	)
}

func makeL4RunnerWithLeaderElection(
	ctx *ingctx.ControllerContext,
	systemHealth *systemhealth.SystemHealth,
	runOption runOption,
	leOption leaderElectionOption,
	logger klog.Logger,
) (*leaderelection.LeaderElectionConfig, error) {
	lockLogger := logger.WithValues("lock", l4LockName)
	return makeRunnerWithLeaderElection(
		leOption,
		l4LockName,
		func(context.Context) {
			lockLogger.V(0).Info("Acquired L4 Leader election lock")
			runL4Controllers(ctx, systemHealth, runOption, leOption, logger)
		},
		func() {
			// When leadership is lost for L4 gate, initiate a graceful shutdown
			lockLogger.Info("L4 controller: Leadership lost, initiating process-wide shutdown")
			runOption.closeStopCh()
		},
	)
}

// makeRunnerWithLeaderElection creates a LeaderElectionConfig with the provided options and callbacks.
// It will create a new resource lock associated with the configuration.
func makeRunnerWithLeaderElection(
	leOption leaderElectionOption,
	leaderElectionLockName string,
	onStartedLeading func(context.Context),
	onStoppedLeading func(),
) (*leaderelection.LeaderElectionConfig, error) {
	rl, err := resourcelock.New(resourcelock.LeasesResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		leaderElectionLockName,
		leOption.client.CoreV1(),
		leOption.client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      leOption.id,
			EventRecorder: leOption.recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	return &leaderelection.LeaderElectionConfig{
		Lock:            rl,
		LeaseDuration:   flags.F.LeaderElection.LeaseDuration.Duration,
		RenewDeadline:   flags.F.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:     flags.F.LeaderElection.RetryPeriod.Duration,
		ReleaseOnCancel: true, // release the lease when context (tied to stopCh) is canceled to speed failover
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: onStartedLeading,
			OnStoppedLeading: onStoppedLeading,
		},
	}, nil
}

func runIngressControllers(ctx *ingctx.ControllerContext, systemHealth *systemhealth.SystemHealth, option runOption, leOption leaderElectionOption, logger klog.Logger) {
	if flags.F.RunIngressController {
		lbc := controller.NewLoadBalancerController(ctx, option.stopCh, logger)
		systemHealth.AddHealthCheck("ingress", lbc.SystemHealth)
		runWithWg(lbc.Run, option.wg)
		logger.V(0).Info("ingress controller started")

		if !flags.F.EnableFirewallCR && flags.F.DisableFWEnforcement {
			klog.Fatalf("We can only disable the ingress controller FW enforcement when enabling the FW CR")
		}
		fwc, err := firewalls.NewFirewallController(ctx, flags.F.NodePortRanges.Values(), flags.F.EnableFirewallCR, flags.F.DisableFWEnforcement, ctx.EnableIngressRegionalExternal, option.stopCh, logger)
		if err != nil {
			klog.Fatalf("failed to create firewall controller: %v", err)
		}
		runWithWg(fwc.Run, option.wg)
		logger.V(0).Info("firewall controller started")
	}

	ctx.Start(option.stopCh)
}

func runL4Controllers(ctx *ingctx.ControllerContext, systemHealth *systemhealth.SystemHealth, option runOption, leOption leaderElectionOption, logger klog.Logger) {
	if !flags.F.RunL4Controller && !flags.F.EnablePSC && !flags.F.EnableIGController && !flags.F.RunL4NetLBController {
		return
	}

	if flags.F.GateL4ByLock {
		go collectLockAvailabilityMetrics(l4LockName, flags.F.GKEClusterType, option.stopCh, logger)
	}

	if flags.F.RunL4Controller {
		l4Controller := l4lb.NewILBController(ctx, option.stopCh, logger)
		systemHealth.AddHealthCheck(l4lb.L4ILBControllerName, l4Controller.SystemHealth)
		runWithWg(l4Controller.Run, option.wg)
		logger.V(0).Info("L4 controller started")
	}

	if flags.F.EnablePSC {
		pscController := psc.NewController(ctx, option.stopCh, logger)
		runWithWg(pscController.Run, option.wg)
		logger.V(0).Info("PSC Controller started")
	}

	if flags.F.EnableIGController {
		igControllerParams := &instancegroups.ControllerConfig{
			NodeInformer:             ctx.NodeInformer,
			ZoneGetter:               ctx.ZoneGetter,
			IGManager:                ctx.InstancePool,
			HasSynced:                ctx.HasSynced,
			EnableMultiSubnetCluster: flags.F.EnableIGMultiSubnetCluster,
			ReadOnlyMode:             flags.F.ReadOnlyMode,
			StopCh:                   option.stopCh,
		}
		igController := instancegroups.NewController(igControllerParams, logger)
		runWithWg(igController.Run, option.wg)
	}

	// The L4NetLbController will be run when RbsMode flag is Set
	if flags.F.RunL4NetLBController {
		l4netlbController := l4lb.NewL4NetLBController(ctx, option.stopCh, logger)
		systemHealth.AddHealthCheck(l4lb.L4NetLBControllerName, l4netlbController.SystemHealth)

		runWithWg(l4netlbController.Run, option.wg)
		logger.V(0).Info("L4NetLB controller started")
	}

	ctx.Start(option.stopCh)
}

func runNEGController(ctx *ingctx.ControllerContext, systemHealth *systemhealth.SystemHealth, option runOption, logger klog.Logger) error {
	lockLogger := logger.WithValues("lockName", negLockName)
	lockLogger.Info("Attempting to grab lock", "lockName", negLockName)
	go collectLockAvailabilityMetrics(negLockName, flags.F.GKEClusterType, option.stopCh, logger)

	if flags.F.EnableNEGController {
		negController, err := createNEGController(ctx, systemHealth, option.stopCh, logger)
		if err != nil {
			return fmt.Errorf("failed to create NEG controller: %w", err)
		}
		go runWithWg(negController.Run, option.wg)
		logger.V(0).Info("negController started")
	}

	// TODO(sawsa307): Find a better approach to start informers.
	// If Ingress and NEG controller run together, since they share the same
	// informers, they will be started twice, but on the second call, Run()
	// will be skipped with the following warnings:
	//    The sharedIndexInformer has started, run more than once is not allowed
	ctx.Start(option.stopCh)
	return nil
}

func createNEGController(ctx *ingctx.ControllerContext, systemHealth *systemhealth.SystemHealth, stopCh <-chan struct{}, logger klog.Logger) (*neg.Controller, error) {
	zoneGetter := ctx.ZoneGetter

	// In NonGCP mode, use the zone specified in gce.conf directly.
	// This overrides the zone/fault-domain label on nodes for NEG controller.
	if flags.F.EnableNonGCPMode {
		zoneGetter = zonegetter.NewNonGCPZoneGetter(ctx.Cloud.LocalZone())
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

	negMetrics := metrics.NewNegMetrics()
	syncerMetrics := syncMetrics.NewNegMetricsCollector(flags.F.NegMetricsExportInterval, logger)
	go syncerMetrics.Run(stopCh)

	// TODO: Refactor NEG to use cloud mocks so ctx.Cloud can be referenced within NewController.
	negController, err := neg.NewController(
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
		negtypes.NewAdapterWithRateLimitSpecs(ctx.Cloud, flags.F.GCERateLimit.Values(), adapter, negMetrics),
		zoneGetter,
		ctx.ClusterNamer,
		flags.F.ResyncPeriod,
		flags.F.NegGCPeriod,
		flags.F.NumNegGCWorkers,
		flags.F.EnableReadinessReflector,
		flags.F.EnableL4NEG,
		flags.F.EnableNonGCPMode,
		flags.F.EnableDualStackNEG,
		lpConfig,
		flags.F.EnableMultiNetworking,
		ctx.EnableIngressRegionalExternal,
		flags.F.EnableL4NetLBNEG,
		flags.F.ReadOnlyMode,
		flags.F.EnableNEGsForIngress,
		stopCh,
		logger,
		negMetrics,
		syncerMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create NEG controller: %w", err)
	}

	systemHealth.AddHealthCheck("neg-controller", negController.IsHealthy)
	return negController, nil
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
