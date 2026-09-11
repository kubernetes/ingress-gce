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

package controllers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	computebeta "google.golang.org/api/compute/v0.beta"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/cloud-provider-gcp/providers/gce"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	ingctx "k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	l4metrics "k8s.io/ingress-gce/pkg/l4/metrics"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	svcnegclientfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

func TestStandaloneNEGLBSync(t *testing.T) {
	l4Namer := namer.NewL4Namer("k8s2-cluster-uid", namer.NewNamer("cluster-id", "firewall-id", klog.TODO()))
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"

	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)
	globalBsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/global/backendServices/bs1", project)

	testCases := []struct {
		desc                string
		svc                 *v1.Service
		frs                 map[string]*composite.ForwardingRule
		bss                 map[string]*composite.BackendService
		hcs                 map[string]*composite.HealthCheck
		svcNegs             []*negv1beta1.ServiceNetworkEndpointGroup
		getBSErr            error
		getHCErr            error
		getHCErrName        string
		expectIPs           []string
		expectedAnnotations map[string]string
		absentAnnotations   []string
		expectEventReasons  []string
		// absentEventReasons are checked after all expectEventReasons are found.
		absentEventReasons []string
		expectError        bool
		expectCondition    *metav1.Condition
	}{
		{
			desc: "Multiple forwarding rules, one missing, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc3",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName + ",missing-fr",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:          []string{frIP},
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100",
			},
		},
		{
			desc: "GCP IPv6 forwarding rule strips /96 from IPAddress string",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-ipv6-strip",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-ipv6",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-ipv6": {
					Name:                "fr-ipv6",
					IPAddress:           "2600:1900:4000:1::/96",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:   []string{"2600:1900:4000:1::"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 2600:1900:4000:1::",
			},
		},
		{
			desc: "GCP IPv6 forwarding rule canonicalizes non-standard IPv6 address",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-ipv6-canonical",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-ipv6-noncanonical",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-ipv6-noncanonical": {
					Name:                "fr-ipv6-noncanonical",
					IPAddress:           "2600:1900:4000:0001:0000:0000:0000:000A/96",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:   []string{"2600:1900:4000:1::a"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 2600:1900:4000:1::a",
			},
		},
		{
			desc: "Dual stack forwarding rules (IPv4 and IPv6)",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-dual-stack",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-ipv4,fr-ipv6",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-ipv4": {
					Name:                "fr-ipv4",
					IPAddress:           "34.1.2.3",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
				"fr-ipv6": {
					Name:                "fr-ipv6",
					IPAddress:           "2600:1900:4000:1::/96",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionGA,
				},
			},
			expectIPs:   []string{"34.1.2.3", "2600:1900:4000:1::"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 34.1.2.3, 2600:1900:4000:1::",
			},
		},
		{
			desc: "Forwarding rule with empty IPAddress does not panic on split",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-empty-ip",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-empty",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-empty": {
					Name:                "fr-empty",
					IPAddress:           "",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
		},
		{
			desc: "Multiple forwarding rules, all missing, error",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc4",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "missing-fr1,missing-fr2",
						annotations.ExternalHealthCheckKey:  "10.0.0.100:12345",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs:         map[string]*composite.ForwardingRule{},
			expectIPs:   nil,
			expectError: true,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
			absentAnnotations: []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc: "Multiple forwarding rules, multiple backend services, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc5",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName + ",fr2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"fr2": {
					Name:                "fr2",
					IPAddress:           "10.0.0.101",
					BackendService:      bsURL + "-2",
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:   []string{frIP, "10.0.0.101"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100, 10.0.0.101",
			},
		},
		{
			desc: "Single forwarding rule with multiple IPs, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-multi-ip",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "global/forwardingRules/" + frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                "global/forwardingRules/" + frName,
					IPAddresses:         []string{"10.0.0.100", "10.0.0.101"},
					BackendService:      globalBsURL,
					LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
					IPProtocol:          "TCP",
					Scope:               meta.Global,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:   []string{"10.0.0.100", "10.0.0.101"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100, 10.0.0.101",
			},
		},
		{
			desc: "Multiple forwarding rules with multiple IPs aggregated, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-multi-fr-multi-ip",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "global/forwardingRules/" + frName + ",global/forwardingRules/fr2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                "global/forwardingRules/" + frName,
					IPAddresses:         []string{"10.0.0.100", "10.0.0.101"},
					BackendService:      globalBsURL,
					LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
					IPProtocol:          "TCP",
					Scope:               meta.Global,
					Version:             meta.VersionBeta,
				},
				"fr2": {
					Name:                "global/forwardingRules/fr2",
					IPAddresses:         []string{"10.0.0.102", "10.0.0.103"},
					BackendService:      globalBsURL,
					LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
					IPProtocol:          "TCP",
					Scope:               meta.Global,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:   []string{"10.0.0.100", "10.0.0.101", "10.0.0.102", "10.0.0.103"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100, 10.0.0.101, 10.0.0.102, 10.0.0.103",
			},
		},
		{
			desc: "Multiple forwarding rules, same backend service, success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc6",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName + ",fr2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"fr2": {
					Name:                "fr2",
					IPAddress:           "10.0.0.101",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:   []string{frIP, "10.0.0.101"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100, 10.0.0.101",
			},
		},
		{
			desc: "Global forwarding rule sync success",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-global",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/global/forwardingRules/global-fr", project),
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"global-fr": {
					Name:                "global-fr",
					IPAddress:           "10.0.0.200",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Global,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs: []string{"10.0.0.200"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.200",
			},
		},
		{
			desc: "Failure when rule has INTERNAL scheme",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-internal-scheme",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "INTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "UnsupportedLBType",
				Message: "The referenced forwarding rule has an unsupported load balancing scheme",
			},
		},
		{
			desc: "Failure when rule protocol is ESP",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-esp-protocol",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
						annotations.ExternalHealthCheckKey:  "10.0.0.100:12345",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "ESP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
			absentAnnotations: []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc: "Success when rule protocol is L3_DEFAULT",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-l3-default",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "L3_DEFAULT",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs: []string{frIP},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100",
			},
		},
		{
			desc: "Missing forwarding rule annotation entirely",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-missing-annotation",
					Namespace: "default",
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "NoForwardingRuleRef",
				Message: "Service is missing the custom forwarding rule reference",
			},
		},
		{
			desc: "Forwarding rule annotation is empty",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-empty-annotation",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "NoForwardingRuleRef",
				Message: "Service is missing the custom forwarding rule reference",
			},
		},
		{
			desc: "Forwarding rule annotation parses to nothing",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-parses-to-nothing",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: ",  , ",
						annotations.ExternalHealthCheckKey:  "10.0.0.100:12345",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "NoForwardingRuleRef",
				Message: "Service is missing the custom forwarding rule reference",
			},
			absentAnnotations: []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc: "Forwarding rule annotation has invalid URL",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-invalid-url",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "invalid/url/format",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
		},
		{
			desc: "Clear status ingress IP when Service type is not LoadBalancer",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-not-lb",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
						annotations.ExternalHealthCheckKey:  "1.2.3.4:12345",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeClusterIP,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "ExternalIPProgrammed",
							Status: metav1.ConditionTrue,
							Reason: "IPProgrammed",
						},
					},
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectError:       false,
			expectCondition:   nil,
			absentAnnotations: []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc: "Clear status ingress IP when CustomForwardingRuleKey annotation is missing",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-missing-annotation",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.ExternalHealthCheckKey: "1.2.3.4:12345",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "ExternalIPProgrammed",
							Status: metav1.ConditionTrue,
							Reason: "IPProgrammed",
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "NoForwardingRuleRef",
				Message: "Service is missing the custom forwarding rule reference",
			},
			absentAnnotations: []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc: "Clear status ingress IP when forwarding rule protocol is ESP (unusable)",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-esp",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: frName,
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "ExternalIPProgrammed",
							Status: metav1.ConditionTrue,
							Reason: "IPProgrammed",
						},
					},
				},
			},
			frs: map[string]*composite.ForwardingRule{
				frName: {
					Name:                frName,
					IPAddress:           frIP,
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "ESP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
		},
		{
			desc: "Clear status ingress IP when CustomForwardingRuleKey annotation parses to nothing",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-parses-to-nothing",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: ",  , ",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "ExternalIPProgrammed",
							Status: metav1.ConditionTrue,
							Reason: "IPProgrammed",
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"NoForwardingRuleRef"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "NoForwardingRuleRef",
				Message: "Service is missing the custom forwarding rule reference",
			},
		},
		{
			desc: "Clear status ingress IP when CustomForwardingRuleKey annotation has an invalid URL",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-clear-invalid-url",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "invalid/url/format",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
				Status: v1.ServiceStatus{
					LoadBalancer: v1.LoadBalancerStatus{
						Ingress: []v1.LoadBalancerIngress{
							{IP: "1.2.3.4"},
						},
					},
					Conditions: []metav1.Condition{
						{
							Type:   "ExternalIPProgrammed",
							Status: metav1.ConditionTrue,
							Reason: "IPProgrammed",
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
		},
		{
			desc: "Too many forwarding rules, skip remaining",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc3",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: generateForwardingRuleKey("custom-fr-", 12),
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"custom-fr-1": {
					Name:                "custom-fr-1",
					IPAddress:           "10.0.0.100",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-2": {
					Name:                "custom-fr-2",
					IPAddress:           "10.0.0.101",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-3": {
					Name:                "custom-fr-3",
					IPAddress:           "10.0.0.102",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-4": {
					Name:                "custom-fr-4",
					IPAddress:           "10.0.0.103",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-5": {
					Name:                "custom-fr-5",
					IPAddress:           "10.0.0.104",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-6": {
					Name:                "custom-fr-6",
					IPAddress:           "10.0.0.105",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-7": {
					Name:                "custom-fr-7",
					IPAddress:           "10.0.0.106",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-8": {
					Name:                "custom-fr-8",
					IPAddress:           "10.0.0.107",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-9": {
					Name:                "custom-fr-9",
					IPAddress:           "10.0.0.108",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-10": {
					Name:                "custom-fr-10",
					IPAddress:           "10.0.0.109",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-11": {
					Name:                "custom-fr-11",
					IPAddress:           "10.0.0.110",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"custom-fr-12": {
					Name:                "custom-fr-12",
					IPAddress:           "10.0.0.111",
					BackendService:      bsURL,
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectError:        false,
			expectEventReasons: []string{"ForwardingRulesLimitExceeded", "SyncLoadBalancerSuccessful"},
			// Note the alphabetical order.
			expectIPs: []string{"10.0.0.100", "10.0.0.109", "10.0.0.110", "10.0.0.111", "10.0.0.101", "10.0.0.102", "10.0.0.103", "10.0.0.104", "10.0.0.105", "10.0.0.106"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100, 10.0.0.109, 10.0.0.110, 10.0.0.111, 10.0.0.101, 10.0.0.102, 10.0.0.103, 10.0.0.104, 10.0.0.105, 10.0.0.106",
			},
		},
		{
			desc: "Forwarding Rule with missing BackendService URL -> expect validation error",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-missing-bs",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-missing-bs",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-missing-bs": {
					Name:                "fr-missing-bs",
					IPAddress:           frIP,
					BackendService:      "",
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
		},
		{
			desc: "Forwarding Rule with non-existent Backend Service (404) -> expect validation error",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-404-bs",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-404-bs",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-404-bs": {
					Name:                "fr-404-bs",
					IPAddress:           frIP,
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/non-existent-bs", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			bss:                map[string]*composite.BackendService{},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "InvalidForwardingRule",
				Message: "The custom forwarding rule reference is invalid",
			},
		},
		{
			desc: "Forwarding Rule with Backend Service returning non-404 system error (500) -> expect ProviderError condition reason",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-500-bs",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-500-bs",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-500-bs": {
					Name:                "fr-500-bs",
					IPAddress:           frIP,
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-500", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			bss: map[string]*composite.BackendService{
				"bs-500": {
					Name:    "bs-500",
					Scope:   meta.Regional,
					Version: meta.VersionBeta,
				},
			},
			getBSErr:           &googleapi.Error{Code: http.StatusInternalServerError, Message: "Internal Server Error"},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "ProviderError",
				Message: "GCE provider error encountered while retrieving forwarding rules",
			},
		},
		{
			desc: "Forwarding Rule with Backend Service whose backends belong to a different Service -> expect validation error",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-diff-neg",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-diff-neg",
						annotations.ExternalHealthCheckKey:  "10.0.0.100:12345",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-diff-neg": {
					Name:                "fr-diff-neg",
					IPAddress:           frIP,
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-other", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			bss: map[string]*composite.BackendService{
				"bs-other": {
					Name:    "bs-other",
					Scope:   meta.Regional,
					Version: meta.VersionBeta,
					Backends: []*composite.Backend{
						{
							Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/other-svc-neg", project),
						},
					},
				},
			},
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            l4Namer.L4Backend("default", "svc-diff-neg"),
						Namespace:       "default",
						OwnerReferences: []metav1.OwnerReference{{Kind: "Service", Name: "svc-diff-neg"}},
					},
					Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: []negv1beta1.NegObjectReference{
							{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/my-svc-neg", project)},
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "BackendNotAttached",
				Message: "The service NEGs are not attached to the load balancer backend service",
			},
			absentAnnotations: []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc: "Forwarding Rule with Backend Service matching target Service NEG via SvcNeg CRD informer -> expect VIP programmed successfully",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-match-crd",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-match-crd",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-match-crd": {
					Name:                "fr-match-crd",
					IPAddress:           frIP,
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-crd", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			bss: map[string]*composite.BackendService{
				"bs-crd": {
					Name:    "bs-crd",
					Scope:   meta.Regional,
					Version: meta.VersionBeta,
					Backends: []*composite.Backend{
						{
							Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/crd-neg-1", project),
						},
					},
				},
			},
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      l4Namer.L4Backend("default", "svc-match-crd"),
						Namespace: "default",
					},
					Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: []negv1beta1.NegObjectReference{
							{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/crd-neg-1", project)},
						},
					},
				},
			},
			expectIPs:   []string{frIP},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100",
			},
		},
		{
			desc: "Forwarding Rule with Backend Service matching target Service NEG via SvcNeg CRD -> expect VIP programmed successfully",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-match-ann",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-match-ann",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-match-ann": {
					Name:                "fr-match-ann",
					IPAddress:           frIP,
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-ann", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			bss: map[string]*composite.BackendService{
				"bs-ann": {
					Name:    "bs-ann",
					Scope:   meta.Regional,
					Version: meta.VersionBeta,
					Backends: []*composite.Backend{
						{
							Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/ann-neg-1", project),
						},
					},
				},
			},
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      l4Namer.L4Backend("default", "svc-match-ann"),
						Namespace: "default",
					},
					Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: []negv1beta1.NegObjectReference{
							{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/ann-neg-1", project)},
						},
					},
				},
			},
			expectIPs:   []string{frIP},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.100",
			},
		},
		{
			desc: "Multiple Forwarding Rules pointing to the same Backend Service -> verify deduplication",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-dedup",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-dedup-1,fr-dedup-2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-dedup-1": {
					Name:                "fr-dedup-1",
					IPAddress:           "10.0.0.1",
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-dedup", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"fr-dedup-2": {
					Name:                "fr-dedup-2",
					IPAddress:           "10.0.0.2",
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-dedup", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			bss: map[string]*composite.BackendService{
				"bs-dedup": {
					Name:    "bs-dedup",
					Scope:   meta.Regional,
					Version: meta.VersionBeta,
					Backends: []*composite.Backend{
						{
							Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/dedup-neg", project),
						},
					},
				},
			},
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      l4Namer.L4Backend("default", "svc-dedup"),
						Namespace: "default",
					},
					Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: []negv1beta1.NegObjectReference{
							{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/dedup-neg", project)},
						},
					},
				},
			},
			expectIPs:   []string{"10.0.0.1", "10.0.0.2"},
			expectError: false,
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionTrue,
				Reason:  "IPProgrammed",
				Message: "IPs programmed: 10.0.0.1, 10.0.0.2",
			},
		},
		{
			desc: "Multiple Forwarding Rules pointing to the same failing Backend Service -> verify deduplication",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "svc-dedup-failing",
					Namespace: "default",
					Annotations: map[string]string{
						annotations.CustomForwardingRuleKey: "fr-dedup-fail-1,fr-dedup-fail-2",
					},
				},
				Spec: v1.ServiceSpec{
					Type:              v1.ServiceTypeLoadBalancer,
					LoadBalancerClass: &lbClass,
				},
			},
			frs: map[string]*composite.ForwardingRule{
				"fr-dedup-fail-1": {
					Name:                "fr-dedup-fail-1",
					IPAddress:           "10.0.0.1",
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-dedup-failing", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
				"fr-dedup-fail-2": {
					Name:                "fr-dedup-fail-2",
					IPAddress:           "10.0.0.2",
					BackendService:      fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs-dedup-failing", project, region),
					LoadBalancingScheme: "EXTERNAL",
					IPProtocol:          "TCP",
					Scope:               meta.Regional,
					Version:             meta.VersionBeta,
				},
			},
			bss: map[string]*composite.BackendService{
				"bs-dedup-failing": {
					Name:    "bs-dedup-failing",
					Scope:   meta.Regional,
					Version: meta.VersionBeta,
					Backends: []*composite.Backend{
						{
							Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/other-svc-neg", project),
						},
					},
				},
			},
			svcNegs: []*negv1beta1.ServiceNetworkEndpointGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      l4Namer.L4Backend("default", "svc-dedup-failing"),
						Namespace: "default",
					},
					Status: negv1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: []negv1beta1.NegObjectReference{
							{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/my-svc-neg", project)},
						},
					},
				},
			},
			expectIPs:          nil,
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "BackendNotAttached",
				Message: "The service NEGs are not attached to the load balancer backend service",
			},
		},
		{
			desc:                "Expect external health checks annotation",
			svc:                 standaloneSvc(frName),
			frs:                 frsByName(regionalFR(frName, frIP, "bs")),
			bss:                 bssByName(regionalBS("bs", "hc")),
			hcs:                 hcsByName(httpHC("hc", 12345)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{frIP},
			expectEventReasons:  []string{"UpdateExternalHealthCheckSuccessful"},
			expectCondition:     ipProgrammedCondition(frIP),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345"},
		},
		{
			desc: "Expect external health checks annotation multiple IPs",
			svc:  standaloneSvc("fr1,fr2,global/forwardingRules/fr3"),
			frs: frsByName(
				regionalFR("fr1", "10.0.0.100", "bs1"),
				regionalFR("fr2", "10.0.0.101", "bs2"),
				globalFR("fr3", "bs3", "10.0.0.102", "10.0.0.103"),
			),
			bss:                 bssByName(regionalBS("bs1", "hc"), regionalBS("bs2", "hc2"), globalBS("bs3", "hc3")),
			hcs:                 hcsByName(httpHC("hc", 12345), httpHC("hc2", 12346), globalHTTPHC("hc3", 12347)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{"10.0.0.100", "10.0.0.101", "10.0.0.102", "10.0.0.103"},
			expectEventReasons:  []string{"UpdateExternalHealthCheckSuccessful"},
			expectCondition:     ipProgrammedCondition("10.0.0.100", "10.0.0.101", "10.0.0.102", "10.0.0.103"),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345,10.0.0.101:12346,10.0.0.102:12347,10.0.0.103:12347"},
		},
		{
			desc:               "External health checks port collision with a service TCP port",
			svc:                standaloneSvc(frName, withExternalHCAnnotation("10.0.0.100:12345"), withServicePorts(tcpPort(12345))),
			frs:                frsByName(regionalFR(frName, frIP, "bs")),
			bss:                bssByName(regionalBS("bs", "hc")),
			hcs:                hcsByName(httpHC("hc", 12345)),
			svcNegs:            standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:          []string{frIP},
			expectEventReasons: []string{"PortCollision"},
			expectCondition:    ipProgrammedCondition(frIP),
			absentAnnotations:  []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc:                "External health checks port collision skips only the colliding health check ports",
			svc:                 standaloneSvc("fr1,fr2", withServicePorts(tcpPort(12345))),
			frs:                 frsByName(regionalFR("fr1", "10.0.0.100", "bs1"), regionalFR("fr2", "10.0.0.101", "bs2")),
			bss:                 bssByName(regionalBS("bs1", "hc"), regionalBS("bs2", "hc2")),
			hcs:                 hcsByName(httpHC("hc", 12345), httpHC("hc2", 12346)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{"10.0.0.100", "10.0.0.101"},
			expectEventReasons:  []string{"PortCollision", "UpdateExternalHealthCheckSuccessful"},
			expectCondition:     ipProgrammedCondition("10.0.0.100", "10.0.0.101"),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.101:12346"},
		},
		{
			desc:                "Service UDP port does not collide with the HTTP health check port",
			svc:                 standaloneSvc(frName, withServicePorts(udpPort(12345))),
			frs:                 frsByName(regionalFR(frName, frIP, "bs", withFRProtocol("UDP"))),
			bss:                 bssByName(regionalBS("bs", "hc")),
			hcs:                 hcsByName(httpHC("hc", 12345)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{frIP},
			expectEventReasons:  []string{"SyncLoadBalancerSuccessful"},
			absentEventReasons:  []string{"PortCollision"},
			expectCondition:     ipProgrammedCondition(frIP),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345"},
		},
		{
			desc:                "L3_DEFAULT forwarding rule with all ports gets the external health checks annotation",
			svc:                 standaloneSvc(frName, withServicePorts(tcpPort(80), udpPort(53))),
			frs:                 frsByName(regionalFR(frName, frIP, "bs", withFRProtocol("L3_DEFAULT"), withAllPorts())),
			bss:                 bssByName(regionalBS("bs", "hc")),
			hcs:                 hcsByName(httpHC("hc", 12345)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{frIP},
			expectEventReasons:  []string{"SyncLoadBalancerSuccessful"},
			absentEventReasons:  []string{"PortCollision"},
			expectCondition:     ipProgrammedCondition(frIP),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345"},
		},
		{
			desc: "Forwarding rules sharing a VIP produce a single external health checks entry",
			svc:  standaloneSvc("fr-tcp,fr-udp", withServicePorts(tcpPort(8080), udpPort(53))),
			frs: frsByName(
				regionalFR("fr-tcp", frIP, "bs", withFRPorts("8080")),
				regionalFR("fr-udp", frIP, "bs", withFRProtocol("UDP"), withFRPorts("53")),
			),
			bss:                 bssByName(regionalBS("bs", "hc")),
			hcs:                 hcsByName(httpHC("hc", 12345)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{frIP, frIP},
			expectEventReasons:  []string{"UpdateExternalHealthCheckSuccessful"},
			expectCondition:     ipProgrammedCondition(frIP, frIP),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345"},
		},
		{
			desc: "Forwarding rules sharing a VIP skip a health check port colliding with a service TCP port",
			svc:  standaloneSvc("fr-tcp,fr-udp", withExternalHCAnnotation("10.0.0.100:8080"), withServicePorts(tcpPort(8080), udpPort(53))),
			frs: frsByName(
				regionalFR("fr-tcp", frIP, "bs", withFRPorts("8080")),
				regionalFR("fr-udp", frIP, "bs", withFRProtocol("UDP"), withFRPorts("53")),
			),
			bss:                bssByName(regionalBS("bs", "hc")),
			hcs:                hcsByName(httpHC("hc", 8080)),
			svcNegs:            standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:          []string{frIP, frIP},
			expectEventReasons: []string{"PortCollision"},
			expectCondition:    ipProgrammedCondition(frIP, frIP),
			absentAnnotations:  []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc:                "External health checks annotation skips health checks targeting the service health check node port",
			svc:                 standaloneSvc("fr1,fr2", withHealthCheckNodePort(30123)),
			frs:                 frsByName(regionalFR("fr1", "10.0.0.100", "bs1"), regionalFR("fr2", "10.0.0.101", "bs2")),
			bss:                 bssByName(regionalBS("bs1", "hc"), regionalBS("bs2", "hc2")),
			hcs:                 hcsByName(httpHC("hc", 30123), httpHC("hc2", 12345)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{"10.0.0.100", "10.0.0.101"},
			expectEventReasons:  []string{"UpdateExternalHealthCheckSuccessful", "SyncLoadBalancerSuccessful"},
			absentEventReasons:  []string{"PortCollision"},
			expectCondition:     ipProgrammedCondition("10.0.0.100", "10.0.0.101"),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.101:12345"},
		},
		{
			desc:               "No external health checks annotation when the health check targets the service health check node port",
			svc:                standaloneSvc(frName, withExternalHCAnnotation("10.0.0.100:30123"), withHealthCheckNodePort(30123)),
			frs:                frsByName(regionalFR(frName, frIP, "bs")),
			bss:                bssByName(regionalBS("bs", "hc")),
			hcs:                hcsByName(httpHC("hc", 30123)),
			svcNegs:            standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:          []string{frIP},
			expectEventReasons: []string{"SyncLoadBalancerSuccessful"},
			absentEventReasons: []string{"PortCollision"},
			expectCondition:    ipProgrammedCondition(frIP),
			absentAnnotations:  []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc:                "External health checks annotation kept when getting the health check fails",
			svc:                 standaloneSvc(frName, withExternalHCAnnotation("10.0.0.100:12345")),
			frs:                 frsByName(regionalFR(frName, frIP, "bs")),
			bss:                 bssByName(regionalBS("bs", "hc")),
			hcs:                 hcsByName(httpHC("hc", 12345)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			getHCErr:            &googleapi.Error{Code: http.StatusInternalServerError, Message: "Internal Server Error"},
			expectIPs:           []string{frIP},
			expectError:         true,
			expectEventReasons:  []string{"ExternalHealthCheckFailed"},
			expectCondition:     ipProgrammedCondition(frIP),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345"},
		},
		{
			desc:               "External health checks annotation removed when the health check is not found",
			svc:                standaloneSvc(frName, withExternalHCAnnotation("10.0.0.100:12345")),
			frs:                frsByName(regionalFR(frName, frIP, "bs")),
			bss:                bssByName(regionalBS("bs", "hc")),
			svcNegs:            standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:          []string{frIP},
			expectEventReasons: []string{"NoHTTPHealthChecks"},
			expectCondition:    ipProgrammedCondition(frIP),
			absentAnnotations:  []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc:               "External health checks annotation removed for non HTTP health check",
			svc:                standaloneSvc(frName, withExternalHCAnnotation("10.0.0.100:12345")),
			frs:                frsByName(regionalFR(frName, frIP, "bs")),
			bss:                bssByName(regionalBS("bs", "hc")),
			hcs:                hcsByName(tcpHC("hc", 12345)),
			svcNegs:            standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:          []string{frIP},
			expectEventReasons: []string{"NoHTTPHealthChecks"},
			expectCondition:    ipProgrammedCondition(frIP),
			absentAnnotations:  []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc:               "External health checks annotation removed for HTTP health check without a fixed port",
			svc:                standaloneSvc(frName, withExternalHCAnnotation("10.0.0.100:12345")),
			frs:                frsByName(regionalFR(frName, frIP, "bs")),
			bss:                bssByName(regionalBS("bs", "hc")),
			hcs:                hcsByName(httpHCWithServingPort("hc")),
			svcNegs:            standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:          []string{frIP},
			expectEventReasons: []string{"NoHTTPHealthChecks"},
			expectCondition:    ipProgrammedCondition(frIP),
			absentAnnotations:  []string{annotations.ExternalHealthCheckKey},
		},
		{
			desc:                "External health checks annotation not updated when getting one of the health checks fails",
			svc:                 standaloneSvc("fr1,fr2", withExternalHCAnnotation("10.0.0.100:12345,10.0.0.101:12346")),
			frs:                 frsByName(regionalFR("fr1", "10.0.0.100", "bs1"), regionalFR("fr2", "10.0.0.101", "bs2")),
			bss:                 bssByName(regionalBS("bs1", "hc"), regionalBS("bs2", "hc2")),
			hcs:                 hcsByName(httpHC("hc", 12345), httpHC("hc2", 12346)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			getHCErr:            &googleapi.Error{Code: http.StatusInternalServerError, Message: "Internal Server Error"},
			getHCErrName:        "hc2",
			expectIPs:           []string{"10.0.0.100", "10.0.0.101"},
			expectError:         true,
			expectEventReasons:  []string{"ExternalHealthCheckFailed"},
			expectCondition:     ipProgrammedCondition("10.0.0.100", "10.0.0.101"),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345,10.0.0.101:12346"},
		},
		{
			desc:                "External health checks annotation uses brackets for IPv6 addresses",
			svc:                 standaloneSvc("fr1,fr2"),
			frs:                 frsByName(regionalFR("fr1", "10.0.0.100", "bs"), regionalFR("fr2", "2001:db8::1/96", "bs")),
			bss:                 bssByName(regionalBS("bs", "hc")),
			hcs:                 hcsByName(httpHC("hc", 12345)),
			svcNegs:             standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			expectIPs:           []string{"10.0.0.100", "2001:db8::1"},
			expectEventReasons:  []string{"UpdateExternalHealthCheckSuccessful"},
			expectCondition:     ipProgrammedCondition("10.0.0.100", "2001:db8::1"),
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345,[2001:db8::1]:12345"},
		},
		{
			desc:               "External health checks annotation not removed on non user forwarding rule errors",
			svc:                standaloneSvc(frName, withExternalHCAnnotation("10.0.0.100:12345")),
			frs:                frsByName(regionalFR(frName, frIP, "bs")),
			bss:                bssByName(regionalBS("bs", "hc")),
			hcs:                hcsByName(httpHC("hc", 12345)),
			svcNegs:            standaloneSvcNegs(l4Namer.L4Backend("default", "svc")),
			getBSErr:           &googleapi.Error{Code: http.StatusInternalServerError, Message: "Internal Server Error"},
			expectError:        true,
			expectEventReasons: []string{"ForwardingRuleUnusable"},
			expectCondition: &metav1.Condition{
				Type:    "ExternalIPProgrammed",
				Status:  metav1.ConditionFalse,
				Reason:  "ProviderError",
				Message: "GCE provider error encountered while retrieving forwarding rules",
			},
			expectedAnnotations: map[string]string{annotations.ExternalHealthCheckKey: "10.0.0.100:12345"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset()
			fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())

			// Populate fakeGCE client with forwarding rules defined in each test case
			for name, fr := range tc.frs {
				key, err := composite.CreateKey(fakeGCE, name, fr.Scope)
				if err != nil {
					t.Fatalf("Failed to create key for forwarding rule %s: %v", name, err)
				}
				err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to create forwarding rule %s: %v", name, err)
				}
			}

			// If tc.bss is nil and tc.frs has rules with BackendService, populate default matching BS and SvcNeg for existing test cases
			if tc.bss == nil && len(tc.frs) > 0 {
				defaultBSMap := make(map[string]*composite.BackendService)
				for _, fr := range tc.frs {
					if fr.BackendService != "" {
						if resID, err := cloud.ParseResourceURL(fr.BackendService); err == nil && resID.Key != nil {
							defaultBSMap[resID.Key.Name] = &composite.BackendService{
								Name:    resID.Key.Name,
								Scope:   resID.Key.Type(),
								Version: meta.VersionBeta,
								Backends: []*composite.Backend{
									{
										Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/default-neg", project),
									},
								},
							}
						}
					}
				}
				if len(defaultBSMap) > 0 {
					tc.bss = defaultBSMap
					if tc.svcNegs == nil && tc.svc != nil {
						svcNegName := l4Namer.L4Backend(tc.svc.Namespace, tc.svc.Name)
						tc.svcNegs = []*negv1beta1.ServiceNetworkEndpointGroup{
							test.NewSvcNeg(types.NamespacedName{Namespace: tc.svc.Namespace, Name: svcNegName}, negv1beta1.ServiceNetworkEndpointGroupStatus{
								NetworkEndpointGroups: []negv1beta1.NegObjectReference{
									{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/default-neg", project)},
								},
							}),
						}
					}
				}
			}

			// Populate fakeGCE client with health checks defined in each test case
			for name, hc := range tc.hcs {
				key, err := composite.CreateKey(fakeGCE, name, hc.Scope)
				t.Logf("key: %v", key)
				if err != nil {
					t.Fatalf("Failed to create key for health check %s: %v", name, err)
				}
				err = composite.CreateHealthCheck(fakeGCE, key, hc, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to create health check %s: %v", name, err)
				}
			}

			// Populate fakeGCE client with backend services defined in each test case
			for name, bs := range tc.bss {
				key, err := composite.CreateKey(fakeGCE, name, bs.Scope)
				if err != nil {
					t.Fatalf("Failed to create key for backend service %s: %v", name, err)
				}
				err = composite.CreateBackendService(fakeGCE, key, bs, klog.TODO())
				if err != nil {
					t.Fatalf("Failed to create backend service %s: %v", name, err)
				}
			}

			bsGetCount := 0
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			mockGCE.MockBetaRegionBackendServices.GetHook = func(ctx context.Context, key *meta.Key, m *cloud.MockBetaRegionBackendServices, options ...cloud.Option) (bool, *computebeta.BackendService, error) {
				if tc.getBSErr != nil {
					return true, nil, tc.getBSErr
				}
				if strings.HasPrefix(key.Name, "bs-dedup") {
					bsGetCount++
				}
				return false, nil, nil
			}
			mockGCE.MockBetaRegionHealthChecks.GetHook = func(ctx context.Context, key *meta.Key, m *cloud.MockBetaRegionHealthChecks, options ...cloud.Option) (bool, *computebeta.HealthCheck, error) {
				if tc.getHCErr != nil && (tc.getHCErrName == "" || tc.getHCErrName == key.Name) {
					return true, nil, tc.getHCErr
				}
				return false, nil, nil
			}

			stopCh := make(chan struct{})
			defer close(stopCh)

			ctxConfig := ingctx.ControllerContextConfig{Namespace: v1.NamespaceAll}
			svcNegClient := svcnegclientfake.NewSimpleClientset()
			c, err := ingctx.NewControllerContext(kubeClient, nil, nil, nil, svcNegClient, nil, nil, nil, nil, nil, kubeClient, fakeGCE, l4Namer.Namer, "k8s2-cluster-uid", ctxConfig, klog.TODO())
			if err != nil {
				t.Fatalf("Failed to create controller context: %v", err)
			}
			c.L4Namer = l4Namer

			for _, svcneg := range tc.svcNegs {
				svcneg.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(tc.svc, tc.svc.GroupVersionKind())})
				c.SvcNegInformer.GetIndexer().Add(svcneg)
				c.SvcNegClient.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcneg.Namespace).Create(context.TODO(), svcneg, metav1.CreateOptions{})
			}

			lc := NewStandaloneNEGLBController(c, stopCh, klog.TODO())

			// Add service to informer
			c.ServiceInformer.GetIndexer().Add(tc.svc)
			// Also add to fake kube client so updateStatus works
			kubeClient.CoreV1().Services(tc.svc.Namespace).Create(context.TODO(), tc.svc, metav1.CreateOptions{})

			key := tc.svc.Namespace + "/" + tc.svc.Name
			err = lc.syncWrapper(key)
			if (err != nil) != tc.expectError {
				t.Errorf("sync() error = %v, expectError %v", err, tc.expectError)
			}

			if strings.Contains(tc.desc, "verify deduplication") {
				if bsGetCount != 1 {
					t.Errorf("Expected BackendService bs-dedup to be fetched 1 time, got %d", bsGetCount)
				}
			}

			updatedSvc, _ := kubeClient.CoreV1().Services(tc.svc.Namespace).Get(context.TODO(), tc.svc.Name, metav1.GetOptions{})

			if len(updatedSvc.Status.LoadBalancer.Ingress) != len(tc.expectIPs) {
				t.Errorf("Expected %d ingress IPs, got %d", len(tc.expectIPs), len(updatedSvc.Status.LoadBalancer.Ingress))
			} else {
				for i, ip := range tc.expectIPs {
					if updatedSvc.Status.LoadBalancer.Ingress[i].IP != ip {
						t.Errorf("Expected IP %s, got %v", ip, updatedSvc.Status.LoadBalancer.Ingress[i].IP)
					}
				}
			}
			for k, v := range tc.expectedAnnotations {
				av, ok := updatedSvc.Annotations[k]
				if !ok {
					t.Errorf("Expected annotation %s to be present", k)
				} else if av != v {
					t.Errorf("Expected annotation %s:%s, got %s:%s", k, v, k, av)
				}
			}
			for _, k := range tc.absentAnnotations {
				if av, ok := updatedSvc.Annotations[k]; ok {
					t.Errorf("Expected annotation %s to be absent, got %s:%s", k, k, av)
				}
			}

			if len(tc.expectEventReasons) > 0 {
				for _, expectedReason := range tc.expectEventReasons {
					expectedReason := expectedReason
					err := wait.PollUntilContextTimeout(context.Background(), 10*time.Millisecond, 5*time.Second, true,
						func(ctx context.Context) (bool, error) {
							events, err := kubeClient.CoreV1().Events(tc.svc.Namespace).List(ctx, metav1.ListOptions{})
							if err != nil {
								return false, err
							}
							for _, e := range events.Items {
								if e.Reason == expectedReason {
									return true, nil
								}
							}
							return false, nil
						})
					if err != nil {
						t.Errorf("Expected %s event, but none found within timeout", expectedReason)
					}
				}
			}
			if len(tc.absentEventReasons) > 0 {
				events, err := kubeClient.CoreV1().Events(tc.svc.Namespace).List(context.Background(), metav1.ListOptions{})
				if err != nil {
					t.Fatalf("Failed to list events: %v", err)
				}
				for _, e := range events.Items {
					if slices.Contains(tc.absentEventReasons, e.Reason) {
						t.Errorf("Unexpected %s event: %s", e.Reason, e.Message)
					}
				}
			}

			// Assert conditions
			if tc.expectCondition != nil {
				var found *metav1.Condition
				for i := range updatedSvc.Status.Conditions {
					if updatedSvc.Status.Conditions[i].Type == tc.expectCondition.Type {
						found = &updatedSvc.Status.Conditions[i]
						break
					}
				}
				if found == nil {
					t.Errorf("Expected condition %s not found", tc.expectCondition.Type)
				} else {
					if found.Status != tc.expectCondition.Status {
						t.Errorf("Expected condition status %v, got %v", tc.expectCondition.Status, found.Status)
					}
					if found.Reason != tc.expectCondition.Reason {
						t.Errorf("Expected condition reason %s, got %s", tc.expectCondition.Reason, found.Reason)
					}
					if found.Message != tc.expectCondition.Message {
						t.Errorf("Expected condition message %q, got %q", tc.expectCondition.Message, found.Message)
					}
				}
			} else {
				// Expect no ExternalIPProgrammed condition
				for _, c := range updatedSvc.Status.Conditions {
					if c.Type == "ExternalIPProgrammed" {
						t.Errorf("Expected no ExternalIPProgrammed condition, but found one: %+v", c)
					}
				}
			}
		})
	}
}

func TestServiceServesTCPPort(t *testing.T) {
	testCases := []struct {
		desc  string
		ports []v1.ServicePort
		port  int64
		want  bool
	}{
		{
			desc: "no ports",
			port: 8080,
			want: false,
		},
		{
			desc:  "TCP port matches",
			ports: []v1.ServicePort{tcpPort(80), tcpPort(8080)},
			port:  8080,
			want:  true,
		},
		{
			desc:  "empty protocol defaults to TCP",
			ports: []v1.ServicePort{{Port: 8080}},
			port:  8080,
			want:  true,
		},
		{
			desc:  "UDP port does not match",
			ports: []v1.ServicePort{udpPort(8080)},
			port:  8080,
			want:  false,
		},
		{
			desc:  "no matching port",
			ports: []v1.ServicePort{tcpPort(80), udpPort(53)},
			port:  8080,
			want:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			svc := &v1.Service{Spec: v1.ServiceSpec{Ports: tc.ports}}
			if got := serviceServesTCPPort(svc, tc.port); got != tc.want {
				t.Errorf("serviceServesTCPPort(%v, %d) = %v, want %v", tc.ports, tc.port, got, tc.want)
			}
		})
	}
}

func TestValidateForwardingRule(t *testing.T) {
	testCases := []struct {
		desc           string
		fr             *composite.ForwardingRule
		frName         string
		expectError    bool
		expectErrorMsg string
	}{
		{
			desc: "Valid TCP rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "TCP",
			},
			frName:      "valid-tcp",
			expectError: false,
		},
		{
			desc: "Valid UDP rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "UDP",
			},
			frName:      "valid-udp",
			expectError: false,
		},
		{
			desc: "Valid two IPv4 addresses",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
				IPAddresses:         []string{"10.0.0.100", "10.0.1.101"},
			},
			frName:      "valid-two-ipv4",
			expectError: false,
		},
		{
			desc: "Valid two IPv6 addresses",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
				IPAddresses:         []string{"2600:1234::1234/96", "2600:1235::123/96"},
			},
			frName:      "valid-two-ipv6",
			expectError: false,
		},
		{
			desc: "Valid L3_DEFAULT rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "L3_DEFAULT",
			},
			frName:      "valid-l3-default",
			expectError: false,
		},
		{
			desc: "Internal scheme not supported",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "INTERNAL",
				IPProtocol:          "TCP",
			},
			frName:         "internal-scheme",
			expectError:    true,
			expectErrorMsg: "forwarding rule internal-scheme has unsupported load balancing scheme: INTERNAL, supported schemes are: EXTERNAL, EXTERNAL_PASSTHROUGH",
		},
		{
			desc: "Valid EXTERNAL_PASSTHROUGH rule",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
			},
			frName:      "valid-ext-passthrough",
			expectError: false,
		},
		{
			desc: "Unsupported scheme",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "INTERNAL_SELF_MANAGED",
				IPProtocol:          "TCP",
			},
			frName:         "invalid-scheme",
			expectError:    true,
			expectErrorMsg: "forwarding rule invalid-scheme has unsupported load balancing scheme: INTERNAL_SELF_MANAGED, supported schemes are: EXTERNAL, EXTERNAL_PASSTHROUGH",
		},
		{
			desc: "Unsupported protocol",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL",
				IPProtocol:          "ESP",
			},
			frName:         "invalid-protocol",
			expectError:    true,
			expectErrorMsg: "forwarding rule invalid-protocol has unsupported protocol: ESP, supported protocols are: TCP, UDP, L3_DEFAULT",
		},
		{
			desc: "More than two IP addresses in IPAddresses array",
			fr: &composite.ForwardingRule{
				LoadBalancingScheme: "EXTERNAL_PASSTHROUGH",
				IPProtocol:          "TCP",
				IPAddresses:         []string{"10.0.0.100", "10.0.0.101", "10.0.0.102"},
			},
			frName:         "too-many-ips",
			expectError:    true,
			expectErrorMsg: "forwarding rule too-many-ips has more than 2 IP addresses",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			err := validateForwardingRule(tc.fr, tc.frName)
			if (err != nil) != tc.expectError {
				t.Errorf("validateForwardingRule() error = %v, expectError = %v", err, tc.expectError)
			}
			if err != nil && tc.expectErrorMsg != "" && err.Error() != tc.expectErrorMsg {
				t.Errorf("validateForwardingRule() error message = %q, expected = %q", err.Error(), tc.expectErrorMsg)
			}
		})
	}
}

func setupControllerContext(t *testing.T) (*fake.Clientset, *gce.Cloud, *StandaloneNEGLBController, chan struct{}) {
	kubeClient := fake.NewSimpleClientset()
	fakeGCE := gce.NewFakeGCECloud(test.DefaultTestClusterValues())
	l4Namer := namer.NewL4Namer("k8s2-cluster-uid", namer.NewNamer("cluster-id", "firewall-id", klog.TODO()))

	stopCh := make(chan struct{})

	ctxConfig := ingctx.ControllerContextConfig{Namespace: v1.NamespaceAll}
	svcNegClient := svcnegclientfake.NewSimpleClientset()
	c, err := ingctx.NewControllerContext(kubeClient, nil, nil, nil, svcNegClient, nil, nil, nil, nil, nil, kubeClient, fakeGCE, l4Namer.Namer, "k8s2-cluster-uid", ctxConfig, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create controller context: %v", err)
	}
	c.L4Namer = l4Namer

	lc := NewStandaloneNEGLBController(c, stopCh, klog.TODO())
	return kubeClient, fakeGCE, lc, stopCh
}

func TestStandaloneNEGLBControllerMetrics_Success(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"
	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a valid forwarding rule
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}
	fr := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           frIP,
		BackendService:      bsURL,
		LoadBalancingScheme: "EXTERNAL",
		IPProtocol:          "TCP",
		Scope:               meta.Regional,
		Version:             meta.VersionBeta,
	}
	err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create forwarding rule: %v", err)
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	// Create Backend Service and SvcNeg for BS validation
	bsKey, _ := composite.CreateKey(fakeGCE, "bs1", meta.Regional)
	bs := &composite.BackendService{
		Name:    "bs1",
		Scope:   meta.Regional,
		Version: meta.VersionBeta,
		Backends: []*composite.Backend{
			{Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/neg-1", project)},
		},
	}
	composite.CreateBackendService(fakeGCE, bsKey, bs, klog.TODO())
	svcNeg := test.NewSvcNeg(types.NamespacedName{Namespace: "default", Name: lc.namer.L4Backend(svc.Namespace, svc.Name)}, negv1beta1.ServiceNetworkEndpointGroupStatus{
		NetworkEndpointGroups: []negv1beta1.NegObjectReference{
			{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/neg-1", project)},
		},
	})
	svcNeg.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(svc, svc.GroupVersionKind()),
	})
	lc.ctx.SvcNegInformer.GetIndexer().Add(svcNeg)

	// Add service to informer and fake kube client
	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	err = lc.syncWrapper(svcKey)
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}

	state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}
	if state.Status != l4metrics.StatusSuccess {
		t.Errorf("Expected status %s, got %s", l4metrics.StatusSuccess, state.Status)
	}
	if !state.LBSchemeExternal {
		t.Errorf("Expected LBSchemeExternal to be true")
	}
}

func TestStandaloneNEGLBControllerMetrics_UserError(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"
	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a forwarding rule with invalid scheme
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}
	fr := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           frIP,
		BackendService:      bsURL,
		LoadBalancingScheme: "INTERNAL_SELF_MANAGED", // Invalid
		IPProtocol:          "TCP",
		Scope:               meta.Regional,
		Version:             meta.VersionBeta,
	}
	err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create forwarding rule: %v", err)
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	err = lc.syncWrapper(svcKey)
	if err == nil {
		t.Fatalf("Expected sync to fail")
	}

	state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}
	if state.Status != l4metrics.StatusUserError {
		t.Errorf("Expected status %s, got %s", l4metrics.StatusUserError, state.Status)
	}
}

func TestStandaloneNEGLBControllerMetrics_SystemError(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a valid forwarding rule key
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}

	// Inject GCE error
	mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
	if mockGCE.MockBetaForwardingRules.GetError == nil {
		mockGCE.MockBetaForwardingRules.GetError = make(map[meta.Key]error)
	}
	mockGCE.MockBetaForwardingRules.GetError[*key] = fmt.Errorf("internal GCE error")
	defer delete(mockGCE.MockBetaForwardingRules.GetError, *key)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	err = lc.syncWrapper(svcKey)
	if err == nil {
		t.Fatalf("Expected sync to fail due to GCE error")
	}

	state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}
	if state.Status != l4metrics.StatusError {
		t.Errorf("Expected status %s, got %s", l4metrics.StatusError, state.Status)
	}
	if state.FirstSyncErrorTime == nil {
		t.Errorf("Expected FirstSyncErrorTime to be set for system error")
	}
}

func TestStandaloneNEGLBControllerMetrics_Deletion(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	frName := "custom-fr"
	frIP := "10.0.0.100"
	project := "test-project"
	region := "us-central1"
	bsURL := fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/backendServices/bs1", project, region)

	kubeClient, fakeGCE, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Create a valid forwarding rule
	key, err := composite.CreateKey(fakeGCE, frName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key for forwarding rule: %v", err)
	}
	fr := &composite.ForwardingRule{
		Name:                frName,
		IPAddress:           frIP,
		BackendService:      bsURL,
		LoadBalancingScheme: "EXTERNAL",
		IPProtocol:          "TCP",
		Scope:               meta.Regional,
		Version:             meta.VersionBeta,
	}
	err = composite.CreateForwardingRule(fakeGCE, key, fr, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to create forwarding rule: %v", err)
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
			Annotations: map[string]string{
				annotations.CustomForwardingRuleKey: frName,
			},
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}

	// Create Backend Service and SvcNeg for BS validation
	bsKey, _ := composite.CreateKey(fakeGCE, "bs1", meta.Regional)
	bs := &composite.BackendService{
		Name:    "bs1",
		Scope:   meta.Regional,
		Version: meta.VersionBeta,
		Backends: []*composite.Backend{
			{Group: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/neg-1", project)},
		},
	}
	composite.CreateBackendService(fakeGCE, bsKey, bs, klog.TODO())
	svcNeg := test.NewSvcNeg(types.NamespacedName{Namespace: "default", Name: lc.namer.L4Backend(svc.Namespace, svc.Name)}, negv1beta1.ServiceNetworkEndpointGroupStatus{
		NetworkEndpointGroups: []negv1beta1.NegObjectReference{
			{SelfLink: fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/us-central1-a/networkEndpointGroups/neg-1", project)},
		},
	})
	svcNeg.SetOwnerReferences([]metav1.OwnerReference{
		*metav1.NewControllerRef(svc, svc.GroupVersionKind()),
	})
	lc.ctx.SvcNegInformer.GetIndexer().Add(svcNeg)

	// Add service to informer and fake kube client
	lc.ctx.ServiceInformer.GetIndexer().Add(svc)
	kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})

	svcKey := svc.Namespace + "/" + svc.Name

	// Initial sync to establish state in metrics
	err = lc.syncWrapper(svcKey)
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}
	_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if !ok {
		t.Fatalf("Expected service %s in metrics map", svcKey)
	}

	// Act - Deletion
	err = kubeClient.CoreV1().Services(svc.Namespace).Delete(context.TODO(), svc.Name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}
	lc.ctx.ServiceInformer.GetIndexer().Delete(svc)

	err = lc.syncWrapper(svcKey)
	if err != nil {
		t.Fatalf("sync() error = %v", err)
	}

	// Assert
	_, ok = lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
	if ok {
		t.Errorf("Expected service %s to be removed from metrics map", svcKey)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Add(t *testing.T) {
	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Act
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Assert - Wait for metrics to be updated (should be UserError because no annotation)
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		state, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok && state.Status == l4metrics.StatusUserError, nil
	})
	if err != nil {
		t.Errorf("Failed to verify AddFunc via metrics: %v", err)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Update(t *testing.T) {
	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass
	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Setup initial state (service exists and is enqueued/processed once)
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	// Wait for initial metrics to be created
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok, nil
	})
	if err != nil {
		t.Fatalf("Failed to establish initial metrics state: %v", err)
	}

	// Act - Modify to not match shouldProcess (remove load balancer class)
	currentSvc, err := kubeClient.CoreV1().Services(svc.Namespace).Get(context.TODO(), svc.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get service: %v", err)
	}
	currentSvc.Spec.LoadBalancerClass = nil
	_, err = kubeClient.CoreV1().Services(svc.Namespace).Update(context.TODO(), currentSvc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update service: %v", err)
	}

	// Assert - Wait for metrics to be deleted
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return !ok, nil
	})
	if err != nil {
		t.Errorf("Failed to verify UpdateFunc (non-matching) via metrics: %v", err)
	}
}

func TestStandaloneNEGLBControllerEventHandlers_Delete(t *testing.T) {
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass

	kubeClient, _, lc, stopCh := setupControllerContext(t)
	defer close(stopCh)

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "svc1",
			Namespace: "default",
		},
		Spec: v1.ServiceSpec{
			Type:              v1.ServiceTypeLoadBalancer,
			LoadBalancerClass: &lbClass,
		},
	}
	svcKey := svc.Namespace + "/" + svc.Name

	// Setup initial state
	_, err := kubeClient.CoreV1().Services(svc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Start the informer
	go lc.ctx.ServiceInformer.Run(stopCh)
	if !cache.WaitForCacheSync(stopCh, lc.ctx.ServiceInformer.HasSynced) {
		t.Fatalf("Failed to sync cache")
	}

	// Start the queue workers
	go lc.svcQueue.Run()
	defer lc.svcQueue.Shutdown()

	// Wait for initial metrics to be created
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return ok, nil
	})
	if err != nil {
		t.Fatalf("Failed to establish initial metrics state: %v", err)
	}

	// Act - Delete service
	err = kubeClient.CoreV1().Services(svc.Namespace).Delete(context.TODO(), svc.Name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}

	// Assert - Wait for metrics to be deleted
	err = wait.PollImmediate(100*time.Millisecond, 5*time.Second, func() (bool, error) {
		_, ok := lc.ctx.L4Metrics.StandaloneNEGServiceState(svcKey)
		return !ok, nil
	})
	if err != nil {
		t.Errorf("Failed to verify DeleteFunc via metrics: %v", err)
	}
}

func TestClassifyError(t *testing.T) {
	testCases := []struct {
		desc     string
		err      error
		expected lbConditionReason
	}{
		{
			desc:     "nil error",
			err:      nil,
			expected: ProviderError,
		},
		{
			desc:     "not found error",
			err:      &googleapi.Error{Code: http.StatusNotFound},
			expected: InvalidForwardingRule,
		},
		{
			desc:     "unsupported load balancing scheme",
			err:      l4utils.NewUnsupportedLoadBalancingSchemeError("fr-name", "EXTERNAL", []string{"INTERNAL"}),
			expected: UnsupportedLBType,
		},
		{
			desc:     "unsupported load balancing scheme wrapped in UserError",
			err:      l4utils.NewUserError(l4utils.NewUnsupportedLoadBalancingSchemeError("fr-name", "EXTERNAL", []string{"INTERNAL"})),
			expected: UnsupportedLBType,
		},
		{
			desc:     "unsupported load balancing scheme wrapped in UserError and fmt.Errorf",
			err:      fmt.Errorf("wrapped: %w", l4utils.NewUserError(l4utils.NewUnsupportedLoadBalancingSchemeError("fr-name", "EXTERNAL", []string{"INTERNAL"}))),
			expected: UnsupportedLBType,
		},
		{
			desc:     "unsupported protocol",
			err:      l4utils.NewUnsupportedProtocolError("fr-name", "UAUDP", []string{"TCP"}),
			expected: InvalidForwardingRule,
		},
		{
			desc:     "unsupported protocol wrapped in UserError",
			err:      l4utils.NewUserError(l4utils.NewUnsupportedProtocolError("fr-name", "UAUDP", []string{"TCP"})),
			expected: InvalidForwardingRule,
		},
		{
			desc:     "unsupported protocol wrapped in UserError and fmt.Errorf",
			err:      fmt.Errorf("wrapped: %w", l4utils.NewUserError(l4utils.NewUnsupportedProtocolError("fr-name", "UAUDP", []string{"TCP"}))),
			expected: InvalidForwardingRule,
		},
		{
			desc:     "backend not attached",
			err:      l4utils.NewBackendNotAttachedError("bs1"),
			expected: BackendNotAttached,
		},
		{
			desc:     "backend not attached wrapped in UserError",
			err:      l4utils.NewUserError(l4utils.NewBackendNotAttachedError("bs1")),
			expected: BackendNotAttached,
		},
		{
			desc:     "backend not attached wrapped in UserError and fmt.Errorf",
			err:      fmt.Errorf("wrapped: %w", l4utils.NewUserError(l4utils.NewBackendNotAttachedError("bs1"))),
			expected: BackendNotAttached,
		},
		{
			desc:     "generic error",
			err:      errors.New("generic error"),
			expected: ProviderError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := classifyError(tc.err)
			if got != tc.expected {
				t.Errorf("classifyError(%v) = %q, expected %q", tc.err, got, tc.expected)
			}
		})
	}
}

func generateForwardingRuleKey(fr string, num int) string {
	var buffer bytes.Buffer
	for i := 1; i <= num; i++ {
		buffer.WriteString(fmt.Sprintf("%s%d,", fr, i))
	}
	return strings.TrimSuffix(buffer.String(), ",")
}

func TestJoinMaybeUserErrors(t *testing.T) {
	userErr1 := l4utils.NewUserError(errors.New("user err 1"))
	userErr2 := l4utils.NewUserError(errors.New("user err 2"))
	sysErr := errors.New("system err")

	testCases := []struct {
		desc       string
		errs       []error
		expectNil  bool
		expectUser bool
	}{
		{
			desc:      "all nil errors",
			errs:      []error{nil, nil},
			expectNil: true,
		},
		{
			desc:       "nil mixed with user errors",
			errs:       []error{nil, userErr1, nil, userErr2},
			expectUser: true,
		},
		{
			desc:       "only user errors",
			errs:       []error{userErr1, userErr2},
			expectUser: true,
		},
		{
			desc:       "nil mixed with system error",
			errs:       []error{nil, sysErr},
			expectUser: false,
		},
		{
			desc:       "user error mixed with system error and nil",
			errs:       []error{nil, userErr1, sysErr},
			expectUser: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := joinMaybeUserErrors(tc.errs...)
			if tc.expectNil {
				if got != nil {
					t.Fatalf("expected nil error, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil error, got nil")
			}
			var userErr *l4utils.UserError
			isUser := errors.As(got, &userErr) && got == userErr
			if isUser != tc.expectUser {
				t.Errorf("isUserErrorWrapper(%v) = %v, expected %v", got, isUser, tc.expectUser)
			}
		})
	}
}

func negSelfLink(project, zone, negName string) string {
	return fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/zones/%s/networkEndpointGroups/%s", project, zone, negName)
}

// negMapKey converts a NEG self link into the key returned by getServiceNEGLinks.
func negMapKey(t *testing.T, selfLink string) cloud.ResourceMapKey {
	t.Helper()
	resID, err := cloud.ParseResourceURL(selfLink)
	if err != nil {
		t.Fatalf("ParseResourceURL(%q) returned error: %v", selfLink, err)
	}
	return resID.MapKey()
}

func TestGetServiceNEGLinks_CustomNEGName(t *testing.T) {
	flags.F.EnableL4CustomStandaloneNEGNames = true
	defer func() { flags.F.EnableL4CustomStandaloneNEGNames = false }()

	const (
		project    = "test-project"
		zone       = "us-central1-a"
		namespace  = "default"
		svcName    = "svc-custom-neg"
		customName = "my-custom-neg"
	)
	lbClass := annotations.StandalonePassthroughNegLoadBalancerClass

	newSvc := func(annots map[string]string) *v1.Service {
		return &v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        svcName,
				Namespace:   namespace,
				Annotations: annots,
			},
			Spec: v1.ServiceSpec{
				Type:              v1.ServiceTypeLoadBalancer,
				LoadBalancerClass: &lbClass,
			},
		}
	}

	testCases := []struct {
		desc string
		svc  *v1.Service
		// negNamesInStore are the SvcNeg CR names to register, each pointing at a NEG of the same name.
		negNamesInStore func(generatedName string) []string
		isOwnedBySvc    bool
		// wantNEGNames are the NEG names expected in the returned set.
		wantNEGNames func(generatedName string) []string
		wantErr      bool
	}{
		{
			desc:            "no annotation: falls back to the generated L4Backend name",
			svc:             newSvc(nil),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated, customName} },
			wantNEGNames:    func(generated string) []string { return []string{generated} },
		},
		{
			desc: "annotation set: NEG name matches the annotation value, generated name is ignored",
			svc: newSvc(map[string]string{
				annotations.StandaloneNegName: customName,
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated, customName} },
			wantNEGNames:    func(generated string) []string { return []string{customName} },
		},
		{
			desc: "annotation set: NEG name matches the annotation value, but is not owned by controller",
			svc: newSvc(map[string]string{
				annotations.StandaloneNegName: customName,
			}),
			isOwnedBySvc:    false,
			negNamesInStore: func(generated string) []string { return []string{generated, customName} },
			wantErr:         true,
		},
		{
			desc: "annotation set but only the generated NEG exists: nothing is returned",
			svc: newSvc(map[string]string{
				annotations.StandaloneNegName: customName,
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantNEGNames:    func(generated string) []string { return nil },
		},
		{
			desc: "invalid custom name is rejected",
			svc: newSvc(map[string]string{
				annotations.StandaloneNegName: "Invalid_NEG_Name",
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantErr:         true,
		},
		{
			desc: "empty custom name is rejected",
			svc: newSvc(map[string]string{
				annotations.StandaloneNegName: "",
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantErr:         true,
		},
		{
			desc: "path traversal name is rejected",
			svc: newSvc(map[string]string{
				annotations.StandaloneNegName: "../../us-central1-c/networkEndpointGroups/custom-neg",
			}),
			isOwnedBySvc:    true,
			negNamesInStore: func(generated string) []string { return []string{generated} },
			wantErr:         true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, lc, stopCh := setupControllerContext(t)
			defer close(stopCh)

			generatedName := lc.namer.L4Backend(namespace, svcName)
			if generatedName == customName {
				t.Fatalf("test setup is broken: generated name equals the custom name %q", customName)
			}

			for _, negName := range tc.negNamesInStore(generatedName) {
				svcNeg := test.NewSvcNeg(
					types.NamespacedName{Namespace: namespace, Name: negName},
					negv1beta1.ServiceNetworkEndpointGroupStatus{
						NetworkEndpointGroups: []negv1beta1.NegObjectReference{
							{SelfLink: negSelfLink(project, zone, negName)},
						},
					},
				)
				if tc.isOwnedBySvc {
					svcNeg.SetOwnerReferences([]metav1.OwnerReference{
						*metav1.NewControllerRef(tc.svc, tc.svc.GroupVersionKind()),
					})
				}
				if err := lc.ctx.SvcNegInformer.GetIndexer().Add(svcNeg); err != nil {
					t.Fatalf("Failed to add SvcNeg %q: %v", negName, err)
				}
			}

			got, err := lc.getServiceNEGLinks(tc.svc)
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("getServiceNEGLinks() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}

			want := tc.wantNEGNames(generatedName)
			if got.Len() != len(want) {
				t.Fatalf("getServiceNEGLinks() returned %d NEG(s) %v, want %d %v", got.Len(), got.UnsortedList(), len(want), want)
			}
			for _, negName := range want {
				if key := negMapKey(t, negSelfLink(project, zone, negName)); !got.Has(key) {
					t.Errorf("getServiceNEGLinks() = %v, missing NEG %q", got.UnsortedList(), negName)
				}
			}
			// The generated name must not leak in when a custom name is requested.
			if len(want) == 1 && want[0] == customName {
				if key := negMapKey(t, negSelfLink(project, zone, generatedName)); got.Has(key) {
					t.Errorf("getServiceNEGLinks() still returned the generated NEG %q", generatedName)
				}
			}
		})
	}
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

func testBS(name string, scope meta.KeyType, hc string) *composite.BackendService {
	return &composite.BackendService{
		Name:         name,
		Scope:        scope,
		Version:      meta.VersionBeta,
		HealthChecks: []string{hc},
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

// ipProgrammedCondition returns the ExternalIPProgrammed condition expected for the given programmed IPs.
func ipProgrammedCondition(ips ...string) *metav1.Condition {
	return &metav1.Condition{
		Type:    "ExternalIPProgrammed",
		Status:  metav1.ConditionTrue,
		Reason:  "IPProgrammed",
		Message: "IPs programmed: " + strings.Join(ips, ", "),
	}
}
