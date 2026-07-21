package firewalls

import (
	"context"
	"reflect"
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
			desc: "pinhole rule with destination ranges",
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
				"--project test-project",
			},
		},
		{
			desc: "standard rule without destination ranges",
			fw: &compute.Firewall{
				Name:         "k8s-fw-std",
				Network:      "projects/test-project/global/networks/default",
				Description:  "std rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80"}},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-std",
				"--network default",
				`--description "std rule"`,
				"--allow tcp:80",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "ipv6 pinhole rule",
			fw: &compute.Firewall{
				Name:              "k8s-fw-ipv6-pinhole",
				Network:           "projects/test-project/global/networks/default",
				Description:       "ipv6 pinhole rule",
				SourceRanges:      []string{"2001:db8::/32"},
				DestinationRanges: []string{"2001:db8::1/128"},
				TargetTags:        []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80"}},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-ipv6-pinhole",
				"--network default",
				`--description "ipv6 pinhole rule"`,
				"--allow tcp:80",
				"--source-ranges 2001:db8::/32",
				"--destination-ranges 2001:db8::1/128",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "deny rule with priority, direction, and disabled",
			fw: &compute.Firewall{
				Name:         "k8s-fw-deny",
				Network:      "projects/test-project/global/networks/default",
				Description:  "deny rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Denied: []*compute.FirewallDenied{
					{IPProtocol: "tcp", Ports: []string{"8080"}},
				},
				Priority:  500,
				Direction: "INGRESS",
				Disabled:  true,
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-deny",
				"--network default",
				`--description "deny rule"`,
				"--deny tcp:8080",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--priority 500",
				"--direction INGRESS",
				"--disabled",
				"--project test-project",
			},
		},
		{
			desc: "rule with multiple protocols and multiple ports",
			fw: &compute.Firewall{
				Name:         "k8s-fw-multi-port",
				Network:      "projects/test-project/global/networks/default",
				Description:  "multi port rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80", "443"}},
					{IPProtocol: "udp", Ports: []string{"53"}},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-multi-port",
				"--network default",
				`--description "multi port rule"`,
				"--allow tcp:443,tcp:80,udp:53",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "deny all rule",
			fw: &compute.Firewall{
				Name:         "k8s-fw-deny-all",
				Network:      "projects/test-project/global/networks/default",
				Description:  "deny all rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Denied: []*compute.FirewallDenied{
					{IPProtocol: "all"},
				},
				Priority:  500,
				Direction: "INGRESS",
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-deny-all",
				"--network default",
				`--description "deny all rule"`,
				"--deny all",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--priority 500",
				"--direction INGRESS",
				"--project test-project",
			},
		},
		{
			desc: "rule allowing ICMP without ports",
			fw: &compute.Firewall{
				Name:         "k8s-fw-icmp",
				Network:      "projects/test-project/global/networks/default",
				Description:  "icmp rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "icmp"},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-icmp",
				"--network default",
				`--description "icmp rule"`,
				"--allow icmp",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "complex multi-protocol rule with tcp, udp, icmp, esp, and ah",
			fw: &compute.Firewall{
				Name:         "k8s-fw-complex",
				Network:      "projects/test-project/global/networks/default",
				Description:  "complex multi proto rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80", "443"}},
					{IPProtocol: "udp", Ports: []string{"53"}},
					{IPProtocol: "icmp"},
					{IPProtocol: "esp"},
					{IPProtocol: "ah"},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules create k8s-fw-complex",
				"--network default",
				`--description "complex multi proto rule"`,
				"--allow ah,esp,icmp,tcp:443,tcp:80,udp:53",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
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
			desc: "pinhole rule with destination ranges",
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
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-pinhole",
				`--description "pinhole rule"`,
				"--allow tcp:80",
				"--source-ranges 10.0.0.0/8",
				"--destination-ranges 192.168.1.1/32,192.168.1.2/32",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "standard rule without destination ranges",
			fw: &compute.Firewall{
				Name:         "k8s-fw-std",
				Network:      "projects/test-project/global/networks/default",
				Description:  "std rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80"}},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-std",
				`--description "std rule"`,
				"--allow tcp:80",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "ipv6 pinhole rule",
			fw: &compute.Firewall{
				Name:              "k8s-fw-ipv6-pinhole",
				Network:           "projects/test-project/global/networks/default",
				Description:       "ipv6 pinhole rule",
				SourceRanges:      []string{"2001:db8::/32"},
				DestinationRanges: []string{"2001:db8::1/128"},
				TargetTags:        []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80"}},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-ipv6-pinhole",
				`--description "ipv6 pinhole rule"`,
				"--allow tcp:80",
				"--source-ranges 2001:db8::/32",
				"--destination-ranges 2001:db8::1/128",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "deny rule with priority, direction, and disabled",
			fw: &compute.Firewall{
				Name:         "k8s-fw-deny",
				Network:      "projects/test-project/global/networks/default",
				Description:  "deny rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Denied: []*compute.FirewallDenied{
					{IPProtocol: "tcp", Ports: []string{"8080"}},
				},
				Priority:  500,
				Direction: "INGRESS",
				Disabled:  true,
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-deny",
				`--description "deny rule"`,
				"--deny tcp:8080",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--priority 500",
				"--direction INGRESS",
				"--disabled",
				"--project test-project",
			},
		},
		{
			desc: "rule with multiple protocols and multiple ports",
			fw: &compute.Firewall{
				Name:         "k8s-fw-multi-port",
				Network:      "projects/test-project/global/networks/default",
				Description:  "multi port rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80", "443"}},
					{IPProtocol: "udp", Ports: []string{"53"}},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-multi-port",
				`--description "multi port rule"`,
				"--allow tcp:443,tcp:80,udp:53",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "deny all rule without ports",
			fw: &compute.Firewall{
				Name:         "k8s-fw-deny-all",
				Network:      "projects/test-project/global/networks/default",
				Description:  "deny all rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Denied: []*compute.FirewallDenied{
					{IPProtocol: "all"},
				},
				Priority:  500,
				Direction: "INGRESS",
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-deny-all",
				`--description "deny all rule"`,
				"--deny all",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--priority 500",
				"--direction INGRESS",
				"--project test-project",
			},
		},
		{
			desc: "rule allowing ICMP without ports",
			fw: &compute.Firewall{
				Name:         "k8s-fw-icmp",
				Network:      "projects/test-project/global/networks/default",
				Description:  "icmp rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "icmp"},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-icmp",
				`--description "icmp rule"`,
				"--allow icmp",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
				"--project test-project",
			},
		},
		{
			desc: "complex multi-protocol rule with tcp, udp, icmp, esp, and ah",
			fw: &compute.Firewall{
				Name:         "k8s-fw-complex",
				Network:      "projects/test-project/global/networks/default",
				Description:  "complex multi proto rule",
				SourceRanges: []string{"10.0.0.0/8"},
				TargetTags:   []string{"node-tag"},
				Allowed: []*compute.FirewallAllowed{
					{IPProtocol: "tcp", Ports: []string{"80", "443"}},
					{IPProtocol: "udp", Ports: []string{"53"}},
					{IPProtocol: "icmp"},
					{IPProtocol: "esp"},
					{IPProtocol: "ah"},
				},
			},
			projectID: "test-project",
			wantArgs: []string{
				"gcloud compute firewall-rules update k8s-fw-complex",
				`--description "complex multi proto rule"`,
				"--allow ah,esp,icmp,tcp:443,tcp:80,udp:53",
				"--source-ranges 10.0.0.0/8",
				"--target-tags node-tag",
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

func TestFirewallToGCloud_FieldExhaustiveness(t *testing.T) {
	// knownFields maps every field in compute.Firewall to a boolean:
	// true  = handled in FirewallToGCloudCreateCmd / FirewallToGCloudUpdateCmd
	// false = intentionally unmapped (e.g. read-only server metadata,
	//         internal SDK fields, or unused GCP features)
	knownFields := map[string]bool{
		// Handled fields in gcloud command generation
		"Name":              true,
		"Network":           true,
		"Description":       true,
		"Allowed":           true,
		"Denied":            true,
		"SourceRanges":      true,
		"DestinationRanges": true,
		"TargetTags":        true,
		"Priority":          true,
		"Direction":         true,
		"Disabled":          true,

		// Read-only server metadata (not CLI flags)
		"CreationTimestamp": false,
		"Id":                false,
		"Kind":              false,
		"SelfLink":          false,

		// Unused GCP features in ingress-gce
		"LogConfig":             false,
		"Params":                false,
		"SourceServiceAccounts": false,
		"SourceTags":            false,
		"TargetServiceAccounts": false,

		// Internal Google API Go SDK helpers used for HTTP status tracking
		// and JSON marshaling (ServerResponse stores HTTP headers/status,
		// ForceSendFields/NullFields control JSON serialization).
		"ServerResponse":  false,
		"ForceSendFields": false,
		"NullFields":      false,
	}

	for field := range reflect.TypeFor[compute.Firewall]().Fields() {
		if _, evaluated := knownFields[field.Name]; !evaluated {
			t.Fatalf(
				"New field %q added to compute.Firewall!\n"+
					"Evaluate whether it needs gcloud flag mapping in\n"+
					"FirewallToGCloudCreateCmd / FirewallToGCloudUpdateCmd,\n"+
					"then update knownFields in this test.",
				field.Name,
			)
		}
	}
}
