package annotations_test

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/annotations"
)

func TestNetworkTier(t *testing.T) {
	testCases := []struct {
		desc                 string
		service              *api_v1.Service
		wantTier             cloud.NetworkTier
		wantIsFromAnnotation bool
	}{
		{
			desc:     "empty",
			service:  &api_v1.Service{},
			wantTier: cloud.NetworkTierDefault,
		},
		{
			desc: "standard",
			service: &api_v1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Standard",
					},
				},
			},
			wantTier:             cloud.NetworkTierStandard,
			wantIsFromAnnotation: true,
		},
		{
			desc: "premium",
			service: &api_v1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Premium",
					},
				},
			},
			wantTier:             cloud.NetworkTierPremium,
			wantIsFromAnnotation: true,
		},
		{
			desc: "typo",
			service: &api_v1.Service{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						annotations.NetworkTierAnnotationKey: "Typo",
					},
				},
			},
			wantTier: cloud.NetworkTierDefault,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			gotTier, gotIsFromAnnotation := annotations.NetworkTier(tC.service)
			if gotTier != tC.wantTier {
				t.Errorf("NetworkTier(_).tier = %v, want %v", gotTier, tC.wantTier)
			}
			if gotIsFromAnnotation != tC.wantIsFromAnnotation {
				t.Errorf("NetworkTier(_).isFromAnnotation = %v, want %v", gotIsFromAnnotation, tC.wantIsFromAnnotation)
			}
		})
	}
}
