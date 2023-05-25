package backends

import (
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/network"
	"k8s.io/ingress-gce/pkg/utils"
	namer "k8s.io/ingress-gce/pkg/utils/namer"
)

const (
	kubeSystemUID = "ksuid123"
)

func TestEnsureL4BackendService(t *testing.T) {
	serviceName := types.NamespacedName{Name: "test-service", Namespace: "test-ns"}
	fakeGCE := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	l4namer := namer.NewL4Namer(kubeSystemUID, nil)
	backendPool := NewPool(fakeGCE, l4namer)

	hcLink := l4namer.L4HealthCheck(serviceName.Namespace, serviceName.Name, false)
	bsName := l4namer.L4Backend(serviceName.Namespace, serviceName.Name)
	network := network.NetworkInfo{IsDefault: false, NetworkURL: "https://www.googleapis.com/compute/v1/projects/test-poject/global/networks/test-vpc"}
	bs, err := backendPool.EnsureL4BackendService(bsName, hcLink, "TCP", string(v1.ServiceAffinityNone), string(cloud.SchemeInternal), serviceName, network)
	if err != nil {
		t.Errorf("EnsureL4BackendService failed")
	}

	if bs.SessionAffinity != strings.ToUpper(string(v1.ServiceAffinityNone)) {
		t.Errorf("BackendService.SessionAffinity was not populated correctly want=%q, got=%q", strings.ToUpper(string(v1.ServiceAffinityNone)), bs.SessionAffinity)
	}
	if bs.Network != network.NetworkURL {
		t.Errorf("BackendService.Network was not populated correctly, want=%q, got=%q", network.NetworkURL, bs.Network)
	}
	if len(bs.HealthChecks) != 1 || bs.HealthChecks[0] != hcLink {
		t.Errorf("BackendService.HealthChecks was not populated correctly, want=%q, got=%q", hcLink, bs.HealthChecks)
	}
	description, err := utils.MakeL4LBServiceDescription(serviceName.String(), "", meta.VersionGA, false, utils.ILB)
	if err != nil {
		t.Errorf("utils.MakeL4LBServiceDescription() failed %v", err)
	}
	if bs.Description != description {
		t.Errorf("BackendService.Description was not populated correctly, want=%q, got=%q", description, bs.Description)
	}
	if bs.Protocol != "TCP" {
		t.Errorf("BackendService.Protocol was not populated correctly, want=%q, got=%q", "TCP", bs.Protocol)
	}
	if bs.LoadBalancingScheme != string(cloud.SchemeInternal) {
		t.Errorf("BackendService.LoadBalancingScheme was not populated correctly, want=%q, got=%q", string(cloud.SchemeInternal), bs.LoadBalancingScheme)
	}
	if bs.ConnectionDraining == nil || bs.ConnectionDraining.DrainingTimeoutSec != DefaultConnectionDrainingTimeoutSeconds {
		t.Errorf("BackendService.ConnectionDraining was not populated correctly, want=connection draining with %q, got=%q", DefaultConnectionDrainingTimeoutSeconds, bs.ConnectionDraining)
	}

}
