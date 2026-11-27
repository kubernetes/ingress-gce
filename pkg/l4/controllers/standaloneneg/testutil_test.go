/*
Copyright 2026 The Kubernetes Authors.

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

package standaloneneg

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/utils/common"
)

func generateForwardingRuleKey(fr string, num int) string {
	var buffer bytes.Buffer
	for i := 1; i <= num; i++ {
		buffer.WriteString(fmt.Sprintf("%s%d,", fr, i))
	}
	return strings.TrimSuffix(buffer.String(), ",")
}

func negSelfLink(project, zone, negName string) string {
	return fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/networkEndpointGroups/%s", project, zone, negName)
}

// Builders of the GCE resources and services used by the Standalone NEG LB external health check test cases.
const (
	snegTestProject = "test-project"
	snegTestRegion  = "us-central1"
	snegTestZone    = "us-central1-a"
	snegTestNEG     = "neg-1"
)

type svcOption func(*v1.Service)

// standaloneSvc returns a Standalone NEG LoadBalancer service "default/svc" referencing the given forwarding rules.
func standaloneSvc(frRefs string, opts ...svcOption) *v1.Service {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "svc",
			Namespace:   "default",
			Annotations: map[string]string{annotations.CustomForwardingRuleKey: frRefs},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func withExternalHCAnnotation(value string) svcOption {
	return func(svc *v1.Service) {
		svc.Annotations[annotations.ExternalHealthCheckKey] = value
	}
}

func withHealthCheckNodePort(port int32) svcOption {
	return func(svc *v1.Service) {
		svc.Spec.HealthCheckNodePort = port
	}
}

func withServicePorts(ports ...v1.ServicePort) svcOption {
	return func(svc *v1.Service) {
		svc.Spec.Ports = ports
	}
}

func tcpPort(port int32) v1.ServicePort {
	return v1.ServicePort{Name: fmt.Sprintf("tcp-%d", port), Protocol: v1.ProtocolTCP, Port: port}
}

func udpPort(port int32) v1.ServicePort {
	return v1.ServicePort{Name: fmt.Sprintf("udp-%d", port), Protocol: v1.ProtocolUDP, Port: port}
}

type frOption func(*composite.ForwardingRule)

// regionalFR returns a regional TCP forwarding rule on port 80 pointing to the regional backend service bs.
func regionalFR(name, ip, bs string, opts ...frOption) *composite.ForwardingRule {
	fr := &composite.ForwardingRule{
		Name:                name,
		IPAddress:           ip,
		BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/%s", snegTestProject, snegTestRegion, bs),
		LoadBalancingScheme: "EXTERNAL",
		IPProtocol:          "TCP",
		Ports:               []string{"80"},
		Scope:               meta.Regional,
		Version:             meta.VersionBeta,
	}
	for _, opt := range opts {
		opt(fr)
	}
	return fr
}

// globalFR returns a global TCP forwarding rule on port 80 pointing to the global backend service bs.
func globalFR(name, bs string, ips ...string) *composite.ForwardingRule {
	return &composite.ForwardingRule{
		Name:                name,
		IPAddresses:         ips,
		BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/global/backendServices/%s", snegTestProject, bs),
		LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
		IPProtocol:          "TCP",
		Ports:               []string{"80"},
		Scope:               meta.Global,
		Version:             meta.VersionBeta,
	}
}

func withFRProtocol(protocol string) frOption {
	return func(fr *composite.ForwardingRule) {
		fr.IPProtocol = protocol
	}
}

func withFRPorts(ports ...string) frOption {
	return func(fr *composite.ForwardingRule) {
		fr.Ports = ports
	}
}

func withAllPorts() frOption {
	return func(fr *composite.ForwardingRule) {
		fr.Ports = nil
		fr.AllPorts = true
	}
}

// regionalBS returns a regional backend service with the test NEG attached and the given health check.
func regionalBS(name, hc string) *composite.BackendService {
	return testBS(name, meta.Regional, hc)
}

// globalBS returns a global backend service with the test NEG attached and the given health check.
func globalBS(name, hc string) *composite.BackendService {
	return testBS(name, meta.Global, hc)
}

// bsWithNEGs sets the backends of the backend service to the given NEGs of the test zone.
func bsWithNEGs(bs *composite.BackendService, negNames ...string) *composite.BackendService {
	bs.Backends = nil
	for _, negName := range negNames {
		bs.Backends = append(bs.Backends, &composite.Backend{Group: negSelfLink(snegTestProject, snegTestZone, negName)})
	}
	return bs
}

// renamedSvcNegs returns the ServiceNetworkEndpointGroups of the test service after its NEG name changed
// from oldNEGName to the test NEG name snegTestNEG. As created by the NEG controller, the
// ServiceNetworkEndpointGroups are named after their NEGs: the one of the new NEG is active, the one of
// the old NEG is being deleted.
func renamedSvcNegs(oldNEGName string) []*negv1beta1.ServiceNetworkEndpointGroup {
	return []*negv1beta1.ServiceNetworkEndpointGroup{
		testSvcNeg(snegTestNEG, negv1beta1.NegObjectReference{SelfLink: negSelfLink(snegTestProject, snegTestZone, snegTestNEG)}),
		deletingSvcNeg(testSvcNeg(oldNEGName, negv1beta1.NegObjectReference{SelfLink: negSelfLink(snegTestProject, snegTestZone, oldNEGName)})),
	}
}

// bsWithHealthCheckURL sets the health check URL of the backend service.
func bsWithHealthCheckURL(bs *composite.BackendService, hcURL string) *composite.BackendService {
	bs.HealthChecks = []string{hcURL}
	return bs
}

func testBS(name string, scope meta.KeyType, hc string) *composite.BackendService {
	hcURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/healthChecks/%s", snegTestProject, snegTestRegion, hc)
	if scope == meta.Global {
		hcURL = fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/global/healthChecks/%s", snegTestProject, hc)
	}
	return &composite.BackendService{
		Name:         name,
		Scope:        scope,
		Version:      meta.VersionBeta,
		HealthChecks: []string{hcURL},
		Backends:     []*composite.Backend{{Group: negSelfLink(snegTestProject, snegTestZone, snegTestNEG)}},
	}
}

func httpHC(name string, port int64) *composite.HealthCheck {
	return &composite.HealthCheck{
		Name:            name,
		Type:            "HTTP",
		Scope:           meta.Regional,
		Version:         meta.VersionBeta,
		HttpHealthCheck: &composite.HTTPHealthCheck{Port: port},
	}
}

func globalHTTPHC(name string, port int64) *composite.HealthCheck {
	hc := httpHC(name, port)
	hc.Scope = meta.Global
	return hc
}

func httpHCWithServingPort(name string) *composite.HealthCheck {
	hc := httpHC(name, 0)
	hc.HttpHealthCheck.PortSpecification = "USE_SERVING_PORT"
	return hc
}

func tcpHC(name string, port int64) *composite.HealthCheck {
	return &composite.HealthCheck{
		Name:           name,
		Type:           "TCP",
		Scope:          meta.Regional,
		Version:        meta.VersionBeta,
		TcpHealthCheck: &composite.TCPHealthCheck{Port: port},
	}
}

// standaloneSvcNegs returns the ServiceNetworkEndpointGroup of the test service with the test NEG.
// Owner references are set by the test runner.
func standaloneSvcNegs(name string) []*negv1beta1.ServiceNetworkEndpointGroup {
	return []*negv1beta1.ServiceNetworkEndpointGroup{
		{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
				NetworkEndpointGroups: []negv1beta1.NegObjectReference{
					{SelfLink: negSelfLink(snegTestProject, snegTestZone, snegTestNEG)},
				},
			},
		},
	}
}

// vmIPSvcNegOf makes the ServiceNetworkEndpointGroup a GCE_VM_IP
// ServiceNetworkEndpointGroup controlled by the service, as created by the NEG
// controller. A service port label set by the test case is kept.
func vmIPSvcNegOf(svcNeg *negv1beta1.ServiceNetworkEndpointGroup, svc *v1.Service) *negv1beta1.ServiceNetworkEndpointGroup {
	svcNeg.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(svc, v1.SchemeGroupVersion.WithKind("Service"))})
	if svcNeg.Labels == nil {
		svcNeg.Labels = map[string]string{}
	}
	svcNeg.Labels[negtypes.NegCRServiceNameKey] = svc.Name
	if _, ok := svcNeg.Labels[negtypes.NegCRServicePortKey]; !ok {
		svcNeg.Labels[negtypes.NegCRServicePortKey] = negtypes.NegCRServicePortVmIp
	}
	return svcNeg
}

// testSvcNeg returns a ServiceNetworkEndpointGroup in the default namespace with the given NEG references.
func testSvcNeg(name string, negRefs ...negv1beta1.NegObjectReference) *negv1beta1.ServiceNetworkEndpointGroup {
	return &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     negv1beta1.ServiceNetworkEndpointGroupStatus{NetworkEndpointGroups: negRefs},
	}
}

// deletingSvcNeg marks the ServiceNetworkEndpointGroup as being deleted, as done by the NEG controller
// for the ServiceNetworkEndpointGroup of a previous NEG name.
func deletingSvcNeg(svcNeg *negv1beta1.ServiceNetworkEndpointGroup) *negv1beta1.ServiceNetworkEndpointGroup {
	now := metav1.Now()
	svcNeg.DeletionTimestamp = &now
	svcNeg.Finalizers = []string{common.NegFinalizerKey}
	return svcNeg
}

func frsByName(frs ...*composite.ForwardingRule) map[string]*composite.ForwardingRule {
	m := make(map[string]*composite.ForwardingRule, len(frs))
	for _, fr := range frs {
		m[fr.Name] = fr
	}
	return m
}

func bssByName(bss ...*composite.BackendService) map[string]*composite.BackendService {
	m := make(map[string]*composite.BackendService, len(bss))
	for _, bs := range bss {
		m[bs.Name] = bs
	}
	return m
}

func hcsByName(hcs ...*composite.HealthCheck) map[string]*composite.HealthCheck {
	m := make(map[string]*composite.HealthCheck, len(hcs))
	for _, hc := range hcs {
		m[hc.Name] = hc
	}
	return m
}

// ipProgrammedCondition returns the externalIPProgrammed condition expected for the given programmed IPs.
func ipProgrammedCondition(ips ...string) *metav1.Condition {
	return &metav1.Condition{
		Type:    "ExternalIPProgrammed",
		Status:  metav1.ConditionTrue,
		Reason:  "IPProgrammed",
		Message: "IPs programmed: " + strings.Join(ips, ", "),
	}
}
