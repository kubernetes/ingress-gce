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
	"bytes"
	go_flag "flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golang/glog"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/ingress-gce/pkg/backends"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/controller"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	neg "k8s.io/ingress-gce/pkg/networkendpointgroup"
	"k8s.io/ingress-gce/pkg/storage"
	"k8s.io/ingress-gce/pkg/utils"

	"k8s.io/kubernetes/pkg/cloudprovider"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

// Entrypoint of GLBC. Example invocation:
// 1. In a pod:
// glbc --delete-all-on-quit
// 2. Dry run (on localhost):
// $ kubectl proxy --api-prefix="/"
// $ glbc --proxy="http://localhost:proxyport"

const (
	// lbAPIPort is the port on which the loadbalancer controller serves a
	// minimal api (/healthz, /delete-all-and-quit etc).
	lbAPIPort = 8081

	// A delimiter used for clarity in naming GCE resources.
	clusterNameDelimiter = "--"

	// Arbitrarily chosen alphanumeric character to use in constructing resource
	// names, eg: to avoid cases where we end up with a name ending in '-'.
	alphaNumericChar = "0"

	// Current docker image version. Only used in debug logging.
	imageVersion = "glbc:0.9.7"

	// Key used to persist UIDs to configmaps.
	uidConfigMapName = "ingress-uid"

	// Sleep interval to retry cloud client creation.
	cloudClientRetryInterval = 10 * time.Second
)

var (
	flags = flag.NewFlagSet(
		`glbc: glbc --running-in-cluster=false`,
		flag.ExitOnError)

	clusterName = flags.String("cluster-uid", controller.DefaultClusterUID,
		`Optional, used to tag cluster wide, shared loadbalancer resources such
		 as instance groups. Use this flag if you'd like to continue using the
		 same resources across a pod restart. Note that this does not need to
		 match the name of you Kubernetes cluster, it's just an arbitrary name
		 used to tag/lookup cloud resources.`)

	inCluster = flags.Bool("running-in-cluster", true,
		`Optional, if this controller is running in a kubernetes cluster, use the
		 pod secrets for creating a Kubernetes client.`)

	apiServerHost = flags.String("apiserver-host", "", "The address of the Kubernetes Apiserver "+
		"to connect to in the format of protocol://address:port, e.g., "+
		"http://localhost:8080. If not specified, the assumption is that the binary runs inside a "+
		"Kubernetes cluster and local discovery is attempted.")
	kubeConfigFile = flags.String("kubeconfig", "", "Path to kubeconfig file with authorization and master location information.")

	// TODO: Consolidate this flag and running-in-cluster. People already use
	// the first one to mean "running in dev", unfortunately.
	useRealCloud = flags.Bool("use-real-cloud", false,
		`Optional, if set a real cloud client is created. Only matters with
		 --running-in-cluster=false, i.e a real cloud is always used when this
		 controller is running on a Kubernetes node.`)

	resyncPeriod = flags.Duration("sync-period", 30*time.Second,
		`Relist and confirm cloud resources this often.`)

	deleteAllOnQuit = flags.Bool("delete-all-on-quit", false,
		`If true, the controller will delete all Ingress and the associated
		external cloud resources as it's shutting down. Mostly used for
		testing. In normal environments the controller should only delete
		a loadbalancer if the associated Ingress is deleted.`)

	defaultSvc = flags.String("default-backend-service", "kube-system/default-http-backend",
		`Service used to serve a 404 page for the default backend. Takes the form
		namespace/name. The controller uses the first node port of this Service for
		the default backend.`)

	healthCheckPath = flags.String("health-check-path", "/",
		`Path used to health-check a backend service. All Services must serve
		a 200 page on this path. Currently this is only configurable globally.`)

	watchNamespace = flags.String("watch-namespace", v1.NamespaceAll,
		`Namespace to watch for Ingress/Services/Endpoints.`)

	verbose = flags.Bool("verbose", false,
		`If true, logs are displayed at V(4), otherwise V(2).`)

	configFilePath = flags.String("config-file-path", "",
		`Path to a file containing the gce config. If left unspecified this
		controller only works with default zones.`)

	healthzPort = flags.Int("healthz-port", lbAPIPort,
		`Port to run healthz server. Must match the health check port in yaml.`)
)

func registerHandlers(lbc *controller.LoadBalancerController) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := lbc.CloudClusterManager.IsHealthy(); err != nil {
			w.WriteHeader(500)
			w.Write([]byte(fmt.Sprintf("Cluster unhealthy: %v", err)))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/delete-all-and-quit", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Retry failures during shutdown.
		lbc.Stop(true)
	})

	glog.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", *healthzPort), nil))
}

func handleSigterm(lbc *controller.LoadBalancerController, deleteAll bool) {
	// Multiple SIGTERMs will get dropped
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGTERM)
	<-signalChan
	glog.Infof("Received SIGTERM, shutting down")

	// TODO: Better retires than relying on restartPolicy.
	exitCode := 0
	if err := lbc.Stop(deleteAll); err != nil {
		glog.Infof("Error during shutdown %v", err)
		exitCode = 1
	}
	glog.Infof("Exiting with %v", exitCode)
	os.Exit(exitCode)
}

// main function for GLBC.
func main() {
	// TODO: Add a healthz endpoint
	var err error
	var clusterManager *controller.ClusterManager

	// TODO: We can simply parse all go flags with
	// flags.AddGoFlagSet(go_flag.CommandLine)
	// but that pollutes --help output with a ton of standard go flags.
	// We only really need a binary switch from light, v(2) logging to
	// heavier debug style V(4) logging, which we use --verbose for.
	flags.Parse(os.Args)

	// Set glog verbosity levels, unconditionally set --alsologtostderr.
	go_flag.Lookup("logtostderr").Value.Set("true")
	if *verbose {
		go_flag.Set("v", "4")
	}
	glog.Infof("Starting GLBC image: %v, cluster name %v", imageVersion, *clusterName)
	if *defaultSvc == "" {
		glog.Fatalf("Please specify --default-backend")
	}

	var config *rest.Config
	// Create kubeclient
	if *inCluster {
		if config, err = rest.InClusterConfig(); err != nil {
			glog.Fatalf("error creating client configuration: %v", err)
		}
	} else {
		if *apiServerHost == "" {
			glog.Fatalf("please specify the api server address using the flag --apiserver-host")
		}

		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: *kubeConfigFile},
			&clientcmd.ConfigOverrides{
				ClusterInfo: clientcmdapi.Cluster{
					Server: *apiServerHost,
				},
			}).ClientConfig()
		if err != nil {
			glog.Fatalf("error creating client configuration: %v", err)
		}
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Failed to create client: %v.", err)
	}

	// Wait for the default backend Service. There's no pretty way to do this.
	parts := strings.Split(*defaultSvc, "/")
	if len(parts) != 2 {
		glog.Fatalf("Default backend should take the form namespace/name: %v",
			*defaultSvc)
	}
	port, nodePort, err := getNodePort(kubeClient, parts[0], parts[1])
	if err != nil {
		glog.Fatalf("Could not configure default backend %v: %v",
			*defaultSvc, err)
	}
	// The default backend is known to be HTTP
	defaultBackendNodePort := backends.ServicePort{
		Port:     int64(nodePort),
		Protocol: utils.ProtocolHTTP,
		SvcName:  types.NamespacedName{Namespace: parts[0], Name: parts[1]},
		SvcPort:  intstr.FromInt(int(port)),
	}

	var namer *utils.Namer
	var cloud *gce.GCECloud
	if *inCluster || *useRealCloud {
		// Create cluster manager
		namer, err = newNamer(kubeClient, *clusterName, controller.DefaultFirewallName)
		if err != nil {
			glog.Fatalf("%v", err)
		}

		// TODO: Make this more resilient. Currently we create the cloud client
		// and pass it through to all the pools. This makes unit testing easier.
		// However if the cloud client suddenly fails, we should try to re-create it
		// and continue.
		if *configFilePath != "" {
			glog.Infof("Reading config from path %v", *configFilePath)
			config, err := os.Open(*configFilePath)
			if err != nil {
				glog.Fatalf("%v", err)
			}
			defer config.Close()
			cloud = getGCEClient(config)
			glog.Infof("Successfully loaded cloudprovider using config %q", *configFilePath)
		} else {
			// While you might be tempted to refactor so we simply assing nil to the
			// config and only invoke getGCEClient once, that will not do the right
			// thing because a nil check against an interface isn't true in golang.
			cloud = getGCEClient(nil)
			glog.Infof("Created GCE client without a config file")
		}

		clusterManager, err = controller.NewClusterManager(cloud, namer, defaultBackendNodePort, *healthCheckPath)
		if err != nil {
			glog.Fatalf("%v", err)
		}
	} else {
		// Create fake cluster manager
		clusterManager = controller.NewFakeClusterManager(*clusterName, controller.DefaultFirewallName).ClusterManager
	}
	enableNEG := cloud.AlphaFeatureGate.Enabled(gce.AlphaFeatureNetworkEndpointGroup)
	ctx := context.NewControllerContext(kubeClient, *watchNamespace, *resyncPeriod, enableNEG)
	// Start loadbalancer controller
	lbc, err := controller.NewLoadBalancerController(kubeClient, ctx, clusterManager, enableNEG)
	if err != nil {
		glog.Fatalf("%v", err)
	}

	if clusterManager.ClusterNamer.UID() != "" {
		glog.V(3).Infof("Cluster name %+v", clusterManager.ClusterNamer.UID())
	}
	clusterManager.Init(&controller.GCETranslator{LoadBalancerController: lbc})

	// Start NEG controller
	if enableNEG {
		negController, _ := neg.NewController(kubeClient, cloud, ctx, lbc.Translator, namer, *resyncPeriod)
		go negController.Run(ctx.StopCh)
	}

	go registerHandlers(lbc)
	go handleSigterm(lbc, *deleteAllOnQuit)

	ctx.Start()
	lbc.Run()
	for {
		glog.Infof("Handled quit, awaiting pod deletion.")
		time.Sleep(30 * time.Second)
	}
}

func newNamer(kubeClient kubernetes.Interface, clusterName string, fwName string) (*utils.Namer, error) {
	name, err := getClusterUID(kubeClient, clusterName)
	if err != nil {
		return nil, err
	}
	fwName, err = getFirewallName(kubeClient, fwName, name)
	if err != nil {
		return nil, err
	}

	namer := utils.NewNamer(name, fwName)
	uidVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName)

	// Start a goroutine to poll the cluster UID config map
	// We don't watch because we know exactly which configmap we want and this
	// controller already watches 5 other resources, so it isn't worth the cost
	// of another connection and complexity.
	go wait.Forever(func() {
		for _, key := range [...]string{storage.UidDataKey, storage.ProviderDataKey} {
			val, found, err := uidVault.Get(key)
			if err != nil {
				glog.Errorf("Can't read uidConfigMap %v", uidConfigMapName)
			} else if !found {
				errmsg := fmt.Sprintf("Can't read %v from uidConfigMap %v", key, uidConfigMapName)
				if key == storage.UidDataKey {
					glog.Errorf(errmsg)
				} else {
					glog.V(4).Infof(errmsg)
				}
			} else {

				switch key {
				case storage.UidDataKey:
					if uid := namer.UID(); uid != val {
						glog.Infof("Cluster uid changed from %v -> %v", uid, val)
						namer.SetUID(val)
					}
				case storage.ProviderDataKey:
					if fwName := namer.Firewall(); fwName != val {
						glog.Infof("Cluster firewall name changed from %v -> %v", fwName, val)
						namer.SetFirewall(val)
					}
				}
			}
		}
	}, 5*time.Second)
	return namer, nil
}

// useDefaultOrLookupVault returns either a 'defaultName' or if unset, obtains a name from a ConfigMap.
// The returned value follows this priority:
// If the provided 'defaultName' is not empty, that name is used.
//       This is effectively a client override via a command line flag.
// else, check cfgVault with 'cmKey' as a key and if found, use the associated value
// else, return an empty 'name' and pass along an error iff the configmap lookup is erroneous.
func useDefaultOrLookupVault(cfgVault *storage.ConfigMapVault, cmKey, defaultName string) (string, error) {
	if defaultName != "" {
		glog.Infof("Using user provided %v %v", cmKey, defaultName)
		// Don't save the uid in the vault, so users can rollback through
		// setting the accompany flag to ""
		return defaultName, nil
	}
	val, found, err := cfgVault.Get(cmKey)
	if err != nil {
		// This can fail because of:
		// 1. No such config map - found=false, err=nil
		// 2. No such key in config map - found=false, err=nil
		// 3. Apiserver flake - found=false, err!=nil
		// It is not safe to proceed in 3.
		return "", fmt.Errorf("failed to retrieve %v: %v, returning empty name", cmKey, err)
	} else if !found {
		// Not found but safe to proceed.
		return "", nil
	}
	glog.Infof("Using %v = %q saved in ConfigMap", cmKey, val)
	return val, nil
}

// getFirewallName returns the firewall rule name to use for this cluster. For
// backwards compatibility, the firewall name will default to the cluster UID.
// Use getFlagOrLookupVault to obtain a stored or overridden value for the firewall name.
// else, use the cluster UID as a backup (this retains backwards compatibility).
func getFirewallName(kubeClient kubernetes.Interface, name, clusterUID string) (string, error) {
	cfgVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName)
	if fwName, err := useDefaultOrLookupVault(cfgVault, storage.ProviderDataKey, name); err != nil {
		return "", err
	} else if fwName != "" {
		return fwName, cfgVault.Put(storage.ProviderDataKey, fwName)
	} else {
		glog.Infof("Using cluster UID %v as firewall name", clusterUID)
		return clusterUID, cfgVault.Put(storage.ProviderDataKey, clusterUID)
	}
}

// getClusterUID returns the cluster UID. Rules for UID generation:
// If the user specifies a --cluster-uid param it overwrites everything
// else, check UID config map for a previously recorded uid
// else, check if there are any working Ingresses
//	- remember that "" is the cluster uid
// else, allocate a new uid
func getClusterUID(kubeClient kubernetes.Interface, name string) (string, error) {
	cfgVault := storage.NewConfigMapVault(kubeClient, metav1.NamespaceSystem, uidConfigMapName)
	if name, err := useDefaultOrLookupVault(cfgVault, storage.UidDataKey, name); err != nil {
		return "", err
	} else if name != "" {
		return name, nil
	}

	// Check if the cluster has an Ingress with ip
	ings, err := kubeClient.Extensions().Ingresses(metav1.NamespaceAll).List(metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
	})
	if err != nil {
		return "", err
	}
	namer := utils.NewNamer("", "")
	for _, ing := range ings.Items {
		if len(ing.Status.LoadBalancer.Ingress) != 0 {
			c := namer.ParseName(loadbalancers.GCEResourceName(ing.Annotations, "forwarding-rule"))
			if c.ClusterName != "" {
				return c.ClusterName, cfgVault.Put(storage.UidDataKey, c.ClusterName)
			}
			glog.Infof("Found a working Ingress, assuming uid is empty string")
			return "", cfgVault.Put(storage.UidDataKey, "")
		}
	}

	// Allocate new uid
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return "", err
	}
	defer f.Close()
	b := make([]byte, 8)
	if _, err := f.Read(b); err != nil {
		return "", err
	}
	uid := fmt.Sprintf("%x", b)
	return uid, cfgVault.Put(storage.UidDataKey, uid)
}

// getNodePort waits for the Service, and returns it's first node port.
func getNodePort(client kubernetes.Interface, ns, name string) (port, nodePort int32, err error) {
	var svc *v1.Service
	glog.V(3).Infof("Waiting for %v/%v", ns, name)
	wait.Poll(1*time.Second, 5*time.Minute, func() (bool, error) {
		svc, err = client.Core().Services(ns).Get(name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		for _, p := range svc.Spec.Ports {
			if p.NodePort != 0 {
				port = p.Port
				nodePort = p.NodePort
				glog.V(3).Infof("Node port %v", nodePort)
				break
			}
		}
		return true, nil
	})
	return
}

func getGCEClient(config io.Reader) *gce.GCECloud {
	getConfigReader := func() io.Reader { return nil }

	if config != nil {
		allConfig, err := ioutil.ReadAll(config)
		if err != nil {
			glog.Fatalf("Error while reading entire config: %v", err)
		}
		glog.V(2).Infof("Using cloudprovider config file:\n%v ", string(allConfig))

		getConfigReader = func() io.Reader {
			return bytes.NewReader(allConfig)
		}
	} else {
		glog.V(2).Infoln("No cloudprovider config file provided. Continuing with default values.")
	}

	// Creating the cloud interface involves resolving the metadata server to get
	// an oauth token. If this fails, the token provider assumes it's not on GCE.
	// No errors are thrown. So we need to keep retrying till it works because
	// we know we're on GCE.
	for {
		cloudInterface, err := cloudprovider.GetCloudProvider("gce", getConfigReader())
		if err == nil {
			cloud := cloudInterface.(*gce.GCECloud)

			// If this controller is scheduled on a node without compute/rw
			// it won't be allowed to list backends. We can assume that the
			// user has no need for Ingress in this case. If they grant
			// permissions to the node they will have to restart the controller
			// manually to re-create the client.
			if _, err = cloud.ListGlobalBackendServices(); err == nil || utils.IsHTTPErrorCode(err, http.StatusForbidden) {
				return cloud
			}
			glog.Warningf("Failed to list backend services, retrying: %v", err)
		} else {
			glog.Warningf("Failed to retrieve cloud interface, retrying: %v", err)
		}
		time.Sleep(cloudClientRetryInterval)
	}
}
