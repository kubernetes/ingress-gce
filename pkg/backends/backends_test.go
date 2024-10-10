package backends

import (
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
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
			affinityType:     string(v1.ServiceAffinityNone),
			schemeType:       string(cloud.SchemeInternal),
		},
		{
			desc:             "Test basic Backend Service with Internal scheme and specified Connection Tracking Policy",
			serviceName:      "test-service",
			serviceNamespace: "test-ns",
			protocol:         "TCP",
			affinityType:     string(v1.ServiceAffinityNone),
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
			affinityType:     string(v1.ServiceAffinityClientIP),
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
			affinityType:                string(v1.ServiceAffinityClientIP),
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
			affinityType:                string(v1.ServiceAffinityClientIP),
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
			affinityType:        string(v1.ServiceAffinityNone),
			updatedAffinityType: string(v1.ServiceAffinityNone),
			schemeType:          string(cloud.SchemeInternal),
			expectUpdate:        utils.ResourceResync,
		},
		{
			desc:                "Test update protocol",
			serviceName:         "test-service",
			serviceNamespace:    "test-ns",
			protocol:            "TCP",
			updatedProtocol:     "UDP",
			affinityType:        string(v1.ServiceAffinityNone),
			updatedAffinityType: string(v1.ServiceAffinityNone),
			schemeType:          string(cloud.SchemeInternal),
			expectUpdate:        utils.ResourceUpdate,
		},
		{
			desc:                "Test update affinityType",
			serviceName:         "test-service",
			serviceNamespace:    "test-ns",
			protocol:            "TCP",
			updatedProtocol:     "TCP",
			affinityType:        string(v1.ServiceAffinityNone),
			updatedAffinityType: string(v1.ServiceAffinityClientIP),
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
				SessionAffinity:     utils.TranslateAffinityType(string(v1.ServiceAffinityNone), klog.TODO()),
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
				SessionAffinity:          string(v1.ServiceAffinityNone),
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
		desc                      string
		oldBackendService         *composite.BackendService
		newBackendService         *composite.BackendService
		compareConnectionTracking bool
		wantEqual                 bool
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
			desc:                      "Test with ignoring connection tracking",
			compareConnectionTracking: false,
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
					EnableStrongAffinity: true,
					IdleTimeoutSec:       100,
					TrackingMode:         "otherTrackingMode",
				},
			},
			wantEqual: true,
		},
		{
			desc: "Test existing backend service diff with weighted load balancing feature enabled",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			wantEqual: true,
		},
		{
			desc: "Test backend service diff with weighted load balancing pods-per-node ENABLED",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyDefault),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyWeightedMaglev),
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
			desc: "Test backend service diff disable weighted load balancing pods-per-node",
			oldBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyWeightedMaglev),
			},
			newBackendService: &composite.BackendService{
				LocalityLbPolicy: string(LocalityLBPolicyMaglev),
			},
			wantEqual: false,
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
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			result := backendSvcEqual(tc.newBackendService, tc.oldBackendService, tc.compareConnectionTracking)
			if result != tc.wantEqual {
				t.Errorf("backendSvcEqual() returned %v, expected %v. Diff(oldScv, newSvc): %s",
					result, tc.wantEqual, cmp.Diff(tc.oldBackendService, tc.newBackendService))
			}
			if result != backendSvcEqual(tc.oldBackendService, tc.newBackendService, tc.compareConnectionTracking) {
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
			desc:                       "from empty to WEIGHTED_MAGLEV",
			existingBSLocalityLbPolicy: LocalityLBPolicyDefault,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyWeightedMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyWeightedMaglev,
		},
		{
			desc:                       "from empty to MAGLEV",
			existingBSLocalityLbPolicy: LocalityLBPolicyDefault,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyDefault,
		},
		{
			desc:                       "from MAGLEV to empty",
			existingBSLocalityLbPolicy: LocalityLBPolicyMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyDefault,
			wantBSLocalityLbPolicy:     LocalityLBPolicyMaglev,
		},
		{
			desc:                       "from MAGLEV to MAGLEV",
			existingBSLocalityLbPolicy: LocalityLBPolicyMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyMaglev,
		},
		{
			desc:                       "from MAGLEV to WEIGHTED_MAGLEV",
			existingBSLocalityLbPolicy: LocalityLBPolicyMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyWeightedMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyWeightedMaglev,
		},
		{
			desc:                       "from WEIGHTED_MAGLEV to MAGLEV",
			existingBSLocalityLbPolicy: LocalityLBPolicyWeightedMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyMaglev,
			wantBSLocalityLbPolicy:     LocalityLBPolicyMaglev,
		},
		{
			desc:                       "from WEIGHTED_MAGLEV to empty",
			existingBSLocalityLbPolicy: LocalityLBPolicyWeightedMaglev,
			updatedBSLocalityLbPolicy:  LocalityLBPolicyDefault,
			wantBSLocalityLbPolicy:     LocalityLBPolicyMaglev,
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
