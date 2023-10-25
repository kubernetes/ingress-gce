package backends

import (
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	kubeSystemUID             = "ksuid123"
	defaultIdleTimeout        = int64(10 * 60)     // 10 minutes
	prolongedIdleTimeout      = int64(2 * 60 * 60) // 2 hours
	perSessionTrackingMode    = "PER_SESSION"
	perConnectionTrackingMode = "PER_CONNECTION"

	testServiceName      = "test-service"
	testServiceNamespace = "test-ns"
)

func TestEnsureL4BackendService(t *testing.T) {
	for _, tc := range []struct {
		desc                        string
		serviceName                 string
		serviceNamespace            string
		protocol                    string
		affinityType                string
		schemeType                  string
		enableStrongSessionAffinity bool
		idleTimeout                 int64
		trackingMode                string
	}{
		{
			desc:                        "Test basic Backend Service with Internal scheme type",
			serviceName:                 testServiceName,
			serviceNamespace:            testServiceNamespace,
			protocol:                    "TCP",
			affinityType:                string(v1.ServiceAffinityNone),
			schemeType:                  string(cloud.SchemeInternal),
			enableStrongSessionAffinity: false,
			idleTimeout:                 defaultIdleTimeout,
			trackingMode:                defaultTrackingMode,
		},
		{
			desc:                        "Test Backend Service with Strong Session Affinity configuration",
			serviceName:                 testServiceName,
			serviceNamespace:            testServiceNamespace,
			protocol:                    "TCP",
			affinityType:                string(v1.ServiceAffinityClientIP),
			schemeType:                  string(cloud.SchemeExternal),
			enableStrongSessionAffinity: true,
			idleTimeout:                 prolongedIdleTimeout,
			trackingMode:                PerSessionTrackingMode,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			namespacedName := types.NamespacedName{Name: tc.serviceName, Namespace: tc.serviceNamespace}
			connectionTrackingPolicy := &composite.BackendServiceConnectionTrackingPolicy{
				EnableStrongAffinity: tc.enableStrongSessionAffinity,
				IdleTimeoutSec:       tc.idleTimeout,
				TrackingMode:         tc.trackingMode,
			}
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4namer := namer.NewL4Namer(kubeSystemUID, nil)
			backendPool := NewPool(fakeGCE, l4namer)

			hcLink := l4namer.L4HealthCheck(tc.serviceNamespace, tc.serviceName, false)
			bsName := l4namer.L4Backend(tc.serviceNamespace, tc.serviceName)
			network := network.NetworkInfo{IsDefault: false, NetworkURL: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"}
			bs, err := backendPool.EnsureL4BackendService(bsName, hcLink, tc.protocol, tc.affinityType, tc.schemeType, namespacedName, network, connectionTrackingPolicy)
			if err != nil {
				t.Errorf("EnsureL4BackendService failed")
			}
			// backend service changes the session affinity from ClientIP to CLIENT_IP etc
			gotSessionAffinity := strings.Replace(bs.SessionAffinity, "_", "", -1)
			if gotSessionAffinity != strings.ToUpper(tc.affinityType) {
				t.Errorf("BackendService.SessionAffinity was not populated correctly want=%q, got=%q", strings.ToUpper(tc.affinityType), gotSessionAffinity)
			}
			if bs.Network != network.NetworkURL {
				t.Errorf("BackendService.Network was not populated correctly, want=%q, got=%q", network.NetworkURL, bs.Network)
			}
			if len(bs.HealthChecks) != 1 || bs.HealthChecks[0] != hcLink {
				t.Errorf("BackendService.HealthChecks was not populated correctly, want=%q, got=%q", hcLink, bs.HealthChecks)
			}
			description, err := utils.MakeL4LBServiceDescription(namespacedName.String(), "", meta.VersionGA, false, utils.ILB)
			if err != nil {
				t.Errorf("utils.MakeL4LBServiceDescription() failed %v", err)
			}
			if bs.Description != description {
				t.Errorf("BackendService.Description was not populated correctly, want=%q, got=%q", description, bs.Description)
			}
			if bs.Protocol != tc.protocol {
				t.Errorf("BackendService.Protocol was not populated correctly, want=%q, got=%q", "TCP", bs.Protocol)
			}
			if bs.LoadBalancingScheme != tc.schemeType {
				t.Errorf("BackendService.LoadBalancingScheme was not populated correctly, want=%q, got=%q", tc.schemeType, bs.LoadBalancingScheme)
			}
			if bs.ConnectionDraining == nil || bs.ConnectionDraining.DrainingTimeoutSec != DefaultConnectionDrainingTimeoutSeconds {
				t.Errorf("BackendService.ConnectionDraining was not populated correctly, want=connection draining with %q, got=%q", DefaultConnectionDrainingTimeoutSeconds, bs.ConnectionDraining)
			}
			if diff := cmp.Diff(bs.ConnectionTrackingPolicy, connectionTrackingPolicy); diff != "" {
				t.Errorf("BackendService.ConnectionTrackingPolicy was not populated correctly, expected to be different: %s", diff)
			}
		})
	}
}

func TestEnsureL4BackendServiceDoesNotDetachBackends(t *testing.T) {
	namespacedName := types.NamespacedName{Name: testServiceName, Namespace: testServiceNamespace}
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	l4namer := namer.NewL4Namer(kubeSystemUID, nil)
	backendPool := NewPool(fakeGCE, l4namer)

	hcLink := l4namer.L4HealthCheck(testServiceNamespace, testServiceName, false)
	bsName := l4namer.L4Backend(testServiceNamespace, testServiceName)
	network := network.NetworkInfo{IsDefault: false, NetworkURL: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"}

	backendName := "testNeg"
	existingBS := &composite.BackendService{
		Name:                bsName,
		Protocol:            "TCP",
		Description:         "test description", // this will make sure the BackendService needs update.
		HealthChecks:        []string{hcLink},
		SessionAffinity:     utils.TranslateAffinityType(string(v1.ServiceAffinityNone)),
		LoadBalancingScheme: string(cloud.SchemeInternal),
		Backends: []*composite.Backend{
			{
				BalancingMode: "CONNECTION",
				Group:         backendName,
			},
		},
	}
	key, err := composite.CreateKey(fakeGCE, bsName, meta.Regional)
	if err != nil {
		t.Fatalf("failed to create key %v", err)
	}
	err = composite.CreateBackendService(fakeGCE, key, existingBS, klog.TODO())
	if err != nil {
		t.Fatalf("failed to create the existing backend service: %v", err)
	}

	bs, err := backendPool.EnsureL4BackendService(bsName, hcLink, "TCP", string(v1.ServiceAffinityNone), string(cloud.SchemeInternal), namespacedName, network, nil)
	if err != nil {
		t.Errorf("EnsureL4BackendService failed")
	}
	if len(bs.Backends) == 0 {
		t.Fatalf("expected backends to be still attached to the backend service but there were none")
	}
	backend := bs.Backends[0]
	if diff := cmp.Diff(existingBS.Backends[0], backend); diff != "" {
		t.Errorf("BackendService.Backends were changed, expected no change (+got,-want): %s", diff)
	}
	description, err := utils.MakeL4LBServiceDescription(namespacedName.String(), "", meta.VersionGA, false, utils.ILB)
	if err != nil {
		t.Errorf("utils.MakeL4LBServiceDescription() failed %v", err)
	}
	if bs.Description != description {
		t.Errorf("BackendService.Description was not populated correctly, want=%q, got=%q", description, bs.Description)
	}
}

// TestBackendSvcL4RelevantPropertiesEqual checks that backendSvcL4RelevantPropertiesEqual() and
// connectionTrackingPolicyEqual() (as a part ofit  backendSvcL4RelevantPropertiesEqual)
// return expected results for two resources compared.
func TestBackendSvcL4RelevantPropertiesEqual(t *testing.T) {
	for _, tc := range []struct {
		desc              string
		oldBackendService *composite.BackendService
		newBackendService *composite.BackendService
		wantEqual         bool
	}{
		{
			desc:              "Test empty backend services are equal",
			oldBackendService: &composite.BackendService{},
			newBackendService: &composite.BackendService{},
			wantEqual:         true,
		},
		{
			desc: "Test with equal non-empty backend services",
			oldBackendService: &composite.BackendService{
				Description:         "same_description",
				Protocol:            "TCP",
				SessionAffinity:     string(v1.ServiceAffinityClientIP),
				LoadBalancingScheme: string(cloud.SchemeExternal),
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: false,
					IdleTimeoutSec:       defaultIdleTimeout,
					TrackingMode:         defaultTrackingMode,
				},
			},
			newBackendService: &composite.BackendService{
				Description:         "same_description",
				Protocol:            "TCP",
				SessionAffinity:     string(v1.ServiceAffinityClientIP),
				LoadBalancingScheme: string(cloud.SchemeExternal),
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: false,
					IdleTimeoutSec:       defaultIdleTimeout,
					TrackingMode:         defaultTrackingMode,
				},
			},
			wantEqual: true,
		},
		{
			desc: "Test with changed idle timeout",
			oldBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					IdleTimeoutSec: prolongedIdleTimeout,
				},
			},
			newBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					IdleTimeoutSec: defaultIdleTimeout,
				},
			},
			wantEqual: false,
		},
		{
			desc: "Test with changed backend description",
			oldBackendService: &composite.BackendService{
				Description: "old description",
			},
			newBackendService: &composite.BackendService{
				Description: "new description",
			},
			wantEqual: false,
		},
		{
			desc: "Test with changed protocols",
			oldBackendService: &composite.BackendService{
				Protocol: "TCP",
			},
			newBackendService: &composite.BackendService{
				Protocol: "UDP",
			},
			wantEqual: false,
		},
		{
			desc: "Test with changed TrackingMode",
			oldBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					TrackingMode: perSessionTrackingMode,
				},
			},
			newBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					TrackingMode: perConnectionTrackingMode,
				},
			},
			wantEqual: false,
		},
		{
			desc: "Test with changed health-checks",
			oldBackendService: &composite.BackendService{
				HealthChecks: []string{"abc"},
			},
			newBackendService: &composite.BackendService{
				HealthChecks: []string{"abc", "xyz"},
			},
			wantEqual: false,
		},
		{
			desc: "Test with deleted network",
			oldBackendService: &composite.BackendService{
				Network: "network-1",
			},
			newBackendService: &composite.BackendService{},
			wantEqual:         false,
		},
		{
			desc: "Test with a few changed parameters",
			oldBackendService: &composite.BackendService{
				SessionAffinity:     string(v1.ServiceAffinityClientIP),
				LoadBalancingScheme: string(cloud.SchemeInternal),
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: false,
					IdleTimeoutSec:       defaultIdleTimeout,
				},
			},
			newBackendService: &composite.BackendService{
				SessionAffinity:     string(v1.ServiceAffinityNone),
				LoadBalancingScheme: string(cloud.SchemeExternal),
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: true,
					IdleTimeoutSec:       prolongedIdleTimeout,
				},
			},
			wantEqual: false,
		},
		{
			// ConnectionPersistenceOnUnhealthyBackends change is not supported yet
			// that's why wantEqual = True. Change this case when the support starts.
			desc: "The customer's update to ConnectionPersistenceOnUnhealthyBackends will not be overriden",
			oldBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity:                     false,
					IdleTimeoutSec:                           defaultIdleTimeout,
					TrackingMode:                             perSessionTrackingMode,
					ConnectionPersistenceOnUnhealthyBackends: "non-default",
				},
			},
			newBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity:                     false,
					IdleTimeoutSec:                           defaultIdleTimeout,
					TrackingMode:                             perSessionTrackingMode,
					ConnectionPersistenceOnUnhealthyBackends: "DEFAULT_FOR_PROTOCOL",
				},
			},
			wantEqual: true,
		},
		{
			desc: "Ignores backends",
			oldBackendService: &composite.BackendService{
				Backends: []*composite.Backend{
					{
						Group: "test1",
					},
				},
			},
			newBackendService: &composite.BackendService{
				Backends: []*composite.Backend{
					{
						Group: "test2",
					},
				},
			},
			wantEqual: true,
		},
		{
			desc: "Ignores non relevant attributes",
			oldBackendService: &composite.BackendService{
				AffinityCookieTtlSec: 1,
				Backends: []*composite.Backend{
					{
						Group: "test1",
					},
				},
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					MaxTtl: 10,
				},
				CircuitBreakers: &composite.CircuitBreakers{
					MaxConnections: 10,
				},
				CompressionMode: "Brotli",
				ConnectionDraining: &composite.ConnectionDraining{
					DrainingTimeoutSec: 10,
				},
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					IdleTimeoutSec: 10,
				},
				ConsistentHash: &composite.ConsistentHashLoadBalancerSettings{
					HttpHeaderName: "Test1",
				},
				CustomRequestHeaders:  []string{"h1"},
				CustomResponseHeaders: []string{"rh1"},
				EdgeSecurityPolicy:    "test1",
				EnableCDN:             false,
				FailoverPolicy: &composite.BackendServiceFailoverPolicy{
					FailoverRatio: 0.5,
				},
				Iap: &composite.BackendServiceIAP{
					Enabled: false,
				},
				Id:                       1,
				IpAddressSelectionPolicy: "IPV4_ONLY",
				Kind:                     "compute#backendService",
				LoadBalancingScheme:      "INTERNAL_MANAGED",
				LocalityLbPolicies: []*composite.BackendServiceLocalityLoadBalancingPolicyConfig{
					{
						CustomPolicy: &composite.BackendServiceLocalityLoadBalancingPolicyConfigCustomPolicy{
							Name: "test1",
						},
					},
				},
				LocalityLbPolicy: "LEAST_REQUEST",
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: false,
				},
				MaxStreamDuration: &composite.Duration{
					Seconds: 10,
				},
				Metadatas: nil,
				Name:      "test1",
				OutlierDetection: &composite.OutlierDetection{
					ConsecutiveErrors: 10,
				},
				Port:           80,
				PortName:       "http",
				Protocol:       "TCP",
				Region:         "us-central1",
				SecurityPolicy: "policyURL1",
				SecuritySettings: &composite.SecuritySettings{
					ClientTlsPolicy: "policy1",
				},
				ServiceBindings: []string{"test1"},
				ServiceLbPolicy: "EXTERNAL",
				Subsetting: &composite.Subsetting{
					SubsetSize: 10,
				},
				TimeoutSec:      10,
				VpcNetworkScope: "REGIONAL_VPC_NETWORK",
				ServerResponse: googleapi.ServerResponse{
					HTTPStatusCode: 200,
				},
			},
			newBackendService: &composite.BackendService{
				AffinityCookieTtlSec: 1,
				Backends: []*composite.Backend{
					{
						Group: "test2",
					},
				},
				CdnPolicy: &composite.BackendServiceCdnPolicy{
					MaxTtl: 20,
				},
				CircuitBreakers: &composite.CircuitBreakers{
					MaxConnections: 20,
				},
				CompressionMode: "gzip",
				ConnectionDraining: &composite.ConnectionDraining{
					DrainingTimeoutSec: 20,
				},
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					IdleTimeoutSec: 10,
				},
				ConsistentHash: &composite.ConsistentHashLoadBalancerSettings{
					HttpHeaderName: "Test2",
				},
				CustomRequestHeaders:  []string{"h2"},
				CustomResponseHeaders: []string{"rh2"},
				EdgeSecurityPolicy:    "test2",
				EnableCDN:             true,
				FailoverPolicy: &composite.BackendServiceFailoverPolicy{
					FailoverRatio: 0.8,
				},
				Iap: &composite.BackendServiceIAP{
					Enabled: true,
				},
				Id:                       2,
				IpAddressSelectionPolicy: "PREFER_IPV6",
				Kind:                     "compute#backendService",
				LoadBalancingScheme:      "INTERNAL_MANAGED",
				LocalityLbPolicies: []*composite.BackendServiceLocalityLoadBalancingPolicyConfig{
					{
						CustomPolicy: &composite.BackendServiceLocalityLoadBalancingPolicyConfigCustomPolicy{
							Name: "test2",
						},
					},
				},
				LocalityLbPolicy: "ROUND_ROBIN",
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
				MaxStreamDuration: &composite.Duration{
					Seconds: 20,
				},
				Metadatas: nil,
				Name:      "test2",
				OutlierDetection: &composite.OutlierDetection{
					ConsecutiveErrors: 20,
				},
				Port:           443,
				PortName:       "https",
				Protocol:       "TCP",
				Region:         "us-central2",
				SecurityPolicy: "policyURL2",
				SecuritySettings: &composite.SecuritySettings{
					ClientTlsPolicy: "policy2",
				},
				ServiceBindings: []string{"test2"},
				ServiceLbPolicy: "INTERNAL",
				Subsetting: &composite.Subsetting{
					SubsetSize: 20,
				},
				TimeoutSec:      20,
				VpcNetworkScope: "GLOBAL_VPC_NETWORK",
				ServerResponse: googleapi.ServerResponse{
					HTTPStatusCode: 400,
				},
			},
			wantEqual: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			if res := backendSvcL4RelevantPropertiesEqual(tc.oldBackendService, tc.newBackendService) == tc.wantEqual; !res {
				t.Errorf("backendSvcL4RelevantPropertiesEqual() returned %v, expected %v. Diff(oldScv, newSvc): %s",
					res, tc.wantEqual, cmp.Diff(tc.oldBackendService, tc.newBackendService))
			}
		})
	}
}
