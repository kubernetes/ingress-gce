package forwardingrules

import (
	"fmt"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestCreateForwardingRule(t *testing.T) {
	testCases := []struct {
		frRule *composite.ForwardingRule
		desc   string
	}{
		{
			frRule: &composite.ForwardingRule{
				Name:                "NetLB",
				LoadBalancingScheme: string(cloud.SchemeExternal),
			},
			desc: "Create external forwarding rule",
		},
		{
			frRule: &composite.ForwardingRule{
				Name:                "ILB",
				LoadBalancingScheme: string(cloud.SchemeInternal),
			},
			desc: "Create internal forwarding rule",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			frc := New(fakeGCE, meta.VersionGA, meta.Regional)

			err := frc.Create(tc.frRule)
			if err != nil {
				t.Fatalf("frc.Create(%v), returned error %v, want nil", tc.frRule, err)
			}

			err = verifyForwardingRuleExists(fakeGCE, tc.frRule.Name)
			if err != nil {
				t.Errorf("verifyForwardingRuleExists(_, %s) returned error %v, want nil", tc.frRule.Name, err)
			}
		})
	}
}

func TestGetForwardingRule(t *testing.T) {
	elbForwardingRule := &composite.ForwardingRule{
		Name:                "NetLB",
		Version:             meta.VersionGA,
		Scope:               meta.Regional,
		LoadBalancingScheme: string(cloud.SchemeExternal),
	}
	ilbForwardingRule := &composite.ForwardingRule{
		Name:                "ILB",
		Version:             meta.VersionGA,
		Scope:               meta.Regional,
		LoadBalancingScheme: string(cloud.SchemeInternal),
	}

	testCases := []struct {
		existingFwdRules []*composite.ForwardingRule
		getFwdRuleName   string
		expectedFwdRule  *composite.ForwardingRule
		desc             string
	}{
		{
			existingFwdRules: []*composite.ForwardingRule{elbForwardingRule, ilbForwardingRule},
			getFwdRuleName:   elbForwardingRule.Name,
			expectedFwdRule:  elbForwardingRule,
			desc:             "Get external forwarding rule",
		},
		{
			existingFwdRules: []*composite.ForwardingRule{elbForwardingRule, ilbForwardingRule},
			getFwdRuleName:   ilbForwardingRule.Name,
			expectedFwdRule:  ilbForwardingRule,
			desc:             "Get internal forwarding rule",
		},
		{
			existingFwdRules: []*composite.ForwardingRule{elbForwardingRule, ilbForwardingRule},
			getFwdRuleName:   "non-existent-rule",
			expectedFwdRule:  nil,
			desc:             "Get non existent forwarding rule",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			frc := New(fakeGCE, meta.VersionGA, meta.Regional)
			mustCreateForwardingRules(t, fakeGCE, tc.existingFwdRules)

			fr, err := frc.Get(tc.getFwdRuleName)
			if err != nil {
				t.Fatalf("frc.Get(%v), returned error %v, want nil", tc.getFwdRuleName, err)
			}

			ignoreFields := cmpopts.IgnoreFields(composite.ForwardingRule{}, "SelfLink", "Region")
			if !cmp.Equal(fr, tc.expectedFwdRule, ignoreFields) {
				diff := cmp.Diff(fr, tc.expectedFwdRule, ignoreFields)
				t.Errorf("frc.Get(s) returned %v, not equal to expectedFwdRule %v, diff: %v", fr, tc.expectedFwdRule, diff)
			}
		})
	}
}

func TestDeleteForwardingRule(t *testing.T) {
	elbForwardingRule := &composite.ForwardingRule{
		Name:                "NetLB",
		LoadBalancingScheme: string(cloud.SchemeExternal),
	}
	ilbForwardingRule := &composite.ForwardingRule{
		Name:                "ILB",
		LoadBalancingScheme: string(cloud.SchemeInternal),
	}

	testCases := []struct {
		existingFwdRules        []*composite.ForwardingRule
		deleteFwdRuleName       string
		shouldNotDeleteFwdRules []*composite.ForwardingRule
		desc                    string
	}{
		{
			existingFwdRules:        []*composite.ForwardingRule{elbForwardingRule, ilbForwardingRule},
			deleteFwdRuleName:       elbForwardingRule.Name,
			shouldNotDeleteFwdRules: []*composite.ForwardingRule{ilbForwardingRule},
			desc:                    "Delete elb forwarding rule",
		},
		{
			existingFwdRules:        []*composite.ForwardingRule{elbForwardingRule, ilbForwardingRule},
			deleteFwdRuleName:       ilbForwardingRule.Name,
			shouldNotDeleteFwdRules: []*composite.ForwardingRule{elbForwardingRule},
			desc:                    "Delete ilb forwarding rule",
		},
		{
			existingFwdRules:        []*composite.ForwardingRule{elbForwardingRule},
			deleteFwdRuleName:       elbForwardingRule.Name,
			shouldNotDeleteFwdRules: []*composite.ForwardingRule{},
			desc:                    "Delete single elb forwarding rule",
		},
		{
			existingFwdRules:        []*composite.ForwardingRule{elbForwardingRule, ilbForwardingRule},
			deleteFwdRuleName:       "non-existent",
			shouldNotDeleteFwdRules: []*composite.ForwardingRule{elbForwardingRule, ilbForwardingRule},
			desc:                    "Delete non existent forwarding rule",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			frc := New(fakeGCE, meta.VersionGA, meta.Regional)
			mustCreateForwardingRules(t, fakeGCE, tc.existingFwdRules)

			err := frc.Delete(tc.deleteFwdRuleName)
			if err != nil {
				t.Fatalf("frc.Delete(%v), returned error %v, want nil", tc.deleteFwdRuleName, err)
			}

			err = verifyForwardingRuleNotExists(fakeGCE, tc.deleteFwdRuleName)
			if err != nil {
				t.Errorf("verifyForwardingRuleNotExists(_, %s) returned error %v, want nil", tc.deleteFwdRuleName, err)
			}
			for _, fw := range tc.shouldNotDeleteFwdRules {
				err = verifyForwardingRuleExists(fakeGCE, fw.Name)
				if err != nil {
					t.Errorf("verifyForwardingRuleExists(_, %s) returned error %v, want nil", fw.Name, err)
				}
			}
		})
	}
}

func verifyForwardingRuleExists(cloud *gce.Cloud, name string) error {
	key, err := composite.CreateKey(cloud, name, meta.Regional)
	if err != nil {
		return fmt.Errorf("failed to create key for fetching forwarding rule %s, err: %w", name, err)
	}

	_, err = composite.GetForwardingRule(cloud, key, meta.VersionGA)
	if err != nil {
		if utils.IsNotFoundError(err) {
			return fmt.Errorf("forwarding rule %s was not found, expected to exist", name)
		}
		return fmt.Errorf("composite.GetForwardingRule(_, %v, %v) returned error %w, want nil", key, meta.VersionGA, err)
	}
	return nil
}

func verifyForwardingRuleNotExists(cloud *gce.Cloud, name string) error {
	key, err := composite.CreateKey(cloud, name, meta.Regional)
	if err != nil {
		return fmt.Errorf("failed to create key for fetching forwarding rule %s, err: %w", name, err)
	}

	_, err = composite.GetForwardingRule(cloud, key, meta.VersionGA)
	if err != nil {
		if utils.IsNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("composite.GetForwardingRule(_, %v, %v) returned error %w, want nil", key, meta.VersionGA, err)
	}
	return fmt.Errorf("forwarding rule %s exists, expected to be not found", name)
}

func mustCreateForwardingRules(t *testing.T, cloud *gce.Cloud, frs []*composite.ForwardingRule) {
	t.Helper()
	for _, fr := range frs {
		mustCreateForwardingRule(t, cloud, fr)
	}
}

func mustCreateForwardingRule(t *testing.T, cloud *gce.Cloud, fr *composite.ForwardingRule) {
	t.Helper()

	key := meta.RegionalKey(fr.Name, cloud.Region())
	err := composite.CreateForwardingRule(cloud, key, fr)
	if err != nil {
		t.Fatalf("composite.CreateForwardingRule(_, %s, %v) returned error %v, want nil", key, fr, err)
	}
}
