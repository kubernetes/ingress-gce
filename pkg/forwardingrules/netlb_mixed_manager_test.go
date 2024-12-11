package forwardingrules_test

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	api_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	kubeSystemUID = "ksuid123"
	namespace     = "test-ns"
	name          = "test-svc"
	ip            = "1.2.3.4"
	tcpName       = "k8s2-udp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	udpName       = "k8s2-tcp-axyqjz2d-test-ns-test-svc-pyn67fp6"
	legacyName    = "aksuid123"
)

func TestMixedManagerNetLB_DeleteIPv4(t *testing.T) {
	testCases := []struct {
		desc   string
		tcp    *compute.ForwardingRule
		udp    *compute.ForwardingRule
		legacy *compute.ForwardingRule
	}{
		{
			desc: "no rules",
		},
		{
			desc:   "single protocol exists",
			legacy: &compute.ForwardingRule{Name: legacyName},
		},
		{
			desc: "mixed protocol exists",
			tcp:  &compute.ForwardingRule{Name: tcpName},
			udp:  &compute.ForwardingRule{Name: udpName},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// Arrange
			vals := gce.DefaultTestClusterValues()
			fakeGCE := gce.NewFakeGCECloud(vals)
			for _, rule := range []*compute.ForwardingRule{tc.tcp, tc.udp, tc.legacy} {
				if rule != nil {
					fakeGCE.CreateRegionForwardingRule(rule, vals.Region)
				}
			}

			m := &forwardingrules.MixedManagerNetLB{
				Namer:    namer.NewL4Namer(kubeSystemUID, nil),
				Provider: forwardingrules.New(fakeGCE, meta.VersionGA, meta.Regional, klog.TODO()),
				Recorder: &record.FakeRecorder{},
				Logger:   klog.TODO(),
				Cloud:    fakeGCE,

				Service: &api_v1.Service{
					ObjectMeta: meta_v1.ObjectMeta{
						UID:       kubeSystemUID,
						Namespace: namespace,
						Name:      name,
					},
				},
			}

			// Act
			err := m.DeleteIPv4()
			// Assert
			if err != nil {
				t.Errorf("DeleteIPv4() error = %v", err)
			}

			if tc.legacy != nil {
				rule, err := fakeGCE.GetRegionForwardingRule(legacyName, vals.Region)
				if err != nil || rule == nil {
					t.Errorf("single protocol named forwarding rule was deleted by mixed manager")
				}
			}

			ruleTCP, err := fakeGCE.GetRegionForwardingRule(tcpName, vals.Region)
			if ruleTCP != nil || !utils.IsNotFoundError(err) {
				t.Errorf("tcp forwarding rule for mixed protocol wasn't deleted")
			}

			ruleUDP, err := fakeGCE.GetRegionForwardingRule(udpName, vals.Region)
			if ruleUDP != nil || !utils.IsNotFoundError(err) {
				t.Errorf("udp forwarding rule for mixed protocol wasn't deleted")
			}
		})
	}
}
