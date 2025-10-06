package l3_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/loadbalancers/l3"
)

func TestWants(t *testing.T) {
	flagVal := flags.F.EnableL3NetLBOptIn
	defer func() {
		flags.F.EnableL3NetLBOptIn = flagVal
	}()
	testCases := []struct {
		desc string
		svc  corev1.Service
		want bool
	}{
		{
			desc: "empty",
			svc:  corev1.Service{},
			want: false,
		},
		{
			desc: "enabled",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/l3-experiment": "enabled",
					},
				},
			},
			want: true,
		},
		{
			desc: "true",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/l3-experiment": "true",
					},
				},
			},
			want: true,
		},
		{
			desc: "false",
			svc: corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"networking.gke.io/l3-experiment": "false",
					},
				},
			},
			want: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			flags.F.EnableL3NetLBOptIn = true
			got := l3.Wants(&tC.svc)
			if got != tC.want {
				t.Errorf("WantsL3NetLB(%+v) = %v, want %v", tC.svc, got, tC.want)
			}
		})
		t.Run(tC.desc+" flag disabled", func(t *testing.T) {
			flags.F.EnableL3NetLBOptIn = false
			got := l3.Wants(&tC.svc)
			if got != false {
				t.Errorf("WantsL3NetLB(%+v) = %v, want %v", tC.svc, got, false)
			}
		})
	}
}
