package adapter

import (
	"reflect"
	"testing"

	"github.com/kr/pretty"
	extv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/api/networking/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLegacyConversions(t *testing.T) {
	for _, tc := range []struct {
		ext *extv1beta1.Ingress
		net *v1beta1.Ingress
	}{
		{
			ext: &extv1beta1.Ingress{
				TypeMeta:   v1.TypeMeta{APIVersion: "extensions/v1beta1"},
				ObjectMeta: v1.ObjectMeta{Namespace: "ns1", Name: "foo"},
				Spec: extv1beta1.IngressSpec{
					Backend: &extv1beta1.IngressBackend{
						ServiceName: "svc1",
					},
				},
			},
			net: &v1beta1.Ingress{
				TypeMeta:   v1.TypeMeta{APIVersion: "networking.k8s.io/v1beta1"},
				ObjectMeta: v1.ObjectMeta{Namespace: "ns1", Name: "foo"},
				Spec: v1beta1.IngressSpec{
					Backend: &v1beta1.IngressBackend{
						ServiceName: "svc1",
					},
				},
			},
		},
	} {
		gotExt := toIngressExtensionsGroup(tc.net)
		gotNet := toIngressNetworkingGroup(tc.ext)

		if !reflect.DeepEqual(gotExt, tc.ext) {
			t.Errorf("Got\n%s\nwant\n%s", pretty.Sprint(gotExt), pretty.Sprint(tc.ext))
		}
		if !reflect.DeepEqual(gotNet, tc.net) {
			t.Errorf("Got%s\nwant\n%s", pretty.Sprint(gotNet), pretty.Sprint(tc.net))
		}
	}
}
