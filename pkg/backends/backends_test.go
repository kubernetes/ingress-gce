package backends

import (
	"context"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	compute "google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
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
		connectionTrackingPolicy    *composite.BackendServiceConnectionTrackingPolicy
	}{
		{
			desc:             "Test basic Backend Service with Internal scheme type",
			serviceName:      "test-service",
			serviceNamespace: "test-ns",
			protocol:         "TCP",
			affinityType:     string(api_v1.ServiceAffinityNone),
			schemeType:       string(cloud.SchemeInternal),
		},
		{
			desc:             "Test basic Backend Service with Internal scheme and specified Connection Tracking Policy",
			serviceName:      "test-service",
			serviceNamespace: "test-ns",
			protocol:         "TCP",
			affinityType:     string(api_v1.ServiceAffinityNone),
			schemeType:       string(cloud.SchemeInternal),
			connectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
				EnableStrongAffinity: false,
				IdleTimeoutSec:       defaultIdleTimeout,
				TrackingMode:         defaultTrackingMode,
			},
		},
		{
			desc:             "Test case with disabled session affinity but specified params",
			serviceName:      "test-service",
			serviceNamespace: "test-ns",
			protocol:         "TCP",
			affinityType:     string(api_v1.ServiceAffinityClientIP),
			schemeType:       string(cloud.SchemeExternal),
			connectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
				EnableStrongAffinity: false,
				IdleTimeoutSec:       0,
				TrackingMode:         defaultTrackingMode,
			},
		},
		{
			desc:                        "Test Backend Service with Strong Session Affinity configuration",
			serviceName:                 "test-service",
			serviceNamespace:            "test-ns",
			protocol:                    "TCP",
			affinityType:                string(api_v1.ServiceAffinityClientIP),
			schemeType:                  string(cloud.SchemeExternal),
			enableStrongSessionAffinity: true,
			connectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
				EnableStrongAffinity: true,
				IdleTimeoutSec:       prolongedIdleTimeout,
				TrackingMode:         perSessionTrackingMode,
			},
		},
		{
			desc:                        "Test Backend Service with enabled SSA but empty connectionTrackingPolicy",
			serviceName:                 "test-service",
			serviceNamespace:            "test-ns",
			protocol:                    "TCP",
			enableStrongSessionAffinity: true,
			affinityType:                string(api_v1.ServiceAffinityClientIP),
			schemeType:                  string(cloud.SchemeExternal),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			namespacedName := types.NamespacedName{Name: tc.serviceName, Namespace: tc.serviceNamespace}
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4namer := namer.NewL4Namer(kubeSystemUID, nil)
			backendPool := NewPoolWithConnectionTrackingPolicy(fakeGCE, l4namer, tc.enableStrongSessionAffinity)

			hcLink := l4namer.L4HealthCheck(tc.serviceNamespace, tc.serviceName, false)
			bsName := l4namer.L4Backend(tc.serviceNamespace, tc.serviceName)
			network := &network.NetworkInfo{IsDefault: false, NetworkURL: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"}
			backendParams := L4BackendServiceParams{
				Name:                     bsName,
				HealthCheckLink:          hcLink,
				Protocol:                 tc.protocol,
				SessionAffinity:          tc.affinityType,
				Scheme:                   tc.schemeType,
				NamespacedName:           namespacedName,
				NetworkInfo:              network,
				ConnectionTrackingPolicy: tc.connectionTrackingPolicy,
			}
			bs, _, err := backendPool.EnsureL4BackendService(backendParams, klog.TODO())
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
			if tc.enableStrongSessionAffinity {
				if diff := cmp.Diff(bs.ConnectionTrackingPolicy, tc.connectionTrackingPolicy); diff != "" {
					t.Errorf("BackendService.ConnectionTrackingPolicy was not populated correctly, expected to be different: %s", diff)
				}
			} else {
				if bs.ConnectionTrackingPolicy != nil {
					t.Errorf("ConnectionTrackingPolicy should not be set for non strong session affinity services.")
				}
			}
		})
	}
}

func TestEnsureL4BackendServiceUpdate(t *testing.T) {
	for _, tc := range []struct {
		desc                string
		serviceName         string
		serviceNamespace    string
		protocol            string
		updatedProtocol     string
		affinityType        string
		updatedAffinityType string
		schemeType          string
		expectUpdate        utils.ResourceSyncStatus
	}{
		{
			desc:                "Test no update needed",
			serviceName:         "test-service",
			serviceNamespace:    "test-ns",
			protocol:            "TCP",
			updatedProtocol:     "TCP",
			affinityType:        string(api_v1.ServiceAffinityNone),
			updatedAffinityType: string(api_v1.ServiceAffinityNone),
			schemeType:          string(cloud.SchemeInternal),
			expectUpdate:        utils.ResourceResync,
		},
		{
			desc:                "Test update protocol",
			serviceName:         "test-service",
			serviceNamespace:    "test-ns",
			protocol:            "TCP",
			updatedProtocol:     "UDP",
			affinityType:        string(api_v1.ServiceAffinityNone),
			updatedAffinityType: string(api_v1.ServiceAffinityNone),
			schemeType:          string(cloud.SchemeInternal),
			expectUpdate:        utils.ResourceUpdate,
		},
		{
			desc:                "Test update affinityType",
			serviceName:         "test-service",
			serviceNamespace:    "test-ns",
			protocol:            "TCP",
			updatedProtocol:     "TCP",
			affinityType:        string(api_v1.ServiceAffinityNone),
			updatedAffinityType: string(api_v1.ServiceAffinityClientIP),
			schemeType:          string(cloud.SchemeInternal),
			expectUpdate:        utils.ResourceUpdate,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			namespacedName := types.NamespacedName{Name: tc.serviceName, Namespace: tc.serviceNamespace}
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4namer := namer.NewL4Namer(kubeSystemUID, nil)
			backendPool := NewPoolWithConnectionTrackingPolicy(fakeGCE, l4namer, false)

			hcLink := l4namer.L4HealthCheck(tc.serviceNamespace, tc.serviceName, false)
			bsName := l4namer.L4Backend(tc.serviceNamespace, tc.serviceName)
			network := &network.NetworkInfo{IsDefault: false, NetworkURL: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"}
			backendParams := L4BackendServiceParams{
				Name:            bsName,
				HealthCheckLink: hcLink,
				Protocol:        tc.protocol,
				SessionAffinity: tc.affinityType,
				Scheme:          tc.schemeType,
				NamespacedName:  namespacedName,
				NetworkInfo:     network,
			}
			_, updated, err := backendPool.EnsureL4BackendService(backendParams, klog.TODO())
			if err != nil {
				t.Errorf("EnsureL4BackendService failed")
			}
			if !updated {
				t.Errorf("EnsureL4BackendService was supposed to return update=true but returned=false")
			}
			// do a second update to verify if no GCE resource update was done.
			_, updated, err = backendPool.EnsureL4BackendService(backendParams, klog.TODO())
			if err != nil {
				t.Errorf("EnsureL4BackendService failed")
			}
			if updated {
				t.Errorf("second EnsureL4BackendService was supposed to return update=false but returned true")
			}

			updatedBackendParams := L4BackendServiceParams{
				Name:            bsName,
				HealthCheckLink: hcLink,
				Protocol:        tc.updatedProtocol,
				SessionAffinity: tc.updatedAffinityType,
				Scheme:          tc.schemeType,
				NamespacedName:  namespacedName,
				NetworkInfo:     network,
			}
			_, updated, err = backendPool.EnsureL4BackendService(updatedBackendParams, klog.TODO())
			if err != nil {
				t.Errorf("EnsureL4BackendService failed")
			}
			if updated != tc.expectUpdate {
				t.Errorf("EnsureL4BackendService was supposed to return update=%v but returned=%v", tc.expectUpdate, updated)
			}

			// the test does not verify values since the fakeGCE impl does not support updating objects.

		})
	}
}

func TestEnsureL4BackendServiceDoesNotDetachBackends(t *testing.T) {
	// needConnectionTrackingPolicy flag for the function input
	for _, needConnectionTrackingPolicy := range []bool{false, true} {
		t.Run("Test that EnsureL4BackendService will not detach backends", func(t *testing.T) {
			serviceName := "test-service"
			serviceNamespace := "test-ns"
			namespacedName := types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
			l4namer := namer.NewL4Namer(kubeSystemUID, nil)
			backendPool := NewPoolWithConnectionTrackingPolicy(fakeGCE, l4namer, needConnectionTrackingPolicy)

			hcLink := l4namer.L4HealthCheck(serviceNamespace, serviceName, false)
			bsName := l4namer.L4Backend(serviceNamespace, serviceName)
			network := &network.NetworkInfo{IsDefault: false, NetworkURL: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"}

			backendName := "testNeg"
			existingBS := &composite.BackendService{
				Name:                bsName,
				Protocol:            "TCP",
				Description:         "test description", // this will make sure the BackendService needs update.
				HealthChecks:        []string{hcLink},
				SessionAffinity:     utils.TranslateAffinityType(string(api_v1.ServiceAffinityNone), klog.TODO()),
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

			var noConnectionTrackingPolicy *composite.BackendServiceConnectionTrackingPolicy = nil
			backendParams := L4BackendServiceParams{
				Name:                     bsName,
				HealthCheckLink:          hcLink,
				Protocol:                 "TCP",
				SessionAffinity:          string(api_v1.ServiceAffinityNone),
				Scheme:                   string(cloud.SchemeInternal),
				NamespacedName:           namespacedName,
				NetworkInfo:              network,
				ConnectionTrackingPolicy: noConnectionTrackingPolicy,
			}
			bs, _, err := backendPool.EnsureL4BackendService(backendParams, klog.TODO())
			if err != nil {
				t.Errorf("EnsureL4BackendService failed")
			}
			if len(bs.Backends) == 0 {
				t.Fatalf("expected backends to be still attached to the backend service but there were none")
			}
			backend := bs.Backends[0]
			if backend.Group != backendName {
				t.Errorf("")
			}
			if diff := cmp.Diff(backend, existingBS.Backends[0]); diff != "" {
				t.Errorf("BackendService.Backends were changed, expected no change: %s", diff)
			}
			description, err := utils.MakeL4LBServiceDescription(namespacedName.String(), "", meta.VersionGA, false, utils.ILB)
			if err != nil {
				t.Errorf("utils.MakeL4LBServiceDescription() failed %v", err)
			}
			if bs.Description != description {
				t.Errorf("BackendService.Description was not populated correctly, want=%q, got=%q", description, bs.Description)
			}
		})
	}

}

// TestBackendSvcEqual checks that backendSvcEqual() and
// connectionTrackingPolicyEqual() (as a part ofit  backendSvcEqual)
// return expected results for two resources compared.
func TestBackendSvcEqual(t *testing.T) {
	for _, tc := range []struct {
		desc                           string
		oldBackendService              *composite.BackendService
		newBackendService              *composite.BackendService
		compareConnectionTracking      bool
		withZonalAffinityEnabled       bool
		withL4LoggingManagementEnabled bool
		wantEqual                      bool
		skipBidirectionalCheck         bool
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
				SessionAffinity:     string(api_v1.ServiceAffinityClientIP),
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
				SessionAffinity:     string(api_v1.ServiceAffinityClientIP),
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
			desc:                      "Test with changed idle timeout",
			compareConnectionTracking: true,
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
			desc:                      "Test to check if an empty structure will cause a change",
			compareConnectionTracking: true,
			oldBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					IdleTimeoutSec: defaultIdleTimeout,
				},
			},
			newBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: nil,
			},
			wantEqual: false,
		},
		{
			desc:                      "Test to check if a default empty structure is equal to nil",
			compareConnectionTracking: true,
			oldBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{},
			},
			newBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: nil,
			},
			wantEqual: false,
		},
		{
			desc:                      "Test to check if function ignores nil connection tracking policy",
			compareConnectionTracking: false,
			oldBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: nil,
			},
			newBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					IdleTimeoutSec: prolongedIdleTimeout,
				},
			},
			wantEqual: true,
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
			desc:                      "Test with changed TrackingMode",
			compareConnectionTracking: true,
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
			desc: "Test with changed health-checks with Zonal Affinity Enabled",
			oldBackendService: &composite.BackendService{
				HealthChecks: []string{"abc"},
			},
			newBackendService: &composite.BackendService{
				HealthChecks: []string{"abc", "xyz"},
			},
			withZonalAffinityEnabled: true,
			wantEqual:                false,
		},
		{
			desc: "Test with same health-checks version v1-beta",
			oldBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/v1/abc"},
			},
			newBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/beta/abc"},
			},
			withZonalAffinityEnabled: true,
			wantEqual:                true,
		},
		{
			desc: "Test with same health-checks version beta-v1",
			oldBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/beta/abc"},
			},
			newBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/v1/abc"},
			},
			withZonalAffinityEnabled: true,
			wantEqual:                true,
		},
		{
			desc: "Test with changed health-checks version beta-beta",
			oldBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/beta/abc"},
			},
			newBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/beta/abcd"},
			},
			withZonalAffinityEnabled: true,
			wantEqual:                false,
		},
		{
			desc: "Test with changed first part of health-checks version v1-v1",
			oldBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/v1/abc"},
			},
			newBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.google.com/compute/v1/abc"},
			},
			withZonalAffinityEnabled: true,
			wantEqual:                false,
		},
		{
			desc: "Test with changed health-checks version beta-v1",
			oldBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/v1/abc"},
			},
			newBackendService: &composite.BackendService{
				HealthChecks: []string{"https://www.googleapis.com/compute/beta/abcd"},
			},
			withZonalAffinityEnabled: true,
			wantEqual:                false,
		},
		{
			desc: "Test unmanaged logging changed",
			oldBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: false,
				},
			},
			newBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
			},
			withL4LoggingManagementEnabled: false,
			wantEqual:                      true,
		},
		{
			desc: "Test managed logging - enable",
			oldBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: false,
				},
			},
			newBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
			},
			withL4LoggingManagementEnabled: true,
			wantEqual:                      false,
		},
		{
			desc: "Test managed logging - sample rate",
			oldBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					SampleRate: 0.2,
				},
			},
			newBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					SampleRate: 0.3,
				},
			},
			withL4LoggingManagementEnabled: true,
			wantEqual:                      false,
		},
		{
			desc: "Test managed logging - optional mode",
			oldBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					OptionalMode: "EXCLUDE_ALL_OPTIONAL",
				},
			},
			newBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					OptionalMode: "INCLUDE_ALL_OPTIONAL",
				},
			},
			withL4LoggingManagementEnabled: true,
			wantEqual:                      false,
		},
		{
			desc: "Test managed logging - optional fields",
			oldBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					OptionalFields: []string{"field1"},
				},
			},
			newBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					OptionalFields: []string{"field1", "field2"},
				},
			},
			withL4LoggingManagementEnabled: true,
			wantEqual:                      false,
		},
		{
			desc: "Test managed - logging config added",
			oldBackendService: &composite.BackendService{
				LogConfig: nil,
			},
			newBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
			},
			withL4LoggingManagementEnabled: true,
			wantEqual:                      false,
		},
		{
			desc: "Test unmanaged - logging config added",
			oldBackendService: &composite.BackendService{
				LogConfig: nil,
			},
			newBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
			},
			withL4LoggingManagementEnabled: false,
			wantEqual:                      true,
		},
		{
			desc: "Test managed - logging config removed",
			oldBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
			},
			newBackendService: &composite.BackendService{
				LogConfig: nil,
			},
			withL4LoggingManagementEnabled: true,
			wantEqual:                      false,
		},
		{
			desc: "Test unmanaged - logging config removed",
			oldBackendService: &composite.BackendService{
				LogConfig: &composite.BackendServiceLogConfig{
					Enable: true,
				},
			},
			newBackendService: &composite.BackendService{
				LogConfig: nil,
			},
			withL4LoggingManagementEnabled: false,
			wantEqual:                      true,
		},
		{
			desc: "Test managed - no logging config",
			oldBackendService: &composite.BackendService{
				LogConfig: nil,
			},
			newBackendService: &composite.BackendService{
				LogConfig: nil,
			},
			withL4LoggingManagementEnabled: true,
			wantEqual:                      true,
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
			desc:                      "Test with a few changed parameters",
			compareConnectionTracking: true,
			oldBackendService: &composite.BackendService{
				SessionAffinity:     string(api_v1.ServiceAffinityClientIP),
				LoadBalancingScheme: string(cloud.SchemeInternal),
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: false,
					IdleTimeoutSec:       defaultIdleTimeout,
				},
			},
			newBackendService: &composite.BackendService{
				SessionAffinity:     string(api_v1.ServiceAffinityNone),
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
			desc:                      "Test with ignoring connection tracking",
			compareConnectionTracking: false,
			oldBackendService: &composite.BackendService{
				Description:         "same_description",
				Protocol:            "TCP",
				SessionAffinity:     string(api_v1.ServiceAffinityClientIP),
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
				SessionAffinity:     string(api_v1.ServiceAffinityClientIP),
				LoadBalancingScheme: string(cloud.SchemeExternal),
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: true,
					IdleTimeoutSec:       100,
					TrackingMode:         "otherTrackingMode",
				},
			},
			wantEqual: true,
		},
		{
			desc: "Test prevent unnecessary NetLB recreation",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			wantEqual: true,
		},
		{
			desc: "Test prevent unsupported disablement of Maglev - NetLB",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			wantEqual: true,
		},
		{
			desc: "Test backend service diff with NetLB weighted load balancing pods-per-node ENABLED",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyWeightedMaglev),
			},
			wantEqual: false,
		},
		{
			desc: "Test backend service diff with ILB weighted load balancing pods-per-node ENABLED",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyWeightedRendezvous),
			},
			wantEqual: false,
		},
		{
			desc: "Test backend service diff with weighted load balancing pods-per-node DISABLED",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			wantEqual: true,
		},
		{
			desc: "Test backend service diff disable NetLB weighted load balancing pods-per-node",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyWeightedMaglev),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			wantEqual: false,
		},
		{
			desc: "Test backend service diff disable ILB weighted load balancing pods-per-node",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyWeightedRendezvous),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			wantEqual: false,
		},
		{
			desc: "Test backend service diff ILB post-disabling weighted load balancing pods-per-node",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyWeightedRendezvous),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLbPolicyRendezvous),
			},
			wantEqual: false,
		},
		{
			desc: "Test prevent ILB resync failure after GCP_RENDEZVOUS is defaulted",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLbPolicyRendezvous),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			wantEqual:              true,
			skipBidirectionalCheck: true,
		},
		{
			desc: "Test GCP_RENDEZVOUS not set as ILB default",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLbPolicyRendezvous),
			},
			wantEqual:              false,
			skipBidirectionalCheck: true,
		},
		{
			desc: "Test LocalityLbPolicy equal does not override the other params",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
				Network:          "network-1",
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			wantEqual: false,
		},
		{
			desc:                      "Test backend service diff with weighted load balancing pods-per-node and connection tracking enabled",
			compareConnectionTracking: true,
			oldBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: false,
					IdleTimeoutSec:       defaultIdleTimeout,
				},
				LocalityLbPolicy: string(LocalityLBPolicyWeightedMaglev),
			},
			newBackendService: &composite.BackendService{
				ConnectionTrackingPolicy: &composite.BackendServiceConnectionTrackingPolicy{
					EnableStrongAffinity: true,
					IdleTimeoutSec:       prolongedIdleTimeout,
				},
				LocalityLbPolicy: string(LocalityLBPolicyWeightedMaglev),
			},
			wantEqual: false,
		},
		{
			desc: "Test existing backend service diff with zonal affinity feature enabled",
			oldBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_SPILL_CROSS_ZONE",
						SpilloverRatio: 0.7,
					},
				},
			},
			newBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_SPILL_CROSS_ZONE",
						SpilloverRatio: 0.7,
					},
				},
			},
			wantEqual: true,
		},
		{
			desc: "Test existing backend service diff with zonal affinity feature enabled but different ratio",
			oldBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_SPILL_CROSS_ZONE",
						SpilloverRatio: 0.7,
					},
				},
			},
			newBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_SPILL_CROSS_ZONE",
						SpilloverRatio: 0.3,
					},
				},
			},
			wantEqual: false,
		},
		{
			desc: "Test existing backend service diff with zonal affinity feature enabled but different spillover strategy",
			oldBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_STAY_WITHIN_ZONE",
						SpilloverRatio: 0.3,
					},
				},
			},
			newBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_SPILL_CROSS_ZONE",
						SpilloverRatio: 0.3,
					},
				},
			},
			wantEqual: false,
		},
		{
			desc:              "Test existing backend service diff enabling zonal affinity feature",
			oldBackendService: &composite.BackendService{},
			newBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_SPILL_CROSS_ZONE",
						SpilloverRatio: 0.3,
					},
				},
			},
			wantEqual: false,
		},
		{
			desc: "Test existing backend service diff enabling zonal affinity feature",
			oldBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_SPILL_CROSS_ZONE",
						SpilloverRatio: 0.3,
					},
				},
			},
			newBackendService: &composite.BackendService{},
			wantEqual:         false,
		},
		{
			desc: "Test existing backend service diff with zonal affinity disables",
			oldBackendService: &composite.BackendService{
				NetworkPassThroughLbTrafficPolicy: &composite.BackendServiceNetworkPassThroughLbTrafficPolicy{
					ZonalAffinity: &composite.BackendServiceNetworkPassThroughLbTrafficPolicyZonalAffinity{
						Spillover:      "ZONAL_AFFINITY_DISABLED",
						SpilloverRatio: 0,
					},
				},
			},
			newBackendService: &composite.BackendService{},
			wantEqual:         true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			oldZonalAffinityFlag := flags.F.EnableL4ILBZonalAffinity
			oldLoggingFlag := flags.F.ManageL4LBLogging
			flags.F.EnableL4ILBZonalAffinity = tc.withZonalAffinityEnabled
			flags.F.ManageL4LBLogging = tc.withL4LoggingManagementEnabled
			defer func() {
				flags.F.EnableL4ILBZonalAffinity = oldZonalAffinityFlag
				flags.F.ManageL4LBLogging = oldLoggingFlag
			}()

			result := backendSvcEqual(tc.newBackendService, tc.oldBackendService, tc.compareConnectionTracking)
			if result != tc.wantEqual {
				t.Errorf("backendSvcEqual() returned %v, expected %v. Diff(oldScv, newSvc): %s",
					result, tc.wantEqual, cmp.Diff(tc.oldBackendService, tc.newBackendService))
			}
			if !tc.skipBidirectionalCheck && result != backendSvcEqual(tc.oldBackendService, tc.newBackendService, tc.compareConnectionTracking) {
				t.Error("result from backendSvcEqual(old, new) should be the same as backendSvcEqual(new, old)")
			}
		})
	}
}

func TestUpdateLocalityLBPolicy(t *testing.T) {
	testCases := []struct {
		desc                       string
		existingBSLocalityLbPolicy LocalityLBPolicyType
		updatedBSLocalityLbPolicy  LocalityLBPolicyType
		wantBSLocalityLbPolicy     LocalityLBPolicyType
	}{
		{
			desc:                       "from MAGLEV to MAGLEV",
			existingBSLocalityLbPolicy: LocalityLBPolicyMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyMaglev,
		},
		{
			desc:                       "from empty to WEIGHTED_MAGLEV NetLB",
			existingBSLocalityLbPolicy: LocalityLBPolicyDefault,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyWeightedMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyWeightedMaglev,
		},
		{
			desc:                       "from empty to MAGLEV NetLB",
			existingBSLocalityLbPolicy: LocalityLBPolicyDefault,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyDefault,
		},
		{
			desc:                       "from MAGLEV to empty NetLB",
			existingBSLocalityLbPolicy: LocalityLBPolicyMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyDefault,
			wantBSLocalityLbPolicy:     LocalityLBPolicyMaglev,
		},
		{
			desc:                       "from MAGLEV to WEIGHTED_MAGLEV NetLB",
			existingBSLocalityLbPolicy: LocalityLBPolicyMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyWeightedMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyWeightedMaglev,
		},
		{
			desc:                       "from WEIGHTED_MAGLEV to MAGLEV NetLB",
			existingBSLocalityLbPolicy: LocalityLBPolicyWeightedMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyMaglev,
		},
		{
			desc:                       "from WEIGHTED_MAGLEV to empty ILB - allowlisted",
			existingBSLocalityLbPolicy: LocalityLBPolicyWeightedMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyDefault,
			wantBSLocalityLbPolicy:     LocalityLBPolicyDefault,
		},
		{
			desc:                       "from empty to GCP_RENDEZVOUS",
			existingBSLocalityLbPolicy: LocalityLBPolicyDefault,
			updatedBSLocalityLbPolicy:  LocalityLbPolicyRendezvous,
			wantBSLocalityLbPolicy:     LocalityLbPolicyRendezvous,
		},
		{
			desc:                       "from empty to WEIGHTED_GCP_RENDEZVOUS",
			existingBSLocalityLbPolicy: LocalityLBPolicyDefault,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyWeightedRendezvous,
			wantBSLocalityLbPolicy:     LocalityLBPolicyWeightedRendezvous,
		},
		{
			desc:                       "from GCP_RENDEZVOUS to empty",
			existingBSLocalityLbPolicy: LocalityLbPolicyRendezvous,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyDefault,
			wantBSLocalityLbPolicy:     LocalityLbPolicyRendezvous,
		},
		{
			desc:                       "from GCP_RENDEZVOUS to WEIGHTED_GCP_RENDEZVOUS",
			existingBSLocalityLbPolicy: LocalityLbPolicyRendezvous,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyWeightedRendezvous,
			wantBSLocalityLbPolicy:     LocalityLBPolicyWeightedRendezvous,
		},
		{
			desc:                       "from WEIGHTED_GCP_RENDEZVOUS to GCP_RENDEZVOUS",
			existingBSLocalityLbPolicy: LocalityLBPolicyWeightedRendezvous,
			updatedBSLocalityLbPolicy:  LocalityLbPolicyRendezvous,
			wantBSLocalityLbPolicy:     LocalityLbPolicyRendezvous,
		},
		{
			desc:                       "from WEIGHTED_GCP_RENDEZVOUS to empty ILB",
			existingBSLocalityLbPolicy: LocalityLBPolicyWeightedRendezvous,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyDefault,
			wantBSLocalityLbPolicy:     LocalityLBPolicyDefault,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			serviceName := "test-service"
			serviceNamespace := "test-ns"
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
			l4namer := namer.NewL4Namer(kubeSystemUID, nil)
			backendPool := NewPool(fakeGCE, l4namer)
			bsName := l4namer.L4Backend(serviceNamespace, serviceName)
			namespacedName := types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}
			description, err := utils.MakeL4LBServiceDescription(namespacedName.String(), "", meta.VersionGA, false, utils.ILB)

			key, err := composite.CreateKey(fakeGCE, bsName, meta.Regional)
			if err != nil {
				t.Fatalf("failed to create key %v", err)
			}
			existingBS := &composite.BackendService{
				Name:             bsName,
				Description:      description,
				LocalityLbPolicy: string(tc.existingBSLocalityLbPolicy),
				SessionAffinity:  "NONE",
				HealthChecks:     []string{""},
			}
			err = composite.CreateBackendService(fakeGCE, key, existingBS, klog.TODO())
			if err != nil {
				t.Fatalf("failed to create the existing backend service: %v", err)
			}

			updateBackendParams := L4BackendServiceParams{
				Name:             bsName,
				NamespacedName:   namespacedName,
				LocalityLbPolicy: tc.updatedBSLocalityLbPolicy,
			}
			updatedBS, _, err := backendPool.EnsureL4BackendService(updateBackendParams, klog.TODO())
			if updatedBS.LocalityLbPolicy != string(tc.wantBSLocalityLbPolicy) {
				t.Errorf("Update LocalityLbPolicy failed, got: %v, want %v", updatedBS.LocalityLbPolicy, tc.wantBSLocalityLbPolicy)
			}
		})
	}
}

func TestSelectAPIVersionForUpdate(t *testing.T) {
	testCases := []struct {
		desc     string
		versionA meta.Version
		versionB meta.Version
		want     meta.Version
	}{
		{
			desc:     "Alpha wins over GA",
			versionA: meta.VersionAlpha,
			versionB: meta.VersionGA,
			want:     meta.VersionAlpha,
		},
		{
			desc:     "Alpha wins over Beta",
			versionA: meta.VersionAlpha,
			versionB: meta.VersionBeta,
			want:     meta.VersionAlpha,
		},
		{
			desc:     "Beta wins over GA",
			versionA: meta.VersionBeta,
			versionB: meta.VersionGA,
			want:     meta.VersionBeta,
		},
		{
			desc:     "GA when both are GA",
			versionA: meta.VersionGA,
			versionB: meta.VersionGA,
			want:     meta.VersionGA,
		},
		{
			desc:     "Alpha when both are Alpha",
			versionA: meta.VersionAlpha,
			versionB: meta.VersionAlpha,
			want:     meta.VersionAlpha,
		},
		{
			desc:     "Invalid version defaults to GA precedence",
			versionA: "invalid",
			versionB: meta.VersionGA,
			want:     meta.VersionGA,
		},
		{
			desc:     "Alpha wins over invalid",
			versionA: meta.VersionAlpha,
			versionB: "invalid",
			want:     meta.VersionAlpha,
		},
		{
			desc:     "Beta wins over invalid",
			versionA: meta.VersionBeta,
			versionB: "invalid",
			want:     meta.VersionBeta,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			result := selectApiVersionForUpdate(tc.versionA, tc.versionB)
			if result != tc.want {
				t.Errorf("selectApiVersionForUpdate(%s, %s) = %s, want %s", tc.versionA, tc.versionB, result, tc.want)
			}

			// Test symmetry - order of arguments shouldn't matter
			reverseResult := selectApiVersionForUpdate(tc.versionB, tc.versionA)
			if reverseResult != result {
				t.Errorf("selectApiVersionForUpdate not symmetric: selectApiVersionForUpdate(%s, %s) = %s, but selectApiVersionForUpdate(%s, %s) = %s",
					tc.versionA, tc.versionB, result, tc.versionB, tc.versionA, reverseResult)
			}
		})
	}
}

func TestFeatureVersionRequirements(t *testing.T) {
	testCases := []struct {
		desc                string
		enableZonalAffinity bool
		expectedVersion     meta.Version
	}{
		{
			desc:                "Zonal affinity requires Beta",
			enableZonalAffinity: true,
			expectedVersion:     meta.VersionBeta,
		},
		{
			desc:                "No special features defaults to GA",
			enableZonalAffinity: false,
			expectedVersion:     meta.VersionGA,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			params := L4BackendServiceParams{
				EnableZonalAffinity: tc.enableZonalAffinity,
			}

			version := apiVersionRequiredbyServiceFeatures(params)
			if version != tc.expectedVersion {
				t.Errorf("apiVersionRequiredbyServiceFeatures returned %s, want %s", version, tc.expectedVersion)
			}
		})
	}
}

func TestVersionSelectionInUpdate(t *testing.T) {
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	l4namer := namer.NewL4Namer(kubeSystemUID, nil)
	backendPool := NewPool(fakeGCE, l4namer)

	serviceName := "test-service"
	serviceNamespace := "test-ns"
	namespacedName := types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}
	bsName := l4namer.L4Backend(serviceNamespace, serviceName)

	testCases := []struct {
		desc                string
		initialVersion      meta.Version
		descriptionVersion  meta.Version
		enableZonalAffinity bool
		expectedVersion     meta.Version
	}{
		{
			desc:                "Version from description is higher",
			initialVersion:      meta.VersionGA,
			descriptionVersion:  meta.VersionAlpha,
			enableZonalAffinity: false,
			expectedVersion:     meta.VersionAlpha,
		},
		{
			desc:                "Version from features is higher",
			initialVersion:      meta.VersionGA,
			descriptionVersion:  meta.VersionGA,
			enableZonalAffinity: true,
			expectedVersion:     meta.VersionBeta,
		},
		{
			desc:                "Max version when both high",
			initialVersion:      meta.VersionGA,
			descriptionVersion:  meta.VersionBeta,
			enableZonalAffinity: true,
			expectedVersion:     meta.VersionBeta,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// Create backend service with initial version
			key, err := composite.CreateKey(fakeGCE, bsName, meta.Regional)
			if err != nil {
				t.Fatalf("Failed to create key: %v", err)
			}

			// Create description with specified version
			desc, err := utils.MakeL4LBServiceDescription(namespacedName.String(), "", tc.descriptionVersion, false, utils.ILB)
			if err != nil {
				t.Fatalf("Failed to create description: %v", err)
			}

			existingBS := &composite.BackendService{
				Name:         bsName,
				Version:      tc.initialVersion,
				Description:  desc,
				HealthChecks: []string{""},
			}

			// Create or update the backend service
			if err := composite.CreateBackendService(fakeGCE, key, existingBS, klog.TODO()); err != nil {
				// If it already exists, try updating it
				if err := composite.UpdateBackendService(fakeGCE, key, existingBS, klog.TODO()); err != nil {
					t.Fatalf("Failed to create/update backend service: %v", err)
				}
			}

			// Use EnsureL4BackendService with zonal affinity param
			backendParams := L4BackendServiceParams{
				Name:                bsName,
				HealthCheckLink:     "health-check-link",
				Protocol:            "TCP",
				SessionAffinity:     "NONE",
				Scheme:              string(cloud.SchemeInternal),
				NamespacedName:      namespacedName,
				EnableZonalAffinity: tc.enableZonalAffinity,
			}

			updatedBS, _, err := backendPool.EnsureL4BackendService(backendParams, klog.TODO())
			if err != nil {
				t.Fatalf("EnsureL4BackendService failed: %v", err)
			}

			// Verify that the version was correctly selected
			if updatedBS.Version != tc.expectedVersion {
				t.Errorf("EnsureL4BackendService set version to %s, want %s", updatedBS.Version, tc.expectedVersion)
			}
		})
	}
}

func TestConnectionDrainingTimeout(t *testing.T) {
	for _, tc := range []struct {
		desc                         string
		protocol                     string
		connectionDrainingTimeoutSec int64
		expectedTimeout              int64
	}{
		{
			desc:                         "Default timeout when not specified",
			protocol:                     "TCP",
			connectionDrainingTimeoutSec: 0,
			expectedTimeout:              DefaultConnectionDrainingTimeoutSeconds,
		},
		{
			desc:                         "Custom timeout specified",
			protocol:                     "TCP",
			connectionDrainingTimeoutSec: 300,
			expectedTimeout:              300,
		},
		{
			desc:                         "Maximum timeout value",
			protocol:                     "TCP",
			connectionDrainingTimeoutSec: 3600,
			expectedTimeout:              3600,
		},
		{
			desc:                         "UDP protocol should have 0 timeout",
			protocol:                     "UDP",
			connectionDrainingTimeoutSec: 300,
			expectedTimeout:              0,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			serviceName := "test-service"
			serviceNamespace := "test-ns"
			namespacedName := types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			l4namer := namer.NewL4Namer(kubeSystemUID, nil)
			backendPool := NewPool(fakeGCE, l4namer)

			hcLink := l4namer.L4HealthCheck(serviceNamespace, serviceName, false)
			bsName := l4namer.L4Backend(serviceNamespace, serviceName)
			network := &network.NetworkInfo{IsDefault: true}

			backendParams := L4BackendServiceParams{
				Name:                         bsName,
				HealthCheckLink:              hcLink,
				Protocol:                     tc.protocol,
				SessionAffinity:              string(api_v1.ServiceAffinityNone),
				Scheme:                       string(cloud.SchemeInternal),
				NamespacedName:               namespacedName,
				NetworkInfo:                  network,
				ConnectionDrainingTimeoutSec: tc.connectionDrainingTimeoutSec,
			}

			bs, _, err := backendPool.EnsureL4BackendService(backendParams, klog.TODO())
			if err != nil {
				t.Errorf("EnsureL4BackendService failed: %v", err)
			}

			if bs.ConnectionDraining == nil {
				t.Errorf("BackendService.ConnectionDraining is nil")
			} else if bs.ConnectionDraining.DrainingTimeoutSec != tc.expectedTimeout {
				t.Errorf("BackendService.ConnectionDraining.DrainingTimeoutSec = %d, want %d", bs.ConnectionDraining.DrainingTimeoutSec, tc.expectedTimeout)
			}
		})
	}
}

func TestConnectionDrainingTimeoutPreserveManualOverride(t *testing.T) {
	serviceName := "test-service"
	serviceNamespace := "test-ns"
	namespacedName := types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = mock.UpdateRegionBackendServiceHook
	l4namer := namer.NewL4Namer(kubeSystemUID, nil)
	backendPool := NewPool(fakeGCE, l4namer)

	hcLink := l4namer.L4HealthCheck(serviceNamespace, serviceName, false)
	bsName := l4namer.L4Backend(serviceNamespace, serviceName)
	network := &network.NetworkInfo{IsDefault: true}

	// Create initial backend service with default timeout
	backendParams := L4BackendServiceParams{
		Name:                         bsName,
		HealthCheckLink:              hcLink,
		Protocol:                     "TCP",
		SessionAffinity:              string(api_v1.ServiceAffinityNone),
		Scheme:                       string(cloud.SchemeInternal),
		NamespacedName:               namespacedName,
		NetworkInfo:                  network,
		ConnectionDrainingTimeoutSec: 0, // Not specified
	}

	_, _, err := backendPool.EnsureL4BackendService(backendParams, klog.TODO())
	if err != nil {
		t.Fatalf("EnsureL4BackendService failed: %v", err)
	}

	// Manually update the backend service timeout (simulating gcloud update)
	key, err := composite.CreateKey(fakeGCE, bsName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}
	bs, err := composite.GetBackendService(fakeGCE, key, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}
	bs.ConnectionDraining = &composite.ConnectionDraining{DrainingTimeoutSec: 600}
	if err := composite.UpdateBackendService(fakeGCE, key, bs, klog.TODO()); err != nil {
		t.Fatalf("Failed to manually update backend service: %v", err)
	}

	// Ensure backend service again without annotation - should preserve manual override
	_, _, err = backendPool.EnsureL4BackendService(backendParams, klog.TODO())
	if err != nil {
		t.Fatalf("EnsureL4BackendService failed: %v", err)
	}

	// Verify manual override was preserved
	bs, err = composite.GetBackendService(fakeGCE, key, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}
	if bs.ConnectionDraining == nil || bs.ConnectionDraining.DrainingTimeoutSec != 600 {
		t.Errorf("Manual override was not preserved. Got timeout=%v, want 600", bs.ConnectionDraining)
	}

	// Now set annotation - should override the manual setting
	backendParams.ConnectionDrainingTimeoutSec = 1800
	_, _, err = backendPool.EnsureL4BackendService(backendParams, klog.TODO())
	if err != nil {
		t.Fatalf("EnsureL4BackendService with annotation failed: %v", err)
	}

	// Verify annotation took precedence
	bs, err = composite.GetBackendService(fakeGCE, key, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}
	if bs.ConnectionDraining == nil || bs.ConnectionDraining.DrainingTimeoutSec != 1800 {
		t.Errorf("Annotation value was not applied. Got timeout=%v, want 1800", bs.ConnectionDraining)
	}
}

func TestConnectionDrainingTimeoutNoUnnecessaryUpdate(t *testing.T) {
	serviceName := "test-service"
	serviceNamespace := "test-ns"
	namespacedName := types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())

	// Track update calls
	updateCallCount := 0
	(fakeGCE.Compute().(*cloud.MockGCE)).MockRegionBackendServices.UpdateHook = func(ctx context.Context, key *meta.Key, be *compute.BackendService, m *cloud.MockRegionBackendServices, options ...cloud.Option) error {
		updateCallCount++
		return mock.UpdateRegionBackendServiceHook(ctx, key, be, m, options...)
	}

	l4namer := namer.NewL4Namer(kubeSystemUID, nil)
	backendPool := NewPool(fakeGCE, l4namer)

	hcLink := l4namer.L4HealthCheck(serviceNamespace, serviceName, false)
	bsName := l4namer.L4Backend(serviceNamespace, serviceName)
	network := &network.NetworkInfo{IsDefault: true}

	// Create initial backend service with default timeout
	backendParams := L4BackendServiceParams{
		Name:                         bsName,
		HealthCheckLink:              hcLink,
		Protocol:                     "TCP",
		SessionAffinity:              string(api_v1.ServiceAffinityNone),
		Scheme:                       string(cloud.SchemeInternal),
		NamespacedName:               namespacedName,
		NetworkInfo:                  network,
		ConnectionDrainingTimeoutSec: 0, // Not specified
	}

	_, _, err := backendPool.EnsureL4BackendService(backendParams, klog.TODO())
	if err != nil {
		t.Fatalf("EnsureL4BackendService failed: %v", err)
	}
	initialUpdateCount := updateCallCount

	// Manually update the backend service timeout (simulating gcloud update)
	key, err := composite.CreateKey(fakeGCE, bsName, meta.Regional)
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}
	bs, err := composite.GetBackendService(fakeGCE, key, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}
	bs.ConnectionDraining = &composite.ConnectionDraining{DrainingTimeoutSec: 600}
	if err := composite.UpdateBackendService(fakeGCE, key, bs, klog.TODO()); err != nil {
		t.Fatalf("Failed to manually update backend service: %v", err)
	}
	updateCountAfterManual := updateCallCount

	// Ensure backend service again without annotation - should preserve manual override WITHOUT triggering update
	_, _, err = backendPool.EnsureL4BackendService(backendParams, klog.TODO())
	if err != nil {
		t.Fatalf("EnsureL4BackendService failed: %v", err)
	}

	// Verify no update call was made (preservation happens before comparison)
	if updateCallCount != updateCountAfterManual {
		t.Errorf("Unnecessary update call made. Update count increased from %d to %d", updateCountAfterManual, updateCallCount)
	}

	// Verify manual override was preserved
	bs, err = composite.GetBackendService(fakeGCE, key, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}
	if bs.ConnectionDraining == nil || bs.ConnectionDraining.DrainingTimeoutSec != 600 {
		t.Errorf("Manual override was not preserved. Got timeout=%v, want 600", bs.ConnectionDraining)
	}

	// Verify that setting annotation DOES trigger an update
	backendParams.ConnectionDrainingTimeoutSec = 1800
	updateCountBeforeAnnotation := updateCallCount
	_, _, err = backendPool.EnsureL4BackendService(backendParams, klog.TODO())
	if err != nil {
		t.Fatalf("EnsureL4BackendService with annotation failed: %v", err)
	}

	// Verify an update was made for the annotation change
	if updateCallCount <= updateCountBeforeAnnotation {
		t.Errorf("Expected update call for annotation change. Update count did not increase from %d", updateCountBeforeAnnotation)
	}

	// Verify annotation took precedence
	bs, err = composite.GetBackendService(fakeGCE, key, meta.VersionGA, klog.TODO())
	if err != nil {
		t.Fatalf("Failed to get backend service: %v", err)
	}
	if bs.ConnectionDraining == nil || bs.ConnectionDraining.DrainingTimeoutSec != 1800 {
		t.Errorf("Annotation value was not applied. Got timeout=%v, want 1800", bs.ConnectionDraining)
	}

	// Final check: reconcile with annotation again - should NOT trigger update since value matches
	updateCountBeforeFinalReconcile := updateCallCount
	_, _, err = backendPool.EnsureL4BackendService(backendParams, klog.TODO())
	if err != nil {
		t.Fatalf("Final EnsureL4BackendService failed: %v", err)
	}
	if updateCallCount != updateCountBeforeFinalReconcile {
		t.Errorf("Unnecessary update on final reconcile. Update count increased from %d to %d", updateCountBeforeFinalReconcile, updateCallCount)
	}

	t.Logf("Test passed. Total update calls: initial=%d, afterManual=%d, final=%d", initialUpdateCount, updateCountAfterManual, updateCallCount)
}
