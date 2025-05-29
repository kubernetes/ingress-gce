package optimized_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/forwardingrules/optimized"
	"k8s.io/klog/v2"
)

// TestApply verifies that Apply function correctly applies API operations to forwarding rules.
// This creates a fake cloud, populates it with initial state, calls Apply and verifies the end state.
func TestApply(t *testing.T) {
	testCases := []struct {
		desc    string
		haveFRs []*composite.ForwardingRule
		ops     *optimized.APIOperations

		wantFRs []*composite.ForwardingRule
	}{
		{
			desc: "nothing changed",
			haveFRs: []*composite.ForwardingRule{
				{Name: "fr-1"},
				{Name: "dont-touch-me"},
			},
			ops: &optimized.APIOperations{},

			wantFRs: []*composite.ForwardingRule{
				{Name: "fr-1"},
				{Name: "dont-touch-me"},
			},
		},
		{
			desc: "simple create",
			haveFRs: []*composite.ForwardingRule{
				{Name: "dont-touch-me"},
			},
			ops: &optimized.APIOperations{
				Create: []*composite.ForwardingRule{
					{Name: "fr-1"},
				},
			},

			wantFRs: []*composite.ForwardingRule{
				{Name: "dont-touch-me"},
				{Name: "fr-1"},
			},
		},
		{
			desc: "simple delete",
			haveFRs: []*composite.ForwardingRule{
				{Name: "fr-1"},
				{Name: "dont-touch-me"},
			},
			ops: &optimized.APIOperations{
				Delete: []string{"fr-1"},
			},

			wantFRs: []*composite.ForwardingRule{
				{Name: "dont-touch-me"},
			},
		},
		{
			desc: "simple patch",
			haveFRs: []*composite.ForwardingRule{
				{Name: "fr-1"},
				{Name: "dont-touch-me"},
			},
			ops: &optimized.APIOperations{
				Update: []*composite.ForwardingRule{
					{Name: "fr-1", AllowGlobalAccess: true},
				},
			},

			wantFRs: []*composite.ForwardingRule{
				{Name: "fr-1", AllowGlobalAccess: true},
				{Name: "dont-touch-me"},
			},
		},
		{
			desc: "multiple operations",
			haveFRs: []*composite.ForwardingRule{
				{Name: "dont-touch-me"},

				{Name: "fr-delete-1"},
				{Name: "fr-delete-2"},

				{Name: "fr-update-1"},
				{Name: "fr-update-2"},
				{Name: "fr-update-3"},
			},
			ops: &optimized.APIOperations{
				Create: []*composite.ForwardingRule{
					{Name: "fr-create-1"},
					{Name: "fr-create-2"},
				},
				Update: []*composite.ForwardingRule{
					{Name: "fr-update-1", AllowGlobalAccess: true},
					{Name: "fr-update-2", NetworkTier: "PREMIUM"},
					{Name: "fr-update-3", NetworkTier: "STANDARD"},
				},
				Delete: []string{"fr-delete-1", "fr-delete-2"},
			},

			wantFRs: []*composite.ForwardingRule{
				{Name: "dont-touch-me"},

				{Name: "fr-update-1", AllowGlobalAccess: true},
				{Name: "fr-update-2", NetworkTier: "PREMIUM"},
				{Name: "fr-update-3", NetworkTier: "STANDARD"},

				{Name: "fr-create-1"},
				{Name: "fr-create-2"},
			},
		},
	}
	for _, tC := range testCases {
		tC := tC
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			vals := gce.DefaultTestClusterValues()
			fakeGCE := gce.NewFakeGCECloud(vals)
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)

			// By default Patch in mock doesn't do anything,
			// we need to add a hook to simulate the patch
			mockGCE.MockForwardingRules.PatchHook = frPatchHook

			provider := forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO())
			for _, fr := range tC.haveFRs {
				if err := provider.Create(fr); err != nil {
					t.Fatalf("Create(%+v) returned error: %v, want nil", fr, err)
				}
			}

			// Act
			if err := optimized.Apply(provider, tC.ops); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			// Assert by comparing all forwarding rules from the fake cloud
			all := filter.None
			got, err := provider.List(all)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			ignoreOpt := cmpopts.IgnoreFields(composite.ForwardingRule{}, "Version", "SelfLink", "Region")
			sortOpt := cmpopts.SortSlices(func(x, y *composite.ForwardingRule) bool { return x.Name < y.Name })
			if diff := cmp.Diff(tC.wantFRs, got, ignoreOpt, sortOpt); diff != "" {
				t.Errorf("unexpected difference in Forwarding Rules (-want +got):\n%s", diff)
			}
		})
	}
}

// TestApplyErrs tests error cases for Apply function.
// This is done by checking that each stage can return an error.
func TestApplyErrs(t *testing.T) {
	var (
		deleteErr = errors.New("delete error")
		patchErr  = errors.New("patch error")
		createErr = errors.New("create error")
	)

	testCases := []struct {
		desc      string
		deleteErr error
		patchErr  error
		createErr error

		wantErr error
	}{
		{
			desc:    "no errors",
			wantErr: nil,
		},
		{
			desc:      "delete",
			deleteErr: deleteErr,
			wantErr:   deleteErr,
		},
		{
			desc:     "patch",
			patchErr: patchErr,
			wantErr:  patchErr,
		},
		{
			desc:      "create",
			createErr: createErr,
			wantErr:   createErr,
		},
		{
			desc:      "all fail, return first",
			deleteErr: deleteErr,
			patchErr:  patchErr,
			createErr: createErr,

			wantErr: deleteErr,
		},
		{
			desc:      "patch and create fail, return patch",
			patchErr:  patchErr,
			createErr: createErr,

			wantErr: patchErr,
		},
	}
	for _, tC := range testCases {
		tC := tC
		t.Run(tC.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			vals := gce.DefaultTestClusterValues()
			fakeGCE := gce.NewFakeGCECloud(vals)

			// Actual Forwarding Rules in fakeGCE don't matter
			// We use mock hooks to inject errors/successes
			mockGCE := fakeGCE.Compute().(*cloud.MockGCE)
			mockGCE.MockForwardingRules.DeleteHook = func(_ context.Context, _ *meta.Key, _ *cloud.MockForwardingRules, _ ...cloud.Option) (bool, error) {
				if tC.deleteErr != nil {
					return true, tC.deleteErr
				}
				return false, nil
			}
			mockGCE.MockForwardingRules.PatchHook = func(_ context.Context, _ *meta.Key, _ *compute.ForwardingRule, _ *cloud.MockForwardingRules, _ ...cloud.Option) error {
				if tC.patchErr != nil {
					return tC.patchErr
				}
				return nil
			}
			mockGCE.MockForwardingRules.InsertHook = func(_ context.Context, _ *meta.Key, _ *compute.ForwardingRule, _ *cloud.MockForwardingRules, _ ...cloud.Option) (bool, error) {
				if tC.createErr != nil {
					return true, tC.createErr
				}
				return false, nil
			}

			provider := forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO())
			ops := &optimized.APIOperations{
				Delete: []string{"fr-delete"},
				Update: []*composite.ForwardingRule{{Name: "fr-update"}},
				Create: []*composite.ForwardingRule{{Name: "fr-create"}},
			}

			// Act
			err := optimized.Apply(provider, ops)

			if diff := cmp.Diff(err, tC.wantErr, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("Apply() returned unexpected error diff (-want +got):\n%s", diff)
			}
		})
	}
}

func frPatchHook(_ context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules, _ ...cloud.Option) error {
	existing, err := m.Get(context.TODO(), key)
	if err != nil {
		return err
	}
	existing.Description = obj.Description
	existing.AllowGlobalAccess = obj.AllowGlobalAccess
	existing.NetworkTier = obj.NetworkTier
	return nil
}
