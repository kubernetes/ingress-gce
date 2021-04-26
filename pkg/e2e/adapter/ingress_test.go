package adapter

import (
	"reflect"
	"testing"

	"github.com/kr/pretty"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/api/networking/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLegacyConversions(t *testing.T) {
	for _, tc := range []struct {
		netV1beta1 *v1beta1.Ingress
		netV1      *networkingv1.Ingress
	}{
		{
			netV1beta1: &v1beta1.Ingress{
				TypeMeta:   v1.TypeMeta{APIVersion: v1beta1GroupVersion},
				ObjectMeta: v1.ObjectMeta{Namespace: "ns1", Name: "foo"},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "svc1",
					},
				},
			},
			netV1: &networkingv1.Ingress{
				TypeMeta:   v1.TypeMeta{APIVersion: v1GroupVersion},
				ObjectMeta: v1.ObjectMeta{Namespace: "ns1", Name: "foo"},
				Spec: networkingv1.IngressSpec{
					DefaultBackend: &networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: "svc1",
						},
					},
				},
			},
		},
	} {
		gotV1, err := toIngressV1(tc.netV1beta1)
		if err != nil {
			t.Errorf("got error in conversion from v1 to v1beta1: %q", err)
		}
		gotV1beta1, err := toIngressV1beta1(tc.netV1)
		if err != nil {
			t.Errorf("got error in conversion from v1beta1 to v1: %q", err)
		}

		if !reflect.DeepEqual(gotV1beta1, tc.netV1beta1) {
			t.Errorf("Got\n%s\nwant\n%s", pretty.Sprint(gotV1beta1), pretty.Sprint(tc.netV1beta1))
		}
		if !reflect.DeepEqual(gotV1, tc.netV1) {
			t.Errorf("Got\n%s\nwant\n%s", pretty.Sprint(gotV1), pretty.Sprint(tc.netV1))
		}
	}
}
