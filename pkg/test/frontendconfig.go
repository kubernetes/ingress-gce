package test

import (
	"k8s.io/api/networking/v1beta1"
	"k8s.io/ingress-gce/pkg/annotations"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
)

// The below vars are used for sharing unit testing types with multiple packages.
var (
	FrontendConfig = &frontendconfigv1beta1.FrontendConfig{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "config-test",
			Namespace: "test",
		},
	}

	IngressWithoutFrontendConfig = &v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "ing-no-config",
			Namespace: "test",
		},
	}

	IngressWithFrontendConfig = &v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "ing-with-config",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.FrontendConfigKey: "config-test",
			},
		},
	}

	IngressWithFrontendConfigOtherNamespace = &v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "ing-with-config",
			Namespace: "other-namespace",
			Annotations: map[string]string{
				annotations.FrontendConfigKey: "config-test",
			},
		},
	}

	IngressWithOtherFrontendConfig = &v1beta1.Ingress{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "ing-with-config",
			Namespace: "test",
			Annotations: map[string]string{
				annotations.FrontendConfigKey: "other-config",
			},
		},
	}
)
