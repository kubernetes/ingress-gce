/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package negstatushandler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	negv1beta1 "k8s.io/ingress-gce/pkg/apis/svcneg/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/neg/metrics"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/ingress-gce/pkg/network"
	negfake "k8s.io/ingress-gce/pkg/svcneg/client/clientset/versioned/fake"
	"k8s.io/ingress-gce/pkg/utils/consistency"
	"k8s.io/klog/v2"
)

const (
	svcNegTestNamespace = "test-ns"
	svcNegTestName      = "test-neg"
)

// svcNegTestNEG is the GCE NEG the tests report on the SvcNeg CR.
var svcNegTestNEG = &composite.NetworkEndpointGroup{
	Id:                  1,
	SelfLink:            "https://www.googleapis.com/compute/v1/projects/mock-project/zones/zone1/networkEndpointGroups/" + svcNegTestName,
	NetworkEndpointType: string(negtypes.VmIpPortEndpointType),
}

// newSvcNegTestCR returns a SvcNeg CR with an empty status at the given
// ResourceVersion.
func newSvcNegTestCR(rv string) *negv1beta1.ServiceNetworkEndpointGroup {
	return &negv1beta1.ServiceNetworkEndpointGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       svcNegTestNamespace,
			Name:            svcNegTestName,
			UID:             "neg-uid",
			ResourceVersion: rv,
		},
	}
}

// newSvcNegTestStore returns a real ConsistencyStore for SvcNeg CRs whose
// informer has observed the given ResourceVersion.
func newSvcNegTestStore(informerRV string) consistency.ConsistencyStore {
	return consistency.NewConsistencyStore(map[schema.GroupResource]consistency.LastSyncRVGetter{
		negtypes.SvcNegGroupResource: consistency.NewFakeRVGetter(informerRV),
	}, klog.TODO())
}

// newSvcNegTestHandler returns a status handler for cr. Both the fake client
// and the lister hold a copy of cr. Nothing propagates a write from the
// client to the lister, which is how the tests keep the informer behind. As
// in neg.NewController, the handler writes through the recording client, so
// that its patches are recorded in store.
func newSvcNegTestHandler(t *testing.T, cr *negv1beta1.ServiceNetworkEndpointGroup, store consistency.ConsistencyStore) (*SvcNegStatusHandler, *negfake.Clientset, cache.Indexer) {
	t.Helper()

	client := negfake.NewSimpleClientset(cr.DeepCopy())
	lister := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if err := lister.Add(cr.DeepCopy()); err != nil {
		t.Fatalf("adding the SvcNeg CR to the lister = %v, want nil", err)
	}
	handler := NewSvcNegStatusHandler(negtypes.NewRecordingSvcNegClient(client, store), lister, cr.Namespace, cr.Name, network.NetworkInfo{}, nil, metrics.FakeNegMetrics(), store, klog.TODO())
	return handler, client, lister
}

// TestReportSyncStatusContinuesFromUnobservedPatch is the case the handler
// keeps its last patch for: ReportSyncStatus runs before the informer has
// observed the patch ReportStatus just sent. Computed from the lister's
// copy, its merge patch would replace the conditions and drop Initialized.
func TestReportSyncStatusContinuesFromUnobservedPatch(t *testing.T) {
	// The informer has observed ResourceVersion 100. The fake client neither
	// assigns ResourceVersions nor enforces the precondition, so the CR
	// stays at 150: that is the version each patch returns and the store
	// records, ahead of the informer.
	handler, client, _ := newSvcNegTestHandler(t, newSvcNegTestCR("150"), newSvcNegTestStore("100"))

	negs := []*composite.NetworkEndpointGroup{svcNegTestNEG}
	if err := handler.ReportStatus(negs, nil); err != nil {
		t.Fatalf("ReportStatus() = %v, want nil", err)
	}

	needInit, err := handler.ReportSyncStatus(nil)
	if err != nil {
		t.Fatalf("ReportSyncStatus(nil) = _, %v, want nil", err)
	}
	if needInit {
		t.Errorf("ReportSyncStatus(nil) = true, want false: the NEGs reported by ReportStatus are initialized")
	}

	got, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcNegTestNamespace).Get(context.Background(), svcNegTestName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%q) = %v, want nil", svcNegTestName, err)
	}
	var gotConditions []string
	for _, condition := range got.Status.Conditions {
		gotConditions = append(gotConditions, condition.Type)
	}
	wantConditions := []string{negv1beta1.Initialized, negv1beta1.Synced}
	if diff := cmp.Diff(wantConditions, gotConditions); diff != "" {
		t.Errorf("Condition types after ReportStatus() and ReportSyncStatus() mismatch (-want +got):\n%s", diff)
	}
	if len(got.Status.NetworkEndpointGroups) != len(negs) {
		t.Errorf("Status has %d NEG references after ReportStatus() and ReportSyncStatus(), want %d", len(got.Status.NetworkEndpointGroups), len(negs))
	}
}

// TestGetSvcNegFromStore covers which copy of the CR the status is computed
// from: the informer's, or the handler's last patch while the informer has
// not observed it.
func TestGetSvcNegFromStore(t *testing.T) {
	lastPatched := newSvcNegTestCR("150")
	lastPatchedOldIncarnation := newSvcNegTestCR("150")
	lastPatchedOldIncarnation.UID = "old-uid"

	for _, tc := range []struct {
		desc        string
		lastPatched *negv1beta1.ServiceNetworkEndpointGroup
		informerRV  string
		// wantLastPatched is whether the last patch is returned instead of
		// the informer's copy.
		wantLastPatched bool
	}{
		{
			desc:            "nothing patched yet",
			lastPatched:     nil,
			informerRV:      "100",
			wantLastPatched: false,
		},
		{
			desc:            "patch not observed by the informer",
			lastPatched:     lastPatched,
			informerRV:      "100",
			wantLastPatched: true,
		},
		{
			desc:            "patch observed by the informer",
			lastPatched:     lastPatched,
			informerRV:      "150",
			wantLastPatched: false,
		},
		{
			desc:            "CR recreated since the patch",
			lastPatched:     lastPatchedOldIncarnation,
			informerRV:      "100",
			wantLastPatched: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			store := newSvcNegTestStore(tc.informerRV)
			handler, _, lister := newSvcNegTestHandler(t, newSvcNegTestCR("100"), store)
			// This controller's last write to the CR returned version 150.
			store.WroteAt(negtypes.SvcNegRef(svcNegTestNamespace, svcNegTestName), "neg-uid", "150")
			handler.lastPatched.Store(tc.lastPatched)
			cached, _, err := lister.GetByKey(svcNegTestNamespace + "/" + svcNegTestName)
			if err != nil {
				t.Fatalf("lister.GetByKey() = %v, want nil", err)
			}
			want := cached.(*negv1beta1.ServiceNetworkEndpointGroup)
			if tc.wantLastPatched {
				want = tc.lastPatched
			}

			got, err := handler.getSvcNegFromStore()
			if err != nil {
				t.Fatalf("getSvcNegFromStore() = _, %v, want nil", err)
			}

			if got != want {
				t.Errorf("getSvcNegFromStore() returned the last patch = %t, want %t", got == tc.lastPatched, tc.wantLastPatched)
			}
		})
	}
}

// TestSvcNegStatusPatchPrecondition verifies that the status patch carries
// the resourceVersion precondition only with consistency checking enabled,
// so that the default behavior is an unconditional patch.
func TestSvcNegStatusPatchPrecondition(t *testing.T) {
	for _, tc := range []struct {
		desc                string
		store               consistency.ConsistencyStore
		wantResourceVersion string
	}{
		{
			desc:                "consistency checking enabled",
			store:               newSvcNegTestStore("150"),
			wantResourceVersion: "150",
		},
		{
			desc:                "consistency checking disabled",
			store:               consistency.NewNoopConsistencyStore(),
			wantResourceVersion: "",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			handler, client, _ := newSvcNegTestHandler(t, newSvcNegTestCR("150"), tc.store)
			var gotPatch []byte
			client.PrependReactor("patch", "servicenetworkendpointgroups", func(action k8stesting.Action) (bool, runtime.Object, error) {
				gotPatch = action.(k8stesting.PatchAction).GetPatch()
				return false, nil, nil
			})

			if _, err := handler.ReportSyncStatus(nil); err != nil {
				t.Fatalf("ReportSyncStatus(nil) = _, %v, want nil", err)
			}

			var patch struct {
				Metadata struct {
					ResourceVersion string `json:"resourceVersion"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(gotPatch, &patch); err != nil {
				t.Fatalf("json.Unmarshal(%s) = %v, want nil", gotPatch, err)
			}
			if patch.Metadata.ResourceVersion != tc.wantResourceVersion {
				t.Errorf("ReportSyncStatus(nil) sent patch %s with metadata.resourceVersion %q, want %q", gotPatch, patch.Metadata.ResourceVersion, tc.wantResourceVersion)
			}
		})
	}
}

// setLiveSvcNeg makes the API server's copy of the test CR differ from the
// informer's: it is at version 150 and has the given condition, which some
// other writer set after the informer's copy was read.
func setLiveSvcNeg(t *testing.T, client *negfake.Clientset, conditionType string) {
	t.Helper()

	live := newSvcNegTestCR("150")
	live.Status.Conditions = []negv1beta1.Condition{{Type: conditionType, Status: v1.ConditionTrue}}
	live.Status.NetworkEndpointGroups = []negv1beta1.NegObjectReference{{Id: "1", SelfLink: svcNegTestNEG.SelfLink}}
	if _, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcNegTestNamespace).Update(context.Background(), live, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("Update(%q) = %v, want nil", svcNegTestName, err)
	}
}

// stalePatchRejecter is a reactor for patches of the test CR. It rejects,
// with a Conflict, every patch whose resourceVersion precondition is not the
// live version 150, as the API server does: the fake clientset does not
// enforce the precondition itself. It records the preconditions it has seen.
type stalePatchRejecter struct {
	preconditions []string
}

func (r *stalePatchRejecter) react(action k8stesting.Action) (bool, runtime.Object, error) {
	var patch struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(action.(k8stesting.PatchAction).GetPatch(), &patch); err != nil {
		return true, nil, err
	}
	r.preconditions = append(r.preconditions, patch.Metadata.ResourceVersion)
	if patch.Metadata.ResourceVersion != "150" {
		return true, nil, apierrors.NewConflict(negtypes.SvcNegGroupResource, svcNegTestName, errors.New("the object has been modified"))
	}
	return false, nil, nil
}

// svcNegTestConditionTypes returns the condition types on the API server's
// copy of the test CR.
func svcNegTestConditionTypes(t *testing.T, client *negfake.Clientset) []string {
	t.Helper()

	got, err := client.NetworkingV1beta1().ServiceNetworkEndpointGroups(svcNegTestNamespace).Get(context.Background(), svcNegTestName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%q) = %v, want nil", svcNegTestName, err)
	}
	var types []string
	for _, condition := range got.Status.Conditions {
		types = append(types, condition.Type)
	}
	return types
}

// TestReportSyncStatusRetriesConflictFromLiveRead covers a status patch that
// the API server rejects because the CR changed after the handler read it.
// The handler must recompute the status from a live read and patch again:
// the syncer only logs a failed report, so nothing else would retry it.
func TestReportSyncStatusRetriesConflictFromLiveRead(t *testing.T) {
	// The informer holds the CR at version 100 with an empty status.
	handler, client, _ := newSvcNegTestHandler(t, newSvcNegTestCR("100"), newSvcNegTestStore("100"))
	setLiveSvcNeg(t, client, negv1beta1.Initialized)
	rejecter := &stalePatchRejecter{}
	client.PrependReactor("patch", "servicenetworkendpointgroups", rejecter.react)

	needInit, err := handler.ReportSyncStatus(nil)
	if err != nil {
		t.Fatalf("ReportSyncStatus(nil) = _, %v, want nil", err)
	}

	if needInit {
		t.Errorf("ReportSyncStatus(nil) = true, want false: the live CR is initialized and has NEG references")
	}
	if diff := cmp.Diff([]string{"100", "150"}, rejecter.preconditions); diff != "" {
		t.Errorf("resourceVersion preconditions of the patches sent by ReportSyncStatus(nil) mismatch (-want +got):\n%s", diff)
	}
	// Built from the informer's copy, the patch would have replaced the
	// conditions and dropped Initialized.
	if diff := cmp.Diff([]string{negv1beta1.Initialized, negv1beta1.Synced}, svcNegTestConditionTypes(t, client)); diff != "" {
		t.Errorf("Condition types after ReportSyncStatus(nil) mismatch (-want +got):\n%s", diff)
	}
}

// TestReportStatusRetriesConflictFromLiveRead is
// TestReportSyncStatusRetriesConflictFromLiveRead for ReportStatus, which
// computes its status separately.
func TestReportStatusRetriesConflictFromLiveRead(t *testing.T) {
	handler, client, _ := newSvcNegTestHandler(t, newSvcNegTestCR("100"), newSvcNegTestStore("100"))
	setLiveSvcNeg(t, client, negv1beta1.Synced)
	rejecter := &stalePatchRejecter{}
	client.PrependReactor("patch", "servicenetworkendpointgroups", rejecter.react)

	if err := handler.ReportStatus([]*composite.NetworkEndpointGroup{svcNegTestNEG}, nil); err != nil {
		t.Fatalf("ReportStatus() = %v, want nil", err)
	}

	if diff := cmp.Diff([]string{"100", "150"}, rejecter.preconditions); diff != "" {
		t.Errorf("resourceVersion preconditions of the patches sent by ReportStatus() mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{negv1beta1.Synced, negv1beta1.Initialized}, svcNegTestConditionTypes(t, client)); diff != "" {
		t.Errorf("Condition types after ReportStatus() mismatch (-want +got):\n%s", diff)
	}
}

// TestReportSyncStatusGivesUpOnRepeatedConflict verifies that the retry is
// bounded: a CR that keeps changing must not hold up the syncer.
func TestReportSyncStatusGivesUpOnRepeatedConflict(t *testing.T) {
	handler, client, _ := newSvcNegTestHandler(t, newSvcNegTestCR("100"), newSvcNegTestStore("100"))
	patches := 0
	client.PrependReactor("patch", "servicenetworkendpointgroups", func(k8stesting.Action) (bool, runtime.Object, error) {
		patches++
		return true, nil, apierrors.NewConflict(negtypes.SvcNegGroupResource, svcNegTestName, errors.New("the object has been modified"))
	})

	_, err := handler.ReportSyncStatus(nil)

	if !apierrors.IsConflict(err) {
		t.Errorf("ReportSyncStatus(nil) = _, %v, want a Conflict", err)
	}
	if patches != retry.DefaultRetry.Steps {
		t.Errorf("ReportSyncStatus(nil) sent %d patches, want %d", patches, retry.DefaultRetry.Steps)
	}
	if got := handler.lastPatched.Load(); got != nil {
		t.Errorf("lastPatched after failed ReportSyncStatus(nil) = %v, want nil", got)
	}
}

// TestPatchSvcNegStatusClearsLastPatchedOnError verifies that a failed status
// patch drops lastPatched, so that the next sync computes its patch from the
// informer's copy instead of a CR that may be what made the patch fail. An
// error other than a Conflict is not retried.
func TestPatchSvcNegStatusClearsLastPatchedOnError(t *testing.T) {
	t.Parallel()

	handler, client, lister := newSvcNegTestHandler(t, newSvcNegTestCR("150"), newSvcNegTestStore("100"))

	if err := handler.ReportStatus([]*composite.NetworkEndpointGroup{svcNegTestNEG}, nil); err != nil {
		t.Fatalf("ReportStatus() = %v, want nil", err)
	}
	if handler.lastPatched.Load() == nil {
		t.Fatal("lastPatched after successful ReportStatus() = nil, want non-nil")
	}

	failedPatches := 0
	client.PrependReactor("patch", "servicenetworkendpointgroups", func(k8stesting.Action) (bool, runtime.Object, error) {
		failedPatches++
		return true, nil, errors.New("apiserver unavailable")
	})
	if _, err := handler.ReportSyncStatus(nil); err == nil {
		t.Fatal("ReportSyncStatus(nil) = nil, want the patch error")
	}
	if failedPatches != 1 {
		t.Errorf("ReportSyncStatus(nil) sent %d patches, want 1: only a Conflict is retried", failedPatches)
	}
	if got := handler.lastPatched.Load(); got != nil {
		t.Errorf("lastPatched after failed ReportSyncStatus() = %v, want nil", got)
	}

	cached, _, err := lister.GetByKey(svcNegTestNamespace + "/" + svcNegTestName)
	if err != nil {
		t.Fatalf("lister.GetByKey() = %v, want nil", err)
	}
	got, err := handler.getSvcNegFromStore()
	if err != nil {
		t.Fatalf("getSvcNegFromStore() = %v, want nil", err)
	}
	if got != cached {
		t.Errorf("getSvcNegFromStore() after failed patch did not return the informer's copy")
	}
}
