/*
Copyright 2018 The Kubernetes Authors.

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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/ingress-gce/cmd/echo/app"
	"k8s.io/ingress-gce/pkg/annotations"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/e2e/adapter"
	"k8s.io/ingress-gce/pkg/fuzz"
	"k8s.io/ingress-gce/pkg/fuzz/features"
	"k8s.io/ingress-gce/pkg/fuzz/whitebox"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/psc"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/common"
	"k8s.io/klog"
	utilpointer "k8s.io/utils/pointer"
)

const (
	ingressPollInterval = 30 * time.Second
	// TODO(shance): Find a way to lower this timeout
	ingressPollTimeout = 45 * time.Minute

	gclbDeletionInterval = 30 * time.Second
	// TODO(smatti): Change this back to 15 when the issue
	// is fixed.
	gclbDeletionTimeout = 60 * time.Minute

	negPollInterval  = 5 * time.Second
	negPollTimeout   = 3 * time.Minute
	negGCPollTimeout = 5 * time.Minute

	k8sApiPoolInterval = 10 * time.Second
	k8sApiPollTimeout  = 30 * time.Minute

	updateIngressPollInterval = 30 * time.Second
	updateIngressPollTimeout  = 15 * time.Minute

	backendConfigEnsurePollInterval = 5 * time.Second
	backendConfigEnsurePollTimeout  = 15 * time.Minute
	backendConfigCRDName            = "backendconfigs.cloud.google.com"

	redirectURLMapPollTimeout = 10 * time.Minute

	healthyState = "HEALTHY"
)

// WaitForIngressOptions holds options dictating how we wait for an ingress to stabilize
type WaitForIngressOptions struct {
	// ExpectUnreachable is true when we expect the LB to still be
	// programming itself (i.e 404's / 502's)
	ExpectUnreachable bool
}

// Scheme is the default instance of runtime.Scheme to which types in the Kubernetes API are already registered.
// This is needed for ConfigMap search.
var Scheme = runtime.NewScheme()

func init() {
	// Register external types for Scheme
	v1.AddToScheme(Scheme)
}

// IsRfc1918Addr returns true if the address supplied is an RFC1918 address
func IsRfc1918Addr(addr string) bool {
	ip := net.ParseIP(addr)
	var ipBlocks []*net.IPNet
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	} {
		_, block, _ := net.ParseCIDR(cidr)
		ipBlocks = append(ipBlocks, block)
	}

	for _, block := range ipBlocks {
		if block.Contains(ip) {
			return true
		}
	}

	return false
}

// UpgradeTestWaitForIngress waits for ingress to stabilize and set sandbox status to stable.
// Note that this is used only for upgrade tests.
func UpgradeTestWaitForIngress(s *Sandbox, ing *networkingv1.Ingress, options *WaitForIngressOptions) (*networkingv1.Ingress, error) {
	ing, err := WaitForIngress(s, ing, nil, options)
	if err != nil {
		return nil, err
	}
	s.PutStatus(Stable)
	return ing, nil
}

// WaitForIngress to stabilize.
// We expect the ingress to be unreachable at first as LB is
// still programming itself (i.e 404's / 502's)
func WaitForIngress(s *Sandbox, ing *networkingv1.Ingress, fc *frontendconfigv1beta1.FrontendConfig, options *WaitForIngressOptions) (*networkingv1.Ingress, error) {
	err := wait.Poll(ingressPollInterval, ingressPollTimeout, func() (bool, error) {
		var err error
		crud := adapter.IngressCRUD{C: s.f.Clientset}
		ing, err = crud.Get(s.Namespace, ing.Name)
		if err != nil {
			return true, err
		}
		attrs := fuzz.DefaultAttributes()
		attrs.Region = s.f.Region
		validator, err := fuzz.NewIngressValidator(s.ValidatorEnv, ing, fc, []fuzz.WhiteboxTest{}, attrs, features.All)
		if err != nil {
			return true, err
		}
		result := validator.Check(context.Background())
		if result.Err == nil {
			return true, nil
		}
		if options == nil || options.ExpectUnreachable {
			return false, nil
		}
		return true, fmt.Errorf("unexpected error from validation: %v", result.Err)
	})
	return ing, err
}

// WaitForHTTPResourceAnnotations waits for http forwarding rule annotation to
// be added on ingress.
// This is to handle a special case where ingress updates from https only to http
// or both http and https enabled. Turns out port 80 on ingress VIP is accessible
// even when http forwarding rule and target proxy do not exist. So, ingress
// validator thinks that http load balancer is configured when https only
// configuration exists.
// TODO(smatti): Remove this when the above issue is fixed.
func WaitForHTTPResourceAnnotations(s *Sandbox, ing *networkingv1.Ingress) (*networkingv1.Ingress, error) {
	ingKey := fmt.Sprintf("%s/%s", s.Namespace, ing.Name)
	klog.Infof("Waiting for HTTP annotations to be added on Ingress %s", ingKey)
	var err error
	if waitErr := wait.Poll(updateIngressPollInterval, updateIngressPollTimeout, func() (bool, error) {
		crud := adapter.IngressCRUD{C: s.f.Clientset}
		if ing, err = crud.Get(s.Namespace, ing.Name); err != nil {
			return true, err
		}
		if _, ok := ing.Annotations[annotations.HttpForwardingRuleKey]; !ok {
			klog.Infof("HTTP forwarding rule annotation not found on ingress %s, retrying", ingKey)
			return false, nil
		}
		klog.Infof("HTTP forwarding rule annotation found on ingress %s", ingKey)
		return true, nil
	}); waitErr != nil {
		if waitErr == wait.ErrWaitTimeout {
			return nil, fmt.Errorf("error time out, last seem error: %v", err)
		}
		return nil, waitErr
	}
	return ing, nil
}

// WaitForFinalizer waits for Finalizer to be added.
// Note that this is used only for upgrade tests.
func WaitForFinalizer(s *Sandbox, ing *networkingv1.Ingress) (*networkingv1.Ingress, error) {
	ingKey := fmt.Sprintf("%s/%s", s.Namespace, ing.Name)
	klog.Infof("Waiting for Finalizer to be added for Ingress %s", ingKey)
	err := wait.Poll(k8sApiPoolInterval, k8sApiPollTimeout, func() (bool, error) {
		var err error
		crud := adapter.IngressCRUD{C: s.f.Clientset}
		if ing, err = crud.Get(s.Namespace, ing.Name); err != nil {
			klog.Infof("WaitForFinalizer(%s) = %v, error retrieving Ingress", ingKey, err)
			return false, nil
		}
		ingFinalizers := ing.GetFinalizers()
		if l := len(ingFinalizers); l > 1 {
			return true, fmt.Errorf("unexpected number of finalizers for ingress %v, got %d", ing, l)
		} else if l != 1 {
			klog.Infof("WaitForFinalizer(%s) = %v, finalizer not added for Ingress %v", ingKey, ingFinalizers, ing)
			return false, nil
		}
		return true, nil
	})
	if err == nil {
		// Set status back to stable.
		s.PutStatus(Stable)
	}
	return ing, err
}

// WhiteboxTest retrieves GCP load-balancer for Ingress VIP and runs the whitebox tests.
func WhiteboxTest(ing *networkingv1.Ingress, fc *frontendconfigv1beta1.FrontendConfig, cloud cloud.Cloud, region string, s *Sandbox) (*fuzz.GCLB, error) {
	if len(ing.Status.LoadBalancer.Ingress) < 1 {
		return nil, fmt.Errorf("ingress does not have an IP: %+v", ing.Status)
	}

	vip := ing.Status.LoadBalancer.Ingress[0].IP
	klog.Infof("Ingress %s/%s VIP = %s", s.Namespace, ing.Name, vip)
	params := &fuzz.GCLBForVIPParams{
		VIP:        vip,
		Region:     region,
		Validators: fuzz.FeatureValidators(features.All),
	}
	gclb, err := fuzz.GCLBForVIP(context.Background(), cloud, params)
	if err != nil {
		return nil, fmt.Errorf("error getting GCP resources for LB with IP = %q: %v", vip, err)
	}

	if err := performWhiteboxTests(s, ing, fc, gclb); err != nil {
		return nil, fmt.Errorf("error performing whitebox tests: %v", err)
	}
	return gclb, nil
}

// performWhiteboxTests runs the whitebox tests against the Ingress.
func performWhiteboxTests(s *Sandbox, ing *networkingv1.Ingress, fc *frontendconfigv1beta1.FrontendConfig, gclb *fuzz.GCLB) error {
	validator, err := fuzz.NewIngressValidator(s.ValidatorEnv, ing, fc, whitebox.AllTests, nil, []fuzz.Feature{})
	if err != nil {
		return err
	}
	if err := validator.PerformWhiteboxTests(gclb); err != nil {
		return err
	}
	return validator.FrontendNamingSchemeTest(gclb)
}

// WaitForIngressDeletion deletes the given ingress and waits for the
// resources associated with it to be deleted.
func WaitForIngressDeletion(ctx context.Context, g *fuzz.GCLB, s *Sandbox, ing *networkingv1.Ingress, options *fuzz.GCLBDeleteOptions) error {
	crud := adapter.IngressCRUD{C: s.f.Clientset}
	if err := crud.Delete(ing.Namespace, ing.Name); err != nil {
		return fmt.Errorf("delete(%q) = %v, want nil", ing.Name, err)
	}
	klog.Infof("Waiting for GCLB resources to be deleted (%s/%s), IngressDeletionOptions=%+v", s.Namespace, ing.Name, options)
	if err := WaitForGCLBDeletion(ctx, s.f.Cloud, g, options); err != nil {
		return fmt.Errorf("WaitForGCLBDeletion(...) = %v, want nil", err)
	}
	klog.Infof("GCLB resources deleted (%s/%s)", s.Namespace, ing.Name)
	return nil
}

// WaitForFinalizerDeletion waits for gclb resources to be deleted and
// the finalizer attached to the Ingress resource to be removed.
func WaitForFinalizerDeletion(ctx context.Context, g *fuzz.GCLB, s *Sandbox, ingName string, options *fuzz.GCLBDeleteOptions) error {
	klog.Infof("Waiting for GCLB resources to be deleted (%s/%s), IngressDeletionOptions=%+v", s.Namespace, ingName, options)
	if err := WaitForGCLBDeletion(ctx, s.f.Cloud, g, options); err != nil {
		return fmt.Errorf("WaitForGCLBDeletion(...) = %v, want nil", err)
	}
	klog.Infof("GCLB resources deleted (%s/%s)", s.Namespace, ingName)

	crud := adapter.IngressCRUD{C: s.f.Clientset}
	klog.Infof("Waiting for Finalizer to be removed for Ingress %s/%s", s.Namespace, ingName)
	return wait.Poll(k8sApiPoolInterval, k8sApiPollTimeout, func() (bool, error) {
		ing, err := crud.Get(s.Namespace, ingName)
		if err != nil {
			klog.Infof("WaitForFinalizerDeletion(%s/%s) = Error retrieving Ingress: %v", s.Namespace, ingName, err)
			return false, nil
		}
		if len(ing.GetFinalizers()) != 0 {
			klog.Infof("WaitForFinalizerDeletion(%s/%s) = %v", s.Namespace, ing.Name, ing.GetFinalizers())
			return false, nil
		}
		return true, nil
	})
}

// WaitForGCLBDeletion waits for the resources associated with the GLBC to be
// deleted.
func WaitForGCLBDeletion(ctx context.Context, c cloud.Cloud, g *fuzz.GCLB, options *fuzz.GCLBDeleteOptions) error {
	return wait.Poll(gclbDeletionInterval, gclbDeletionTimeout, func() (bool, error) {
		if err := g.CheckResourceDeletion(ctx, c, options); err != nil {
			klog.Infof("WaitForGCLBDeletion(%q) = %v", g.VIP, err)
			return false, nil
		}
		return true, nil
	})
}

// WaitForFrontendResourceDeletion waits for frontend resources associated with the GLBC to be
// deleted for given protocol.
func WaitForFrontendResourceDeletion(ctx context.Context, c cloud.Cloud, g *fuzz.GCLB, options *fuzz.GCLBDeleteOptions) error {
	return wait.Poll(gclbDeletionInterval, gclbDeletionTimeout, func() (bool, error) {
		if options.CheckHttpFrontendResources {
			if err := g.CheckResourceDeletionByProtocol(ctx, c, options, fuzz.HttpProtocol); err != nil {
				klog.Infof("WaitForGCLBDeletionByProtocol(..., %q, %q) = %v", g.VIP, fuzz.HttpProtocol, err)
				return false, nil
			}
		}
		if options.CheckHttpsFrontendResources {
			if err := g.CheckResourceDeletionByProtocol(ctx, c, options, fuzz.HttpsProtocol); err != nil {
				klog.Infof("WaitForGCLBDeletionByProtocol(..., %q, %q) = %v", g.VIP, fuzz.HttpsProtocol, err)
				return false, nil
			}
		}
		return true, nil
	})
}

// WaitForNEGDeletion waits for all NEGs associated with a GCLB to be deleted via GC
func WaitForNEGDeletion(ctx context.Context, c cloud.Cloud, g *fuzz.GCLB, options *fuzz.GCLBDeleteOptions) error {
	return wait.Poll(negPollInterval, gclbDeletionTimeout, func() (bool, error) {
		if err := g.CheckNEGDeletion(ctx, c, options); err != nil {
			klog.Infof("WaitForNegDeletion(%q) = %v", g.VIP, err)
			return false, nil
		}
		return true, nil
	})
}

func WaitForRedirectURLMapDeletion(ctx context.Context, c cloud.Cloud, g *fuzz.GCLB) error {
	return wait.Poll(gclbDeletionInterval, redirectURLMapPollTimeout, func() (bool, error) {
		if err := g.CheckRedirectUrlMapDeletion(ctx, c); err != nil {
			klog.Infof("WaitForRedirectURLMapDeletion(%q) = %v", g.VIP, err)
			return false, nil
		}
		return true, nil
	})
}

// WaitForEchoDeploymentStable waits until the deployment's readyReplicas, availableReplicas and updatedReplicas are equal to replicas.
func WaitForEchoDeploymentStable(s *Sandbox, name string) error {
	return wait.Poll(k8sApiPoolInterval, k8sApiPollTimeout, func() (bool, error) {
		deployment, err := s.f.Clientset.AppsV1().Deployments(s.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if deployment == nil || err != nil {
			return false, fmt.Errorf("failed to get deployment %s/%s: %v", s.Namespace, name, err)
		}
		if err := CheckDeployment(deployment); err != nil {
			klog.Infof("WaitForEchoDeploymentStable(%s/%s) = %v", s.Namespace, name, err)
			return false, nil
		}
		return true, nil
	})
}

// WaitForNegStatus waits util the neg status on the service got to expected state.
// if noPresentTest set to true, WaitForNegStatus makes sure no NEG annotation is added until timeout(5 mins).
func WaitForNegStatus(s *Sandbox, name string, expectSvcPorts []string, noPresentTest bool) (*annotations.NegStatus, error) {
	var ret annotations.NegStatus
	var err error
	timeout := gclbDeletionTimeout
	if noPresentTest {
		timeout = 2 * time.Minute
	}
	err = wait.Poll(negPollInterval, timeout, func() (bool, error) {
		svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if svc == nil || err != nil {
			return false, fmt.Errorf("failed to get service %s/%s: %v", s.Namespace, name, err)
		}
		ret, err = CheckNegStatus(svc, expectSvcPorts)
		if err != nil {
			klog.Infof("WaitForNegStatus(%s/%s, %v) = %v", s.Namespace, name, expectSvcPorts, err)
			return false, nil
		}
		return true, nil
	})
	if noPresentTest && err == wait.ErrWaitTimeout {
		return nil, nil
	}
	return &ret, err
}

// WaitForNegs waits until the input NEG got into the expect states.
func WaitForNegs(ctx context.Context, c cloud.Cloud, negName string, zones []string, expectHealthy bool, expectCount int) error {
	return wait.Poll(negPollInterval, negPollTimeout, func() (bool, error) {
		negs, err := fuzz.NetworkEndpointsInNegs(ctx, c, negName, zones)
		if err != nil {
			klog.Infof("WaitForNegs(%q, %v, %v, %v) failed to retrieve NEGs: %v", negName, zones, expectHealthy, expectCount, err)
			return false, nil
		}

		if err := CheckNegs(negs, expectHealthy, expectCount); err != nil {
			klog.Infof("WaitForNegs(%q, %v, %v, %v) = %v", negName, zones, expectHealthy, expectCount, err)
			return false, nil
		}
		return true, nil
	})
}

// WaitForDistinctHosts waits util
func WaitForDistinctHosts(ctx context.Context, vip string, expectDistinctHosts int, tolerateTransientError bool) error {
	return wait.Poll(negPollInterval, negPollTimeout, func() (bool, error) {
		if err := CheckDistinctResponseHost(vip, expectDistinctHosts, tolerateTransientError); err != nil {
			klog.Infof("WaitForDistinctHosts(%q, %v, %v) = %v", vip, expectDistinctHosts, tolerateTransientError, err)
			return false, nil
		}
		return true, nil
	})
}

// CheckSvcEvents checks to see if the service has an event with the provided msgType and message
func CheckSvcEvents(s *Sandbox, svcName, msgType, message string, ignoreMessages ...string) (bool, error) {
	svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Get(context.TODO(), svcName, metav1.GetOptions{})
	if svc == nil || err != nil {
		return false, fmt.Errorf("failed to get service %s/%s: %v", s.Namespace, svcName, err)
	}

	eventList, err := s.f.Clientset.CoreV1().Events(s.Namespace).Search(Scheme, svc)
	if err != nil {
		return false, fmt.Errorf("failed to get events: %q", err)
	}

	// ignoreMessage returns true if the provided message contains on of the ignoreMessages
	// indicating that the message can be ignored
	ignoreMessage := func(eventMessage string) bool {
		for _, ignoreMsg := range ignoreMessages {
			if strings.Contains(eventMessage, ignoreMsg) {
				return true
			}
		}
		return false
	}

	for _, event := range eventList.Items {
		if event.Type == msgType {
			if strings.Contains(event.Message, message) && !ignoreMessage(event.Message) {
				return true, nil
			}
		}
	}
	return false, nil
}

// CheckGCLB whitebox testing is OK.
func CheckGCLB(gclb *fuzz.GCLB, numForwardingRules int, numBackendServices int) error {
	// Do some cursory checks on the GCP objects.
	if len(gclb.ForwardingRule) != numForwardingRules {
		return fmt.Errorf("got %d forwarding rules, want %d", len(gclb.ForwardingRule), numForwardingRules)
	}
	if len(gclb.BackendService) != numBackendServices {
		return fmt.Errorf("got %d backend services, want %d", len(gclb.BackendService), numBackendServices)
	}

	return nil
}

// CheckDistinctResponseHost issue GET call to the vip for 100 times, parse the reponses and calculate the number of distinct backends.
func CheckDistinctResponseHost(vip string, expectDistinctHosts int, tolerateTransientError bool) error {
	var errs []error
	const repeat = 100
	hosts := sets.NewString()
	for i := 0; i < repeat; i++ {
		res, err := CheckEchoServerResponse(vip)
		if err != nil {
			if tolerateTransientError {
				klog.Infof("ignoring error from vip %q: %v. ", vip, err)
				continue
			}
			errs = append(errs, err)
		}
		hosts.Insert(res.K8sEnv.Pod)
	}
	if hosts.Len() != expectDistinctHosts {
		errs = append(errs, fmt.Errorf("got %v distinct hosts responding vip %q, want %v", hosts.Len(), vip, expectDistinctHosts))
	}
	return utilerrors.NewAggregate(errs)
}

// CheckEchoServerResponse issue a GET call to the vip and return the ResponseBody.
func CheckEchoServerResponse(vip string) (app.ResponseBody, error) {
	url := fmt.Sprintf("http://%s/", vip)
	var body app.ResponseBody
	resp, err := http.Get(url)
	if err != nil {
		return body, fmt.Errorf("failed to GET %q: %v", url, err)
	}
	if resp.StatusCode != 200 {
		return body, fmt.Errorf("GET %q got status code %d, want 200", url, resp.StatusCode)
	}
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return body, fmt.Errorf("failed to read response from GET %q : %v", url, err)
	}
	err = json.Unmarshal(bytes, &body)
	if err != nil {
		return body, fmt.Errorf("failed to marshal response body %s from %q into ResponseBody of echo server: %v", bytes, url, err)
	}
	return body, nil
}

// CheckDeployment checks if the given deployment is in a stable state.
func CheckDeployment(deployment *apps.Deployment) error {
	if deployment.Spec.Replicas == nil {
		return fmt.Errorf("deployment %s/%s has nil replicas: %v", deployment.Namespace, deployment.Name, deployment)
	}

	wantedReplicas := *deployment.Spec.Replicas

	for _, f := range []struct {
		v    *int32
		name string
		want int32
	}{
		{&deployment.Status.Replicas, "replicas", wantedReplicas},
		{&deployment.Status.ReadyReplicas, "ready replicas", wantedReplicas},
		{&deployment.Status.AvailableReplicas, "available replicas", wantedReplicas},
		{&deployment.Status.UpdatedReplicas, "updated replicas", wantedReplicas},
		{&deployment.Status.UnavailableReplicas, "unavailable replicas", 0},
	} {
		if *f.v != f.want {
			return fmt.Errorf("deployment %s/%s has %d %s, want %d", deployment.Namespace, deployment.Name, *f.v, f.name, f.want)
		}
	}
	return nil
}

// CheckNegs checks if the network endpoints in the NEGs is in expected state
func CheckNegs(negs map[meta.Key]*fuzz.NetworkEndpoints, expectHealthy bool, expectCount int) error {
	var (
		count    int
		errs     []error
		negNames []string
	)
	for key, neg := range negs {
		count += len(neg.Endpoints)
		negNames = append(negNames, key.String())

		if expectHealthy {
			for _, ep := range neg.Endpoints {
				json, _ := ep.NetworkEndpoint.MarshalJSON()
				if ep.Healths == nil || len(ep.Healths) != 1 || ep.Healths[0] == nil {
					errs = append(errs, fmt.Errorf("network endpoint %s in NEG %v has health status %v, want 1 health status", json, key.String(), ep.Healths))
					continue
				}

				health := ep.Healths[0].HealthState
				if health != healthyState {
					errs = append(errs, fmt.Errorf("network endpoint %s in NEG %v has health status %q, want %q", json, key.String(), health, healthyState))
				}
			}
		}
	}

	if count != expectCount {
		return fmt.Errorf("NEGs (%v) have a total %v of endpoints, want %v", strings.Join(negNames, "/"), count, expectCount)
	}

	return utilerrors.NewAggregate(errs)
}

// CheckNegStatus checks if the NEG Status annotation is presented and in the expected state
func CheckNegStatus(svc *v1.Service, expectSvcPors []string) (annotations.NegStatus, error) {
	annotation, ok := svc.Annotations[annotations.NEGStatusKey]
	if !ok {
		return annotations.NegStatus{}, fmt.Errorf("service %s/%s does not have neg status annotation: %v", svc.Namespace, svc.Name, svc)
	}

	negStatus, err := annotations.ParseNegStatus(annotation)
	if err != nil {
		return negStatus, fmt.Errorf("service %s/%s has invalid neg status annotation %q: %v", svc.Namespace, svc.Name, annotation, err)
	}

	expectPorts := sets.NewString(expectSvcPors...)
	existingPorts := sets.NewString()
	for port := range negStatus.NetworkEndpointGroups {
		existingPorts.Insert(port)
	}

	if !expectPorts.Equal(existingPorts) {
		return negStatus, fmt.Errorf("service %s/%s does not have neg status annotation: %q, want ports %q", svc.Namespace, svc.Name, annotation, expectPorts.List())
	}
	return negStatus, nil
}

// CheckNameInNegStatus checks if the NEG Status annotation is present and in the expected state
// The parameter expectedNegAttrs will map a port to a neg name. If the the neg name is empty, CheckNameInNegStatus expects
// that the name is autogenerated and will check it
func CheckNameInNegStatus(svc *v1.Service, expectedNegAttrs map[string]string) (annotations.NegStatus, error) {
	annotation, ok := svc.Annotations[annotations.NEGStatusKey]
	if !ok && len(expectedNegAttrs) > 0 {
		return annotations.NegStatus{}, fmt.Errorf("service %s/%s does not have neg status annotation: %+v", svc.Namespace, svc.Name, svc)
	} else if ok && len(expectedNegAttrs) == 0 {
		return annotations.NegStatus{}, fmt.Errorf("service %s/%s should not have neg status annotation: %+v", svc.Namespace, svc.Name, svc)
	} else if !ok && len(expectedNegAttrs) == 0 {
		return annotations.NegStatus{}, nil
	}

	negStatus, err := annotations.ParseNegStatus(annotation)
	if err != nil {
		return negStatus, fmt.Errorf("service %s/%s has invalid neg status annotation %s: %+v", svc.Namespace, svc.Name, annotation, err)
	}

	existingPorts := 0
	for port, negName := range negStatus.NetworkEndpointGroups {
		expectedName, ok := expectedNegAttrs[port]
		if !ok {
			// Port was not expected
			return annotations.NegStatus{}, fmt.Errorf("CheckCustomNegNameStatus: Service: %s, did not expect NEG with port %s", svc.Name, port)
		}
		if expectedName != "" && negName != expectedName {
			return annotations.NegStatus{}, fmt.Errorf("CheckCustomNegNameStatus: Service: %s, with port %s was expected to have name %s", svc.Name, port, expectedName)
		}
		existingPorts += 1
	}

	if existingPorts != len(expectedNegAttrs) {
		return negStatus, fmt.Errorf("service %s/%s does not have neg status annotation: %s, want port:name %+v", svc.Namespace, svc.Name, annotation, expectedNegAttrs)
	}
	return negStatus, nil
}

// CheckForAnyFinalizer asserts that an ingress finalizer exists on Ingress.
func CheckForAnyFinalizer(ing *networkingv1.Ingress) error {
	ingFinalizers := ing.GetFinalizers()
	if l := len(ingFinalizers); l != 1 {
		return fmt.Errorf("expected 1 Finalizer but got %d", l)
	}
	if ingFinalizers[0] != common.FinalizerKey && ingFinalizers[0] != common.FinalizerKeyV2 {
		return fmt.Errorf("unexpected finalizer %q found", ingFinalizers[0])
	}
	return nil
}

// CheckV1Finalizer asserts that only v1 finalizer exists on Ingress.
func CheckV1Finalizer(ing *networkingv1.Ingress) error {
	ingFinalizers := ing.GetFinalizers()
	if l := len(ingFinalizers); l != 1 {
		return fmt.Errorf("expected 1 Finalizer but got %d", l)
	}
	if ingFinalizers[0] != common.FinalizerKey {
		return fmt.Errorf("expected Finalizer %q but got %q", common.FinalizerKey, ingFinalizers[0])
	}
	return nil
}

// CheckV2Finalizer asserts that only v2 finalizer exists on Ingress.
func CheckV2Finalizer(ing *networkingv1.Ingress) error {
	ingFinalizers := ing.GetFinalizers()
	if l := len(ingFinalizers); l != 1 {
		return fmt.Errorf("expected 1 Finalizer but got %d", l)
	}
	if ingFinalizers[0] != common.FinalizerKeyV2 {
		return fmt.Errorf("expected Finalizer %q but got %q", common.FinalizerKeyV2, ingFinalizers[0])
	}
	return nil
}

// WaitDestinationRuleAnnotation waits until the DestinationRule NEG annotation count equal to negCount.
func WaitDestinationRuleAnnotation(s *Sandbox, namespace, name string, negCount int, timeout time.Duration) (*annotations.DestinationRuleNEGStatus, error) {
	var rsl annotations.DestinationRuleNEGStatus
	if err := wait.Poll(5*time.Second, timeout, func() (bool, error) {
		unsDr, err := s.f.DestinationRuleClient.Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		ann := unsDr.GetAnnotations()
		klog.Infof("Wait for DestinationRule NEG annotation, want count: %d, got annotation: %v", negCount, ann)
		if ann != nil {
			if val, ok := ann[annotations.NEGStatusKey]; ok {
				rsl, err = annotations.ParseDestinationRuleNEGStatus(val)
				if err != nil {
					return false, err
				}
				if len(rsl.NetworkEndpointGroups) == negCount {
					return true, nil
				}
			}
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return &rsl, nil
}

// WaitConfigMapEvents waits the msgs messages present for namespace:name ConfigMap until timeout.
func WaitConfigMapEvents(s *Sandbox, namespace, name string, msgs []string, timeout time.Duration) error {
	cm, err := s.f.Clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if cm == nil {
		return fmt.Errorf("Cannot find ConfigMap: %s/%s", namespace, name)
	}
	return wait.Poll(5*time.Second, timeout, func() (bool, error) {
		eventList, err := s.f.Clientset.CoreV1().Events(namespace).Search(Scheme, cm)
		if err != nil {
			return false, err
		}
		if len(eventList.Items) < len(msgs) {
			return false, nil
		}
		allMsg := ""
		events := eventList.Items
		sort.Slice(events, func(i, j int) bool {
			return events[i].LastTimestamp.Before(&events[j].LastTimestamp)
		})
		for _, event := range events[len(events)-len(msgs):] {
			allMsg += event.Message
		}
		klog.Infof("WaitConfigMapEvents, allMsg: %s, want: %v", allMsg, msgs)

		for _, msg := range msgs {
			if !strings.Contains(allMsg, msg) {
				return false, nil
			}
		}
		return true, nil
	})
}

// waitForBackendConfigCRDEstablish waits for backendconfig CRD to be ensured
// by the ingress controller.
func waitForBackendConfigCRDEstablish(crdClient *apiextensionsclient.Clientset) error {
	condition := func() (bool, error) {
		bcCRD, err := crdClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), backendConfigCRDName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				klog.V(3).Infof("CRD %s is not found, retrying", backendConfigCRDName)
				return false, nil
			}
			return false, err
		}
		for _, c := range bcCRD.Status.Conditions {
			if c.Type == apiextensionsv1.Established && c.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
		}
		klog.V(3).Infof("CRD %s is not established, retrying", backendConfigCRDName)
		return false, nil
	}
	if err := wait.Poll(backendConfigEnsurePollInterval, backendConfigEnsurePollTimeout, condition); err != nil {
		return fmt.Errorf("error waiting for CRD established: %v", err)
	}
	return nil
}

// WaitForNegCRs waits up to the gclbDeletionTimeout for  neg crs that have the configurations in expectedNegs, and are owned by the given service name,
// otherwise returns an error. The parameter expectedNegs maps a port to an expected neg name or an empty string for a generated name.
func WaitForNegCRs(s *Sandbox, serviceName string, expectedNegs map[string]string) (annotations.NegStatus, error) {
	var svc *v1.Service

	err := wait.Poll(negPollInterval, negPollTimeout, func() (bool, error) {
		var err error
		svc, err = s.f.Clientset.CoreV1().Services(s.Namespace).Get(context.TODO(), serviceName, metav1.GetOptions{})
		if svc == nil || err != nil {
			return false, fmt.Errorf("failed to get service %s/%s: %v", s.Namespace, serviceName, err)
		}

		svcNegs, err := s.f.SvcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(s.Namespace).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to get negCRs : %s", err)
		}

		err = CheckNegCRs(svc, svcNegs, expectedNegs)
		if err != nil {
			klog.Infof("WaitForCustomNameNegs(%s/%s, %v) = %v", s.Namespace, serviceName, expectedNegs, err)
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return annotations.NegStatus{}, err
	}

	return CheckNameInNegStatus(svc, expectedNegs)
}

// CheckNegCRs will check that the provided neg cr list have negs with the expected neg attributes
func CheckNegCRs(svc *v1.Service, svcNegs *negv1beta1.ServiceNetworkEndpointGroupList, expectedNegAttrs map[string]string) error {
	portsFound := 0
	for _, svcNeg := range svcNegs.Items {
		port := svcNeg.Labels[negtypes.NegCRServicePortKey]
		svcName := svcNeg.Labels[negtypes.NegCRServiceNameKey]
		if svcName != svc.Name {
			// not part of this service, so no need to keep checking
			continue
		}

		expectedName, ok := expectedNegAttrs[port]
		if !ok {
			// Port was not expected
			return fmt.Errorf("CheckNegCRs: Service: %s, did not expect NEG with port %s", svc.Name, port)
		}
		if expectedName != "" && svcNeg.Name != expectedName {
			return fmt.Errorf("CheckNegCRs: Service: %s, with port %s was expected to have name %s", svc.Name, port, expectedName)
		}
		err := CheckNegOwnerRef(svc, svcNeg)
		if err != nil {
			return fmt.Errorf("CheckNegCRs: errored checking neg owner reference: %s", err)
		}

		err = CheckNegFinalizer(svcNeg)
		if err != nil {
			return fmt.Errorf("CheckNegCRs: errored checking neg finalizer: %s", err)
		}

		portsFound += 1
	}

	if portsFound != len(expectedNegAttrs) {
		return fmt.Errorf("missing one or more negs for service %s/%s found %d, want negs %+v", svc.Namespace, svc.Name, portsFound, expectedNegAttrs)
	}
	return nil
}

// WaitForStandaloneNegDeletion waits for standalone NEGs and corresponding CR are deleted via GC.
func WaitForStandaloneNegDeletion(ctx context.Context, c cloud.Cloud, s *Sandbox, port string, negStatus annotations.NegStatus) error {
	negName := negStatus.NetworkEndpointGroups[port]
	return wait.Poll(negPollInterval, negGCPollTimeout, func() (bool, error) {
		if crDeleted, err := CheckDeletedNegCRs(s, negName, port); !crDeleted {
			return false, err
		}

		negsDeleted, err := fuzz.CheckStandaloneNEGDeletion(ctx, c, negName, port, negStatus.Zones)
		if err != nil {
			return false, fmt.Errorf("WaitForStandaloneNegDeletion() error: %s", err)
		} else if !negsDeleted {
			return false, nil
		}
		return true, nil
	})
}

// CheckDeletedNegCRs verifies that the provided neg list does not have negs that are associated with the provided neg atrributes
func CheckDeletedNegCRs(s *Sandbox, negName, port string) (bool, error) {
	svcNeg, err := s.f.SvcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(s.Namespace).Get(context.Background(), negName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		} else if apierrors.IsNotFound(err) {
			klog.Infof("CheckDeletedNegCR() failed querying for neg %s/%s: %s", s.Namespace, negName, err)
			return false, err
		}
	}

	portLabel := svcNeg.Labels[negtypes.NegCRServicePortKey]
	if port != portLabel {
		// If port is different, not the CR that needs to be deleted
		return true, nil
	}
	return false, nil
}

// CheckNegOwnerRef verifies the owner reference on the provided neg cr point to the given service
func CheckNegOwnerRef(svc *v1.Service, svcNeg negv1beta1.ServiceNetworkEndpointGroup) error {
	if len(svcNeg.OwnerReferences) != 1 {
		return fmt.Errorf("CheckNegOwnerRef: neg %s/%s has more than one owner reference", svcNeg.Labels[negtypes.NegCRServicePortKey], svcNeg.Name)
	}

	gvk := schema.GroupVersionKind{Version: "v1", Kind: "Service"}
	expectedOwnerReference := metav1.NewControllerRef(svc, gvk)
	expectedOwnerReference.BlockOwnerDeletion = utilpointer.BoolPtr(false)

	if !reflect.DeepEqual(*expectedOwnerReference, svcNeg.OwnerReferences[0]) {
		return fmt.Errorf("CheckNegOwnerRef: neg %s/%s owner reference is %+v expected %+v", svcNeg.Namespace, svcNeg.Name, svcNeg.OwnerReferences[0], expectedOwnerReference)
	}

	return nil
}

// CheckNegFinalizer asserts that only the Neg finalizer exists on NEG CR.
func CheckNegFinalizer(svcNeg negv1beta1.ServiceNetworkEndpointGroup) error {
	negFinalizers := svcNeg.GetFinalizers()
	if l := len(negFinalizers); l != 1 {
		return fmt.Errorf("expected 1 neg Finalizer but got %d", l)
	}
	if negFinalizers[0] != common.NegFinalizerKey {
		return fmt.Errorf("expected neg Finalizer %q but got %q", common.NegFinalizerKey, negFinalizers[0])
	}
	return nil
}

// WaitForSvcNegErrorEvents waits for at least one of the possibles messages to be emitted on the
// namespace:svcName serice until timeout
func WaitForSvcNegErrorEvents(s *Sandbox, svcName string, possibleMessages []string) error {
	svc, err := s.f.Clientset.CoreV1().Services(s.Namespace).Get(context.TODO(), svcName, metav1.GetOptions{})
	if svc == nil || err != nil {
		return fmt.Errorf("failed to get service %s/%s: %v", s.Namespace, svcName, err)
	}

	return wait.Poll(5*time.Second, negPollTimeout, func() (bool, error) {
		eventList, err := s.f.Clientset.CoreV1().Events(s.Namespace).Search(Scheme, svc)
		if err != nil {
			return false, err
		}

		// Use set to avoid double counting duplicate messages
		foundMessages := sets.NewString()
		for _, event := range eventList.Items {
			if event.Type == v1.EventTypeWarning {

				for _, msg := range possibleMessages {
					if strings.Contains(event.Message, msg) {
						foundMessages.Insert(msg)
						break
					}
				}
			}
		}

		if len(foundMessages) == 0 {
			klog.Infof("WaitForSvcNegErrorEvents(), waiting to find at least one event in %+v, but only found: %+v", possibleMessages, foundMessages)
			return false, nil
		}

		return true, nil
	})
}

// CreateNegCR creates a neg cr with the provided neg name and service port. The neg cr created will not be a valid one
// and is expected to be GC'd by the controller
func CreateNegCR(s *Sandbox, negName string, servicePort string) error {

	neg := negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: negName,
			Labels: map[string]string{
				negtypes.NegCRServicePortKey: servicePort,
			},
		},
	}
	_, err := s.f.SvcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(s.Namespace).Create(context.Background(), &neg, metav1.CreateOptions{})
	return err
}

// DeleteNegCR sends a deletion request for the neg cr with the provided negName in the sandbox's namespace
func DeleteNegCR(s *Sandbox, negName string) error {
	return s.f.SvcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(s.Namespace).Delete(context.Background(), negName, metav1.DeleteOptions{})
}

// WaitForServiceAttachment waits until the gce service attachment corresponding to the provided CR name is
// created and properly configured
func WaitForServiceAttachment(s *Sandbox, saName string) (string, error) {
	var gceSAURL string
	err := wait.Poll(negPollInterval, negPollTimeout, func() (bool, error) {
		saCR, err := s.f.SAClient.NetworkingV1beta1().ServiceAttachments(s.Namespace).Get(context.TODO(), saName, metav1.GetOptions{})
		if saCR == nil || err != nil {
			return false, fmt.Errorf("failed to get service attachment %s/%s: %v", s.Namespace, saName, err)
		}

		sa, err := fuzz.GetServiceAttachment(context.TODO(), s.f.Cloud, saCR.Status.ServiceAttachmentURL)
		if err != nil {
			klog.Infof("WaitForServiceAttachment() failed to retrieve ServiceAttachment %s: %v", saCR.Status.ServiceAttachmentURL, err)
			return false, nil
		}

		if gceSAURL, err = CheckServiceAttachment(sa, saCR); err != nil {
			klog.Infof("WaitForServiceAttachment() failed checking service attachment %s: %v", saCR.Status.ServiceAttachmentURL, err)
			return false, nil
		}

		if err := CheckServiceAttachmentForwardingRule(s, s.f.Cloud, saCR); err != nil {
			klog.Infof("WaitForServiceAttachment(), forwarding rule on service attachment does not match expected: %q", err)
			return false, nil
		}
		klog.Infof("WaitForServiceAttachment(), found ServiceAttachment %s", gceSAURL)
		return true, nil
	})
	return gceSAURL, err
}

// WaitForServiceAttachmentDeletion waits until the Service Attachment CR and resource in GCE has been deleted.
func WaitForServiceAttachmentDeletion(s *Sandbox, saName, gceSAURL string) error {
	return wait.Poll(negPollInterval, negGCPollTimeout, func() (bool, error) {
		if !CheckServiceAttachmentCRDeletion(s, saName) {
			return false, nil
		}

		if gceSAURL != "" {
			deleted, err := fuzz.CheckServiceAttachmentDeletion(context.TODO(), s.f.Cloud, gceSAURL)
			if err != nil {
				klog.Infof("WaitForServiceAttachment(), errored when checking for service attachment deletion in gce: %q", err)
			}
			return deleted, nil
		}
		return true, nil
	})
}

// CheckServiceAttachmentCRDeletion verifes that the CR does not exist
func CheckServiceAttachmentCRDeletion(s *Sandbox, saName string) bool {
	_, err := s.f.SAClient.NetworkingV1beta1().ServiceAttachments(s.Namespace).Get(context.Background(), saName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true
		} else if apierrors.IsNotFound(err) {
			klog.Infof("CheckDeletedNegCR() failed querying for neg %s/%s: %s", s.Namespace, saName, err)
			return false
		}
	}
	return false
}

// CheckServiceAttachment verifes that the CR spec matches the GCE Service Attachment configuration and
// that the CR's Status was properly populated
func CheckServiceAttachment(sa *fuzz.ServiceAttachment, cr *sav1beta1.ServiceAttachment) (string, error) {
	if err := CheckServiceAttachmentFinalizer(cr); err != nil {
		return "", fmt.Errorf("failed checking Service Attachment CR %s/%s: %q", cr.Namespace, cr.Name, err)
	}

	if cr.Status.ServiceAttachmentURL == "" || cr.Status.ForwardingRuleURL == "" {
		return "", fmt.Errorf("Service Attachment CR %s/%s status is not populated", cr.Namespace, cr.Name)
	}

	if sa.Beta.ConnectionPreference != cr.Spec.ConnectionPreference {
		return "", fmt.Errorf("service attachment %s connection preference does not CR %s/%s", sa.Beta.ConnectionPreference, cr.Namespace, cr.Name)
	}

	var subnets []string
	for _, subnetURL := range sa.Beta.NatSubnets {
		resourceID, err := cloud.ParseResourceURL(subnetURL)
		if err != nil {
			return "", fmt.Errorf("unparseable subnet url %s in gce service attachment %s", subnetURL, sa.Beta.Name)
		}
		subnets = append(subnets, resourceID.Key.Name)
	}

	if !utils.EqualStringSets(subnets, cr.Spec.NATSubnets) {
		return "", fmt.Errorf("subnets in gce service attachment %s does not make CR %s/%s", sa.Beta.Name, cr.Namespace, cr.Name)
	}
	return sa.Beta.SelfLink, nil
}

// CheckServiceAttachmentForwardingRule verfies that the forwarding rule used in the GCE Service Attachment creation
// is the same one created by the Service referenced in the CR
func CheckServiceAttachmentForwardingRule(s *Sandbox, c cloud.Cloud, cr *sav1beta1.ServiceAttachment) error {

	svc, err := s.f.Clientset.CoreV1().Services(cr.Namespace).Get(context.TODO(), cr.Spec.ResourceRef.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed getting service %s/%s: %q", s.Namespace, cr.Spec.ResourceRef.Name, err)
	}

	if len(svc.Status.LoadBalancer.Ingress) != 1 {
		return fmt.Errorf("expected service to have one loadbalancer, but found %d", len(svc.Status.LoadBalancer.Ingress))
	}

	fr, err := fuzz.GetForwardingRule(context.TODO(), c, cr.Status.ForwardingRuleURL)
	if err != nil {
		return fmt.Errorf("failed getting forwarding rule %s: %q", cr.Status.ForwardingRuleURL, err)
	}

	if fr.GA.IPAddress != svc.Status.LoadBalancer.Ingress[0].IP {
		return fmt.Errorf("gclb had IP %s, which does not match IP %s in service %s/%s", fr.GA.IPAddress, svc.Status.LoadBalancer.Ingress[0].IP, svc.Namespace, svc.Name)
	}

	return nil
}

// CheckServiceAttachmentFinalizer verifes that the CR has the ServiceAttachment Finalizer
func CheckServiceAttachmentFinalizer(cr *sav1beta1.ServiceAttachment) error {
	finalizers := cr.GetFinalizers()
	if l := len(finalizers); l != 1 {
		return fmt.Errorf("expected 1 finalizer on service attachment but got %d", l)
	}
	if finalizers[0] != psc.ServiceAttachmentFinalizerKey {
		return fmt.Errorf("expected service attachment finalizer %q but got %q", psc.ServiceAttachmentFinalizerKey, finalizers[0])
	}
	return nil
}

// Truncate truncates a gce resource name if it exceeds 62 chars
// This function is based on the one in pkg/namer/namer.go
func Truncate(key string) string {
	if len(key) > 62 {
		// GCE requires names to end with an alphanumeric, but allows
		// characters like '-', so make sure the trucated name ends
		// legally.
		return fmt.Sprintf("%v%v", key[:62], "0")
	}
	return key
}
