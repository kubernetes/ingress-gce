package adapter

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sav1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1"
	sav1beta1 "k8s.io/ingress-gce/pkg/apis/serviceattachment/v1beta1"
)

func TestPSCConversions(t *testing.T) {
	for _, tc := range []struct {
		netV1beta1 *sav1beta1.ServiceAttachment
		netV1      *sav1.ServiceAttachment
	}{
		{
			netV1beta1: &sav1beta1.ServiceAttachment{
				TypeMeta:   v1.TypeMeta{APIVersion: v1beta1PSC},
				ObjectMeta: v1.ObjectMeta{Namespace: "ns1", Name: "foo"},
				Spec: sav1beta1.ServiceAttachmentSpec{
					ConnectionPreference: "my-preference",
					NATSubnets:           []string{"my-subnet"},
					ResourceRef: corev1.TypedLocalObjectReference{
						Kind: "Service",
						Name: "my-service",
					},
					ProxyProtocol: true,
					ConsumerAllowList: []sav1beta1.ConsumerProject{
						{
							ConnectionLimit: 100,
							Project:         "my-project-1",
						},
						{
							ConnectionLimit: 102,
							Project:         "my-project-2",
						},
					},
					ConsumerRejectList: []string{"reject-project-1", "reject-project-2"},
				},
				Status: sav1beta1.ServiceAttachmentStatus{
					ServiceAttachmentURL: "gce-service-attachment-url",
					ForwardingRuleURL:    "gce-forwarding-rule-url",
					ConsumerForwardingRules: []sav1beta1.ConsumerForwardingRule{
						{
							ForwardingRuleURL: "consumer-rule-1",
							Status:            "ESTABLISHED",
						},
						{
							ForwardingRuleURL: "consumer-rule-2",
							Status:            "DISCONNECTED",
						},
					},
				},
			},
			netV1: &sav1.ServiceAttachment{
				TypeMeta:   v1.TypeMeta{APIVersion: v1PSC},
				ObjectMeta: v1.ObjectMeta{Namespace: "ns1", Name: "foo"},
				Spec: sav1.ServiceAttachmentSpec{
					ConnectionPreference: "my-preference",
					NATSubnets:           []string{"my-subnet"},
					ResourceRef: corev1.TypedLocalObjectReference{
						Kind: "Service",
						Name: "my-service",
					},
					ProxyProtocol: true,
					ConsumerAllowList: []sav1.ConsumerProject{
						{
							ConnectionLimit: 100,
							Project:         "my-project-1",
						},
						{
							ConnectionLimit: 102,
							Project:         "my-project-2",
						},
					},
					ConsumerRejectList: []string{"reject-project-1", "reject-project-2"},
				},
				Status: sav1.ServiceAttachmentStatus{
					ServiceAttachmentURL: "gce-service-attachment-url",
					ForwardingRuleURL:    "gce-forwarding-rule-url",
					ConsumerForwardingRules: []sav1.ConsumerForwardingRule{
						{
							ForwardingRuleURL: "consumer-rule-1",
							Status:            "ESTABLISHED",
						},
						{
							ForwardingRuleURL: "consumer-rule-2",
							Status:            "DISCONNECTED",
						},
					},
				},
			},
		},
	} {
		gotV1 := toPSCV1(tc.netV1beta1)
		gotV1beta1 := toPSCV1beta1(tc.netV1)

		if diff := cmp.Diff(gotV1beta1, tc.netV1beta1); diff != "" {
			t.Errorf("V1beta1 ServiceAttachments are not the same(-want, +got):\n%s", diff)
		}
		if diff := cmp.Diff(gotV1, tc.netV1); diff != "" {
			t.Errorf("V1 ServiceAttachments are not the same(-want, +got):\n%s", diff)
		}
	}
}
