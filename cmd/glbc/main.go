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
	"fmt"
	"math/rand"
	"os"
	"time"

	flag "github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/frontendconfig"
	"k8s.io/ingress-gce/pkg/ingparams"
	"k8s.io/ingress-gce/pkg/psc"
	"k8s.io/ingress-gce/pkg/serviceattachment"
	"k8s.io/ingress-gce/pkg/svcneg"
	"k8s.io/klog"

	crdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	backendconfigclient "k8s.io/ingress-gce/pkg/backendconfig/client/clientset/versioned"
	frontendconfigclient "k8s.io/ingress-gce/pkg/frontendconfig/client/clientset/versioned"
	ingparamsclient "k8s.io/ingress-gce/pkg/ingparams/client/clientset/versioned"
	serviceattachmentclient "k8s.io/ingress-gce/pkg/serviceattachment/client/clientset/versioned"
	svcnegclient "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned"

	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	"k8s.io/ingress-gce/pkg/neg"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"

	"k8s.io/ingress-gce/cmd/glbc/app"
	"k8s.io/ingress-gce/pkg/backendconfig"
	"k8s.io/ingress-gce/pkg/crd"
	"k8s.io/ingress-gce/pkg/firewalls"
	"k8s.io/ingress-gce/pkg/flags"
	_ "k8s.io/ingress-gce/pkg/klog"
	"k8s.io/ingress-gce/pkg/l4"
	"k8s.io/ingress-gce/pkg/version"
)

func main() {
	flags.Register()
	rand.Seed(time.Now().UTC().UnixNano())
	flag.Parse()

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

	var svcNegClient svcnegclient.Interface
	negCRDMeta := svcneg.CRDMeta()
	if _, err := crdHandler.EnsureCRD(negCRDMeta, true); err != nil {
		klog.Fatalf("Failed to ensure ServiceNetworkEndpointGroup CRD: %v", err)
	}

	svcNegClient, err = svcnegclient.NewForConfig(kubeConfig)
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
		Namespace:             flags.F.WatchNamespace,
		ResyncPeriod:          flags.F.ResyncPeriod,
		NumL4Workers:          flags.F.NumL4Workers,
		DefaultBackendSvcPort: defaultBackendServicePort,
		HealthCheckPath:       flags.F.HealthCheckPath,
		FrontendConfigEnabled: flags.F.EnableFrontendConfig,
		EnableASMConfigMap:    flags.F.EnableASMConfigMapBasedConfig,
		ASMConfigMapNamespace: flags.F.ASMConfigMapBasedConfigNamespace,
		ASMConfigMapName:      flags.F.ASMConfigMapBasedConfigCMName,
	}
	ctx := ingctx.NewControllerContext(kubeConfig, kubeClient, backendConfigClient, frontendConfigClient, svcNegClient, ingParamsClient, svcAttachmentClient, cloud, namer, kubeSystemUID, ctxConfig)
	go app.RunHTTPServer(ctx.HealthCheck)

	if !flags.F.LeaderElection.LeaderElect {
		runControllers(ctx)
		return
	}

	electionConfig, err := makeLeaderElectionConfig(ctx, leaderElectKubeClient, ctx.Recorder(flags.F.LeaderElection.LockObjectNamespace))
	if err != nil {
		klog.Fatalf("%v", err)
	}
	leaderelection.RunOrDie(context.Background(), *electionConfig)
	klog.Warning("Ingress Controller exited.")
}

// makeLeaderElectionConfig builds a leader election configuration. It will
// create a new resource lock associated with the configuration.
func makeLeaderElectionConfig(ctx *ingctx.ControllerContext, client clientset.Interface, recorder record.EventRecorder) (*leaderelection.LeaderElectionConfig, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("unable to get hostname: %v", err)
	}
	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := fmt.Sprintf("%v_%x", hostname, rand.Intn(1e6))
	rl, err := resourcelock.New(resourcelock.ConfigMapsResourceLock,
		flags.F.LeaderElection.LockObjectNamespace,
		flags.F.LeaderElection.LockObjectName,
		client.CoreV1(),
		client.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: recorder,
		})
	if err != nil {
		return nil, fmt.Errorf("couldn't create resource lock: %v", err)
	}

	run := func() {
		runControllers(ctx)
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

func runControllers(ctx *ingctx.ControllerContext) {
	stopCh := make(chan struct{})
	ctx.Init()
	lbc := controller.NewLoadBalancerController(ctx, stopCh)
	if ctx.EnableASMConfigMap {
		ctx.ASMConfigController.RegisterInformer(ctx.ConfigMapInformer, func() {
			lbc.Stop(false) // We want to trigger a restart, don't have to clean up all the resources.
		})
	}

	fwc := firewalls.NewFirewallController(ctx, flags.F.NodePortRanges.Values())

	if flags.F.RunL4Controller {
		l4Controller := l4.NewController(ctx, stopCh)
		go l4Controller.Run()
		klog.V(0).Infof("L4 controller started")
	}

	if flags.F.EnablePSC {
		pscController := psc.NewController(ctx)
		go pscController.Run(stopCh)
		klog.V(0).Infof("PSC Controller started")
	}

	var zoneGetter negtypes.ZoneGetter
	zoneGetter = lbc.Translator
	// In NonGCP mode, use the zone specified in gce.conf directly.
	// This overrides the zone/fault-domain label on nodes for NEG controller.
	if flags.F.EnableNonGCPMode {
		zone, err := ctx.Cloud.GetZone(context.Background())
		if err != nil {
			klog.Errorf("Failed to retrieve zone information from Cloud provider: %v; Please check if local-zone is specified in gce.conf.", err)
		} else {
			zoneGetter = negtypes.NewSimpleZoneGetter(zone.FailureDomain)
		}
	}

	enableAsm := false
	asmServiceNEGSkipNamespaces := []string{}
	if ctx.EnableASMConfigMap {
		cmconfig := ctx.ASMConfigController.GetConfig()
		enableAsm = cmconfig.EnableASM
		asmServiceNEGSkipNamespaces = cmconfig.ASMServiceNEGSkipNamespaces
	}

	// TODO: Refactor NEG to use cloud mocks so ctx.Cloud can be referenced within NewController.
	negController := neg.NewController(
		ctx.KubeClient,
		ctx.SvcNegClient,
		ctx.DestinationRuleClient,
		ctx.KubeSystemUID,
		ctx.IngressInformer,
		ctx.ServiceInformer,
		ctx.PodInformer,
		ctx.NodeInformer,
		ctx.EndpointInformer,
		ctx.DestinationRuleInformer,
		ctx.SvcNegInformer,
		ctx.HasSynced,
		ctx.ControllerMetrics,
		ctx.L4Namer,
		ctx.DefaultBackendSvcPort,
		negtypes.NewAdapter(ctx.Cloud),
		zoneGetter,
		ctx.ClusterNamer,
		flags.F.ResyncPeriod,
		flags.F.NegGCPeriod,
		flags.F.EnableReadinessReflector,
		flags.F.RunIngressController,
		flags.F.RunL4Controller,
		flags.F.EnableNonGCPMode,
		enableAsm,
		asmServiceNEGSkipNamespaces,
	)

	ctx.AddHealthCheck("neg-controller", negController.IsHealthy)

	go negController.Run(stopCh)
	klog.V(0).Infof("negController started")

	go app.RunSIGTERMHandler(lbc, flags.F.DeleteAllOnQuit)

	go fwc.Run()
	klog.V(0).Infof("firewall controller started")

	ctx.Start(stopCh)
	lbc.Init()
	lbc.Run()

	for {
		klog.Warning("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
		if ctx.EnableASMConfigMap {
			return
		}
	}
}
