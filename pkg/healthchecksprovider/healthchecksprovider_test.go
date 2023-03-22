package healthchecksprovider

import (
	"fmt"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

func TestCreateHealthCheck(t *testing.T) {
	testCases := []struct {
		healthCheck *composite.HealthCheck
		desc        string
	}{
		{
			desc: "Create regional health check",
			healthCheck: &composite.HealthCheck{
				Name:  "regional-hc",
				Scope: meta.Regional,
			},
		},
		{
			desc: "Create global health check",
			healthCheck: &composite.HealthCheck{
				Name:  "global-hc",
				Scope: meta.Global,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			hc := NewHealthChecks(fakeGCE, meta.VersionGA)

			err := hc.Create(tc.healthCheck)
			if err != nil {
				t.Fatalf("hc.Create(%v), returned error %v, want nil", tc.healthCheck, err)
			}

			err = verifyHealthCheckExists(fakeGCE, tc.healthCheck.Name, tc.healthCheck.Scope)
			if err != nil {
				t.Errorf("verifyHealthCheckExists(_, %s, %s) returned error %v, want nil", tc.healthCheck.Name, tc.healthCheck.Scope, err)
			}
		})
	}
}

func TestGetHealthCheck(t *testing.T) {
	regionalHealthCheck := &composite.HealthCheck{
		Name:    "regional-hc",
		Version: meta.VersionGA,
		Scope:   meta.Regional,
	}
	globalHealthCheck := &composite.HealthCheck{
		Name:    "global-hc",
		Version: meta.VersionGA,
		Scope:   meta.Global,
	}

	testCases := []struct {
		existingHealthChecks []*composite.HealthCheck
		getHCName            string
		getHCScope           meta.KeyType
		expectedHealthCheck  *composite.HealthCheck
		desc                 string
	}{
		{
			desc:                 "Get regional health check",
			existingHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			getHCName:            regionalHealthCheck.Name,
			getHCScope:           regionalHealthCheck.Scope,
			expectedHealthCheck:  regionalHealthCheck,
		},
		{
			desc:                 "Get global health check",
			existingHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			getHCName:            globalHealthCheck.Name,
			getHCScope:           globalHealthCheck.Scope,
			expectedHealthCheck:  globalHealthCheck,
		},
		{
			desc:                 "Get non existent global health check",
			existingHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			getHCName:            "non-existent-hc",
			getHCScope:           meta.Global,
			expectedHealthCheck:  nil,
		},
		{
			desc:                 "Get non existent regional health check",
			existingHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			getHCName:            "non-existent-hc",
			getHCScope:           meta.Regional,
			expectedHealthCheck:  nil,
		},
		{
			desc:                 "Get existent regional health check, but providing global scope",
			existingHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			getHCName:            regionalHealthCheck.Name,
			getHCScope:           meta.Global,
			expectedHealthCheck:  nil,
		},
		{
			desc:                 "Get existent global health check, but providing regional scope",
			existingHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			getHCName:            globalHealthCheck.Name,
			getHCScope:           meta.Regional,
			expectedHealthCheck:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			mustCreateHealthChecks(t, fakeGCE, tc.existingHealthChecks)
			hcp := NewHealthChecks(fakeGCE, meta.VersionGA)

			hc, err := hcp.Get(tc.getHCName, tc.getHCScope)
			if err != nil {
				t.Fatalf("hcp.Get(%v), returned error %v, want nil", tc.getHCName, err)
			}

			// Scope field gets removed (but region added), after creating health check
			ignoreFields := cmpopts.IgnoreFields(composite.HealthCheck{}, "SelfLink", "Region", "Scope")
			if !cmp.Equal(hc, tc.expectedHealthCheck, ignoreFields) {
				diff := cmp.Diff(hc, tc.expectedHealthCheck, ignoreFields)
				t.Errorf("hcp.Get(s) returned %v, not equal to expectedHealthCheck %v, diff: %v", hc, tc.expectedHealthCheck, diff)
			}
		})
	}
}

func TestDeleteHealthCheck(t *testing.T) {
	regionalHealthCheck := &composite.HealthCheck{
		Name:    "regional-hc",
		Version: meta.VersionGA,
		Scope:   meta.Regional,
	}
	globalHealthCheck := &composite.HealthCheck{
		Name:    "global-hc",
		Version: meta.VersionGA,
		Scope:   meta.Global,
	}

	testCases := []struct {
		existingHealthChecks        []*composite.HealthCheck
		deleteHCName                string
		deleteHCScope               meta.KeyType
		shouldNotDeleteHealthChecks []*composite.HealthCheck
		desc                        string
	}{
		{
			desc:                        "Delete regional health check",
			existingHealthChecks:        []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			deleteHCName:                regionalHealthCheck.Name,
			deleteHCScope:               regionalHealthCheck.Scope,
			shouldNotDeleteHealthChecks: []*composite.HealthCheck{globalHealthCheck},
		},
		{
			desc:                        "Delete global health check",
			existingHealthChecks:        []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			deleteHCName:                globalHealthCheck.Name,
			deleteHCScope:               globalHealthCheck.Scope,
			shouldNotDeleteHealthChecks: []*composite.HealthCheck{regionalHealthCheck},
		},
		{
			desc:                        "Delete non existent healthCheck",
			existingHealthChecks:        []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			deleteHCName:                "non-existent",
			deleteHCScope:               meta.Regional,
			shouldNotDeleteHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
		},
		{
			desc:                        "Delete global health check name, but using regional scope",
			existingHealthChecks:        []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			deleteHCName:                globalHealthCheck.Name,
			deleteHCScope:               meta.Regional,
			shouldNotDeleteHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
		},
		{
			desc:                        "Delete regional health check name, but using global scope",
			existingHealthChecks:        []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
			deleteHCName:                regionalHealthCheck.Name,
			deleteHCScope:               meta.Global,
			shouldNotDeleteHealthChecks: []*composite.HealthCheck{regionalHealthCheck, globalHealthCheck},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
			mustCreateHealthChecks(t, fakeGCE, tc.existingHealthChecks)
			hc := NewHealthChecks(fakeGCE, meta.VersionGA)

			err := hc.Delete(tc.deleteHCName, tc.deleteHCScope)
			if err != nil {
				t.Fatalf("hc.Delete(%v), returned error %v, want nil", tc.deleteHCName, err)
			}

			err = verifyHealthCheckNotExists(fakeGCE, tc.deleteHCName, tc.deleteHCScope)
			if err != nil {
				t.Errorf("verifyHealthCheckNotExists(_, %s, %s) returned error %v", tc.deleteHCName, tc.deleteHCScope, err)
			}
			for _, hc := range tc.shouldNotDeleteHealthChecks {
				err = verifyHealthCheckExists(fakeGCE, hc.Name, hc.Scope)
				if err != nil {
					t.Errorf("verifyHealthCheckExists(_, %s, %s) returned error %v", hc.Name, hc.Scope, err)
				}
			}
		})
	}
}

func verifyHealthCheckExists(cloud *gce.Cloud, name string, scope meta.KeyType) error {
	return verifyHealthCheckShouldExist(cloud, name, scope, true)
}

func verifyHealthCheckNotExists(cloud *gce.Cloud, name string, scope meta.KeyType) error {
	return verifyHealthCheckShouldExist(cloud, name, scope, false)
}

func verifyHealthCheckShouldExist(cloud *gce.Cloud, name string, scope meta.KeyType, shouldExist bool) error {
	key, err := composite.CreateKey(cloud, name, scope)
	if err != nil {
		return fmt.Errorf("hailed to create key for fetching health check %s, err: %w", name, err)
	}
	_, err = composite.GetHealthCheck(cloud, key, meta.VersionGA)
	if err != nil {
		if utils.IsNotFoundError(err) {
			if shouldExist {
				return fmt.Errorf("health check %s in scope %s was not found", name, scope)
			}
			return nil
		}
		return fmt.Errorf("composite.GetHealthCheck(_, %v, %v) returned error %w, want nil", key, meta.VersionGA, err)
	}
	if !shouldExist {
		return fmt.Errorf("health Check %s in scope %s exists, expected to be not found", name, scope)
	}
	return nil
}

func mustCreateHealthChecks(t *testing.T, cloud *gce.Cloud, hcs []*composite.HealthCheck) {
	t.Helper()

	for _, hc := range hcs {
		mustCreateHealthCheck(t, cloud, hc)
	}
}

func mustCreateHealthCheck(t *testing.T, cloud *gce.Cloud, hc *composite.HealthCheck) {
	t.Helper()

	key, err := composite.CreateKey(cloud, hc.Name, hc.Scope)
	if err != nil {
		t.Fatalf("composite.CreateKey(_, %s, %s) returned error %v, want nil", hc.Name, hc.Scope, err)
	}
	err = composite.CreateHealthCheck(cloud, key, hc)
	if err != nil {
		t.Fatalf("composite.CreateHealthCheck(_, %s, %v) returned error %v, want nil", key, hc, err)
	}
}
