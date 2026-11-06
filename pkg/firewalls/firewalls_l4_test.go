package firewalls

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	l4utils "k8s.io/ingress-gce/pkg/l4/utils"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEnsureL4FirewallRule(t *testing.T) {
	firewallDescription, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc("test-ns", "test-name"), "10.0.0.1", meta.VersionGA, false)
	if err != nil {
		t.Errorf("Failed making the description, err=%v", err)
	}
	tests := []struct {
		desc         string
		nsName       string
		params       *FirewallParams
		shared       bool
		existingRule *compute.Firewall
		want         *compute.Firewall
		expectUpdate l4utils.ResourceSyncStatus
	}{
		{
			desc:   "default ensure",
			nsName: utils.ServiceKeyFunc("test-ns", "test-name"),
			params: &FirewallParams{
				Name: "test-firewall",
				IP:   "10.0.0.1",
				SourceRanges: []string{
					"10.1.2.8/29",
				},
				DestinationRanges: []string{
					"10.1.2.16/29",
				},
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"8080"},
					},
				},
				NodeNames: []string{"k8s-test-node"},
				L4Type:    utils.ILB,
				Network:   network.NetworkInfo{IsDefault: true},
			},
			shared: false,
			want: &compute.Firewall{
				Name:    "test-firewall",
				Network: "",
				SourceRanges: []string{
					"10.1.2.8/29",
				},
				TargetTags:  []string{"k8s-test"},
				Description: firewallDescription,
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"8080"},
					},
				},
				Priority: 1000,
			},
			expectUpdate: l4utils.ResourceUpdate,
		},
		{
			desc:   "default setup no udpate",
			nsName: utils.ServiceKeyFunc("test-ns", "test-name"),
			params: &FirewallParams{
				Name: "test-firewall",
				IP:   "10.0.0.1",
				SourceRanges: []string{
					"10.1.2.8/29",
				},
				DestinationRanges: []string{
					"10.1.2.16/29",
				},
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"8080"},
					},
				},
				NodeNames: []string{"k8s-test-node"},
				L4Type:    utils.ILB,
				Network:   network.NetworkInfo{IsDefault: true},
			},
			shared: false,
			existingRule: &compute.Firewall{
				Name:    "test-firewall",
				Network: "",
				SourceRanges: []string{
					"10.1.2.8/29",
				},
				TargetTags:  []string{"k8s-test"},
				Description: firewallDescription,
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"8080"},
					},
				},
				Priority: 1000,
			},
			want: &compute.Firewall{
				Name:    "test-firewall",
				Network: "",
				SourceRanges: []string{
					"10.1.2.8/29",
				},
				TargetTags:  []string{"k8s-test"},
				Description: firewallDescription,
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"8080"},
					},
				},
				Priority: 1000,
			},
			expectUpdate: l4utils.ResourceResync,
		},
		{
			desc:   "non default network",
			nsName: utils.ServiceKeyFunc("test-ns", "test-name"),
			params: &FirewallParams{
				Name: "test-firewall",
				IP:   "10.0.0.1",
				SourceRanges: []string{
					"10.1.2.8/29",
				},
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"8080"},
					},
				},
				NodeNames: []string{"k8s-test-node"},
				L4Type:    utils.ILB,
				Network: network.NetworkInfo{
					IsDefault:  false,
					NetworkURL: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				},
			},
			shared: false,
			want: &compute.Firewall{
				Name:    "test-firewall",
				Network: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc",
				SourceRanges: []string{
					"10.1.2.8/29",
				},
				TargetTags:  []string{"k8s-test"},
				Description: firewallDescription,
				Allowed: []*compute.FirewallAllowed{
					{
						IPProtocol: "TCP",
						Ports:      []string{"8080"},
					},
				},
				Priority: 1000,
			},
			expectUpdate: l4utils.ResourceUpdate,
		},
		{
			desc:   "non default priority",
			nsName: utils.ServiceKeyFunc("test-ns", "test-name"),
			params: &FirewallParams{
				Name:     "test-firewall",
				Priority: ptr.To(1234),
				IP:       "10.0.0.1",
			},
			existingRule: &compute.Firewall{
				Name:        "test-firewall",
				Description: firewallDescription,
				Priority:    1000,
			},
			want: &compute.Firewall{
				Name:        "test-firewall",
				Description: firewallDescription,
				Priority:    1234,
			},
			expectUpdate: l4utils.ResourceUpdate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			mockGCE.MockFirewalls.PatchHook = mock.UpdateFirewallHook
			// Add some instance to act as the node so that target tags in the firewall can be resolved.
			createVMInstanceWithTag(t, fakeGCE, "k8s-test")
			if tc.existingRule != nil {
				fakeGCE.CreateFirewall(tc.existingRule)
			}
			updateDone, err := EnsureL4FirewallRule(fakeGCE, tc.nsName, tc.params, tc.shared, klog.TODO())
			if err != nil {
				t.Errorf("EnsureL4FirewallRule() failed, err=%v", err)
			}
			firewall, err := fakeGCE.GetFirewall(tc.params.Name)
			if err != nil {
				t.Errorf("failed to get firewall err=%v", err)
			}
			if diff := cmp.Diff(tc.want, firewall, cmpopts.IgnoreFields(compute.Firewall{}, "SelfLink")); diff != "" {
				t.Errorf("EnsureL4FirewallRule() diff -want +got\n%v\n", diff)
			}
			if updateDone != tc.expectUpdate {
				t.Errorf("EnsureL4FirewallRule() expectedUpdate: %v, but the update was: %v", tc.expectUpdate, updateDone)
			}
		})
	}
}

func TestEnsureL4LBFirewallForNodesPortsLimitExceededEvent(t *testing.T) {
	const fwName = "test-firewall"
	portsLimitErr := &googleapi.Error{
		Code:    http.StatusRequestEntityTooLarge,
		Message: "Value for field 'resource.allowed[0].ports' is too large: maximum size 100 element(s); actual size 120.",
	}
	otherErr := &googleapi.Error{
		Code:    http.StatusInternalServerError,
		Message: "internal error",
	}

	testCases := []struct {
		desc         string
		existingRule *compute.Firewall
		insertErr    error
		patchErr     error
		wantErr      error
		wantEvent    bool
	}{
		{
			desc:      "create fails with allowed ports limit exceeded",
			insertErr: portsLimitErr,
			wantErr:   portsLimitErr,
			wantEvent: true,
		},
		{
			desc: "patch fails with allowed ports limit exceeded",
			existingRule: &compute.Firewall{
				Name:    fwName,
				Allowed: []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{"80"}}},
			},
			patchErr:  portsLimitErr,
			wantErr:   portsLimitErr,
			wantEvent: true,
		},
		{
			desc:      "create fails with other error",
			insertErr: otherErr,
			wantErr:   otherErr,
			wantEvent: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			createVMInstanceWithTag(t, fakeGCE, "k8s-test")
			if tc.existingRule != nil {
				if err := fakeGCE.CreateFirewall(tc.existingRule); err != nil {
					t.Fatalf("fakeGCE.CreateFirewall() failed, err=%v", err)
				}
			}
			if tc.insertErr != nil {
				mockGCE.MockFirewalls.InsertError = map[meta.Key]error{*meta.GlobalKey(fwName): tc.insertErr}
			}
			if tc.patchErr != nil {
				mockGCE.MockFirewalls.PatchHook = func(context.Context, *meta.Key, *compute.Firewall, *cloud.MockFirewalls, ...cloud.Option) error {
					return tc.patchErr
				}
			}

			svc := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-ns"}}
			params := &FirewallParams{
				Name:         fwName,
				SourceRanges: []string{"10.1.2.8/29"},
				Allowed:      []*compute.FirewallAllowed{{IPProtocol: "TCP", Ports: []string{"8080", "8082"}}},
				NodeNames:    []string{"k8s-test-node"},
				L4Type:       utils.ILB,
				Network:      network.NetworkInfo{IsDefault: true},
			}
			recorder := record.NewFakeRecorder(10)

			_, err := EnsureL4LBFirewallForNodes(svc, params, fakeGCE, recorder, klog.TODO())
			if err != tc.wantErr {
				t.Errorf("EnsureL4LBFirewallForNodes() err = %v, want %v", err, tc.wantErr)
			}

			gotEvent := false
			for len(recorder.Events) > 0 {
				if strings.HasPrefix(<-recorder.Events, "Warning FirewallPortsLimitExceeded") {
					gotEvent = true
				}
			}
			if gotEvent != tc.wantEvent {
				t.Errorf("FirewallPortsLimitExceeded event emitted = %v, want %v", gotEvent, tc.wantEvent)
			}
		})
	}
}

func createVMInstanceWithTag(t *testing.T, fakeGCE *gce.Cloud, tag string) {
	err := fakeGCE.Compute().Instances().Insert(context.Background(),
		meta.ZonalKey("k8s-test-node", "us-central1-b"),
		&compute.Instance{
			Name: "test-node",
			Zone: "us-central1-b",
			Tags: &compute.Tags{
				Items: []string{tag},
			},
		})
	if err != nil {
		t.Errorf("failed to create instance err=%v", err)
	}
}

func TestFirewallToGCloudCreateCmd(t *testing.T) {
	testCases := []struct {
		desc      string
		fw        *compute.Firewall
		projectID string
		wantArgs  []string
	}{
		{
			desc: "pinhole rule with destination ranges, priority, direction, and disabled",
			fw: &compute.Firewall{
				Name:              "k8s-fw-pinhole",
				Network:           "projects/test-project/global/networks/default",
				Description:       "pinhole rule",
				SourceRanges:      []string{"10.0.0.0/8"},
				DestinationRanges: []string{"192.168.1.2/32", "192.168.1.1/32"},
				TargetTags:        []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80"}},
				},
				Priority:  500,
				Direction: "INGRESS",
				Disabled:  true,
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-pinhole",
				"--network default",
				`--description "pinhole rule"`,
				"--allow tcp:80",
				"--source-ranges 10.0.0.0/8",
				"--destination-ranges 192.168.1.1/32,192.168.1.2/32",
				"--target-tags node-tag",
				"--priority 500",
				"--direction INGRESS",
				"--disabled",
				"--project test-project",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := gce.FirewallToGCloudCreateCmd(tc.fw, tc.projectID)
			want := strings.Join(tc.wantArgs, " ")
			if got != want {
				t.Errorf("%s failed:\nGot:  %q\nWant: %q", tc.desc, got, want)
			}
		})
	}
}

func TestFirewallToGCloudUpdateCmd(t *testing.T) {
	testCases := []struct {
		desc      string
		fw        *compute.Firewall
		projectID string
		wantArgs  []string
	}{
		{
			desc: "pinhole rule with destination ranges, priority, direction, and disabled",
			fw: &compute.Firewall{
				Name:              "k8s-fw-pinhole",
				Network:           "projects/test-project/global/networks/default",
				Description:       "pinhole rule",
				SourceRanges:      []string{"10.0.0.0/8"},
				DestinationRanges: []string{"192.168.1.2/32", "192.168.1.1/32"},
				TargetTags:        []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80"}},
				},
				Priority:  500,
				Direction: "INGRESS",
				Disabled:  true,
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-pinhole",
				`--description "pinhole rule"`,
				"--allow tcp:80",
				"--source-ranges 10.0.0.0/8",
				"--destination-ranges 192.168.1.1/32,192.168.1.2/32",
				"--target-tags node-tag",
				"--priority 500",
				"--direction INGRESS",
				"--disabled",
				"--project test-project",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			got := gce.FirewallToGCloudUpdateCmd(tc.fw, tc.projectID)
			want := strings.Join(tc.wantArgs, " ")
			if got != want {
				t.Errorf("%s failed:\nGot:  %q\nWant: %q", tc.desc, got, want)
			}
		})
	}
}
