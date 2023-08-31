package firewalls

import (
	"context"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestEnsureL4FirewallRule(t *testing.T) {
	firewallDescription, err := utils.MakeL4LBFirewallDescription(utils.ServiceKeyFunc("test-ns", "test-name"), "10.0.0.1", meta.VersionGA, false)
	if err != nil {
		t.Errorf("Failed making the description, err=%v", err)
	}
	tests := []struct {
		desc   string
		nsName string
		params *FirewallParams
		shared bool
		want   *compute.Firewall
	}{
		{
			desc:   "default setup",
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
				PortRanges: []string{"8080"},
				NodeNames:  []string{"k8s-test-node"},
				Protocol:   "TCP",
				L4Type:     utils.ILB,
				Network:    network.NetworkInfo{IsDefault: true},
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
						IPProtocol: "tcp",
						Ports:      []string{"8080"},
					},
				},
			},
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
				PortRanges: []string{"8080"},
				NodeNames:  []string{"k8s-test-node"},
				Protocol:   "TCP",
				L4Type:     utils.ILB,
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
						IPProtocol: "tcp",
						Ports:      []string{"8080"},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			// Add some instance to act as the node so that target tags in the firewall can be resolved.
			createVMInstanceWithTag(t, fakeGCE, "k8s-test")
			if err := EnsureL4FirewallRule(fakeGCE, tc.nsName, tc.params, tc.shared); err != nil {
				t.Errorf("EnsureL4FirewallRule() failed, err=%v", err)
			}
			firewall, err := fakeGCE.GetFirewall(tc.params.Name)
			if err != nil {
				t.Errorf("failed to get firewall err=%v", err)
			}
			if diff := cmp.Diff(tc.want, firewall, cmpopts.IgnoreFields(compute.Firewall{}, "SelfLink")); diff != "" {
				t.Errorf("EnsureL4FirewallRule() diff -want +got\n%v\n", diff)
			}
		})
	}
}

func TestFirewallRuleEqual(t *testing.T) {
	tests := []struct {
		desc            string
		fw1             *compute.Firewall
		fw2             *compute.Firewall
		skipDescription bool
		expectEqual     bool
	}{
		{
			desc:        "empty rules",
			fw1:         &compute.Firewall{},
			fw2:         &compute.Firewall{},
			expectEqual: true,
		},
		{
			desc: "same firewall",
			fw1: &compute.Firewall{
				Name: "testFW",
				Allowed: []*compute.FirewallAllowed{{
					IPProtocol: "tcp",
					Ports:      []string{"80", "81"},
				}},
				DestinationRanges: []string{"dstrange1", "dstrange2"},
				SourceRanges:      []string{"srcrange1", "srcrange2"},
				TargetTags:        []string{"tag1", "tag2"},
				Network:           "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/default",
			},
			fw2: &compute.Firewall{
				Name: "testFW",
				Allowed: []*compute.FirewallAllowed{{
					IPProtocol: "tcp",
					Ports:      []string{"81", "80"},
				}},
				DestinationRanges: []string{"dstrange2", "dstrange1"},
				SourceRanges:      []string{"srcrange2", "srcrange1"},
				TargetTags:        []string{"tag2", "tag1"},
				Network:           "projects/test-project/global/networks/default",
			},
			expectEqual: true,
		},
		{
			desc: "different allowed ip proto",
			fw1: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{{
					IPProtocol: "tcp",
					Ports:      []string{"80"},
				}},
			},
			fw2: &compute.Firewall{
				Allowed: []*compute.FirewallAllowed{{
					IPProtocol: "udp",
					Ports:      []string{"80"},
				}},
			},
			expectEqual: false,
		},
		{
			desc: "different dst range",
			fw1: &compute.Firewall{
				DestinationRanges: []string{"dstrange1"},
			},
			fw2: &compute.Firewall{
				DestinationRanges: []string{"dstrange1", "dstrange2"},
			},
			expectEqual: false,
		},
		{
			desc: "different src range",
			fw1: &compute.Firewall{
				SourceRanges: []string{"srcrange1"},
			},
			fw2: &compute.Firewall{
				SourceRanges: []string{"srcrange2", "srcrange1"},
			},
			expectEqual: false,
		},
		{
			desc: "different target tags",
			fw1: &compute.Firewall{
				TargetTags: []string{"tag1"},
			},
			fw2: &compute.Firewall{
				TargetTags: []string{"tag1", "tag2"},
			},
			expectEqual: false,
		},
		{
			desc: "different network",
			fw1: &compute.Firewall{
				Network: "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/default",
			},
			fw2: &compute.Firewall{
				Network: "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/other",
			},
			expectEqual: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			result := firewallRuleEqual(tc.fw1, tc.fw2, tc.skipDescription)
			if result != tc.expectEqual {
				t.Errorf("firewallRuleEqual() unexpected result, want=%v, got=%v, fw1=%+v fw2=%+v", tc.expectEqual, result, tc.fw1, tc.fw2)
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
