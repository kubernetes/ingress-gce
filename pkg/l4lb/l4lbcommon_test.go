package l4lb

import (
	context1 "context"
	"reflect"
	"testing"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/cloud-provider-gcp/providers/gce"
	l4lbconfigv1 "k8s.io/ingress-gce/pkg/apis/l4lbconfig/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/l4annotations"
	l4lbconfigclient "k8s.io/ingress-gce/pkg/l4lbconfig/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/loadbalancers"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils/common"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"
)

const (
	defaultTestSubnetURL = "https://www.googleapis.com/compute/v1/projects/mock-project/regions/test-region/subnetworks/default"
	fakeZone             = "zone-a"
)

func TestFinalizerWasRemovedUnexpectedly(t *testing.T) {
	testCases := []struct {
		desc           string
		oldService     *v1.Service
		newService     *v1.Service
		finalizerName  string
		expectedResult bool
	}{
		{
			desc:           "Clean service",
			oldService:     &v1.Service{},
			newService:     &v1.Service{},
			finalizerName:  "random",
			expectedResult: false,
		},
		{
			desc: "Empty finalizers",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{},
				},
			},
			finalizerName:  "random",
			expectedResult: false,
		},
		{
			desc: "Changed L4 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.LegacyILBFinalizer, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", "gke.networking.io/l4-ilb-v1-fake"},
				},
			},
			finalizerName:  common.LegacyILBFinalizer,
			expectedResult: true,
		},
		{
			desc: "Removed L4 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.LegacyILBFinalizer, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			finalizerName:  common.LegacyILBFinalizer,
			expectedResult: true,
		},
		{
			desc: "Added L4 ILB v2 Finalizer",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", common.ILBFinalizerV2},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Service with NetLB Finalizer hasn't changed",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random", common.NetLBFinalizerV2},
				},
			},
			finalizerName:  common.NetLBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Finalizer was removed but given name is wrong",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{"random"},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
		{
			desc: "Finalizer was removed and service to be deleted",
			oldService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{common.NetLBFinalizerV2, "random"},
				},
			},
			newService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &metav1.Time{Time: time.Date(2024, 12, 30, 0, 0, 0, 0, time.Local)},
					Finalizers:        []string{common.ILBFinalizerV2},
				},
			},
			finalizerName:  common.ILBFinalizerV2,
			expectedResult: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			gotResult := finalizerWasRemovedUnexpectedly(tc.oldService, tc.newService, tc.finalizerName)
			if gotResult != tc.expectedResult {
				t.Errorf("finalizerWasRemoved(oldSvc=%v, newSvc=%v, finalizer=%s) returned %v, but expected %v", tc.oldService, tc.newService, tc.finalizerName, gotResult, tc.expectedResult)
			}
		})
	}
}

func TestUpdateL4LBConfig_Extended(t *testing.T) {
	const (
		namespace  = "test-ns"
		configName = "test-config"
	)

	testCases := []struct {
		desc                   string
		svc                    *v1.Service
		bsLogConfig            *composite.BackendServiceLogConfig
		initialConfig          *l4lbconfigv1.L4LBConfig
		expectedUpdate         bool
		expectedAnnotated      string
		expectedSampleRate     int32
		expectedEnable         bool
		expectedOptionalMode   string
		expectedOptionalFields []string
	}{
		{
			desc: "Annotation added if missing - uses service name as default",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "my-loadbalancer-svc",
				},
			},
			bsLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 1.0,
			},
			initialConfig:      nil,
			expectedUpdate:     true,
			expectedAnnotated:  "my-loadbalancer-svc",
			expectedEnable:     true,
			expectedSampleRate: 1000000,
		},
		{
			desc: "Logging disabled in GCE - ignore other fields in comparison",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      "svc-1",
					Annotations: map[string]string{
						l4annotations.L4LBConfigKey: configName,
					},
				},
			},
			bsLogConfig: &composite.BackendServiceLogConfig{
				Enable:     false,
				SampleRate: 0.1,
			},
			initialConfig: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: configName},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{
						Enabled:    false,
						SampleRate: func(i int32) *int32 { return &i }(500000),
					},
				},
			},
			// Since both are "Disabled", the config is considered equal. No update.
			expectedUpdate: false,
		},
		{
			desc: "Update existing config - transition from enabled to disabled",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "svc-1",
					Annotations: map[string]string{l4annotations.L4LBConfigKey: configName},
				},
			},
			bsLogConfig: &composite.BackendServiceLogConfig{
				Enable: false,
			},
			initialConfig: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: configName},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{Enabled: true, SampleRate: func(i int32) *int32 { return &i }(100)},
				},
			},
			expectedUpdate: true,
			expectedEnable: false,
		},
		{
			desc: "Explicitly overwrite existing CRD fields with GCE empty state",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "svc-mirror",
					Annotations: map[string]string{l4annotations.L4LBConfigKey: configName},
				},
			},
			bsLogConfig: &composite.BackendServiceLogConfig{
				Enable:         true,
				SampleRate:     0.5,
				OptionalMode:   "EXCLUDE_ALL_OPTIONAL",
				OptionalFields: nil,
			},
			initialConfig: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: configName},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{
						Enabled:        true,
						SampleRate:     func(i int32) *int32 { return &i }(100),
						OptionalMode:   "CUSTOM",
						OptionalFields: []string{"serverInstance"}, // This should be wiped
					},
				},
			},
			expectedUpdate:         true,
			expectedEnable:         true,
			expectedSampleRate:     500000,
			expectedOptionalMode:   "EXCLUDE_ALL_OPTIONAL",
			expectedOptionalFields: nil,
		},
		{
			desc: "GCE state is Enabled with 0 percent rate",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "svc-zero",
					Annotations: map[string]string{l4annotations.L4LBConfigKey: configName},
				},
			},
			bsLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 0.0,
			},
			initialConfig: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: configName},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{Enabled: false},
				},
			},
			expectedUpdate:     true,
			expectedEnable:     true,
			expectedSampleRate: 0,
		},
		{
			desc: "No-op when GCE state matches L4LBConfig exactly",
			svc: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Name:        "svc-idempotent",
					Annotations: map[string]string{l4annotations.L4LBConfigKey: configName},
				},
			},
			bsLogConfig: &composite.BackendServiceLogConfig{
				Enable:     true,
				SampleRate: 0.75,
			},
			initialConfig: &l4lbconfigv1.L4LBConfig{
				ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: configName},
				Spec: l4lbconfigv1.L4LBConfigSpec{
					Logging: &l4lbconfigv1.LoggingConfig{
						Enabled:    true,
						SampleRate: func(i int32) *int32 { return &i }(750000),
					},
				},
			},
			expectedUpdate: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {

			fakeL4LBClient := l4lbconfigclient.NewSimpleClientset()
			kubeClient := fake.NewSimpleClientset()

			vals := gce.DefaultTestClusterValues()
			vals.SubnetworkURL = defaultTestSubnetURL
			fakeGCE := gce.NewFakeGCECloud(vals)
			nodeInformer := zonegetter.FakeNodeInformer()
			fakeZoneGetter, err := zonegetter.NewFakeZoneGetter(nodeInformer, zonegetter.FakeNodeTopologyInformer(), defaultTestSubnetURL, false)
			if err != nil {
				t.Fatal("failed to initialize fake zone getter")
			}
			zonegetter.AddFakeNodes(fakeZoneGetter, fakeZone, "test-node")

			(fakeGCE.Compute().(*cloud.MockGCE)).MockGlobalForwardingRules.InsertHook = loadbalancers.InsertGlobalForwardingRuleHook
			namer := namer_util.NewNamer(clusterUID, "", klog.TODO())

			_, _ = kubeClient.CoreV1().Services(tc.svc.Namespace).Create(context1.Background(), tc.svc, metav1.CreateOptions{})

			if tc.initialConfig != nil {
				_, _ = fakeL4LBClient.NetworkingV1().L4LBConfigs(namespace).Create(context1.Background(), tc.initialConfig, metav1.CreateOptions{})
			}

			ctxConfig := context.ControllerContextConfig{
				Namespace:                     api_v1.NamespaceAll,
				ResyncPeriod:                  1 * time.Minute,
				DefaultBackendSvcPort:         test.DefaultBeSvcPort,
				HealthCheckPath:               "/",
				EnableIngressRegionalExternal: true,
			}

			ctx, err := context.NewControllerContext(kubeClient, nil, nil, nil, nil, nil, nil, nil, fakeL4LBClient, kubeClient /*kube client to be used for events*/, fakeGCE, namer, "" /*kubeSystemUID*/, ctxConfig, klog.TODO())
			if err != nil {
				t.Fatalf("Failed to create ControllerContext: %v", err)
			}

			err = updateL4LBConfig(ctx, tc.svc, tc.bsLogConfig, klog.TODO())
			if err != nil {
				t.Fatalf("updateL4LBConfig failed: %v", err)
			}

			if tc.expectedAnnotated != "" {
				updatedSvc, _ := kubeClient.CoreV1().Services(tc.svc.Namespace).Get(context1.Background(), tc.svc.Name, metav1.GetOptions{})
				val := updatedSvc.Annotations[l4annotations.L4LBConfigKey]
				if val != tc.expectedAnnotated {
					t.Errorf("Service annotation mismatch: got %q, want %q", val, tc.expectedAnnotated)
				}
			}

			targetConfigName := configName
			if tc.expectedAnnotated != "" {
				targetConfigName = tc.expectedAnnotated
			}

			updatedConfig, err := fakeL4LBClient.NetworkingV1().L4LBConfigs(tc.svc.Namespace).Get(context1.Background(), targetConfigName, metav1.GetOptions{})
			if tc.expectedUpdate {
				if err != nil {
					t.Fatalf("Expected L4LBConfig %s to exist, but got err: %v", targetConfigName, err)
				}

				if updatedConfig.Spec.Logging.Enabled != tc.expectedEnable {
					t.Errorf("Logging Enabled mismatch: got %v, want %v", updatedConfig.Spec.Logging.Enabled, tc.expectedEnable)
				}
				if tc.expectedSampleRate != 0 && *updatedConfig.Spec.Logging.SampleRate != tc.expectedSampleRate {
					t.Errorf("SampleRate mismatch: got %v, want %v", *updatedConfig.Spec.Logging.SampleRate, tc.expectedSampleRate)
				}
				if tc.expectedOptionalMode != "" && updatedConfig.Spec.Logging.OptionalMode != tc.expectedOptionalMode {
					t.Errorf("OptionalMode was corrupted: got %q, want %q", updatedConfig.Spec.Logging.OptionalMode, tc.expectedOptionalMode)
				}
				if len(tc.expectedOptionalFields) > 0 {
					if !reflect.DeepEqual(updatedConfig.Spec.Logging.OptionalFields, tc.expectedOptionalFields) {
						t.Errorf("OptionalFields were corrupted or cleared: got %v, want %v", updatedConfig.Spec.Logging.OptionalFields, tc.expectedOptionalFields)
					}
				}

			} else if tc.initialConfig != nil {
				// If no update was expected, ensure the config remains exactly as it was
				if !reflect.DeepEqual(updatedConfig.Spec, tc.initialConfig.Spec) {
					t.Errorf("Config Spec was updated unexpectedly. Diff found between initial and current state.")
				}
			}
		})
	}
}
