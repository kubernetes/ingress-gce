package firewalls

import (
	"context"
	"testing"

	"k8s.io/klog/v2"

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
		desc         string
		nsName       string
		params       *FirewallParams
		shared       bool
		existingRule *compute.Firewall
		want         *compute.Firewall
		expectUpdate utils.ResourceSyncStatus
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
			},
			expectUpdate: utils.ResourceUpdate,
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
			},
			expectUpdate: utils.ResourceResync,
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
			},
			expectUpdate: utils.ResourceUpdate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
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
