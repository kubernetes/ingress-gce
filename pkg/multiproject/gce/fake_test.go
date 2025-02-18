package gce

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/klog/v2"
)

func TestNewGCEForProviderConfig(t *testing.T) {
	fake := NewGCEFake()

	providerConfig := &v1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-config",
		},
		Spec: v1.ProviderConfigSpec{
			ProjectID: "custom-project-id",
			NetworkConfig: &v1.NetworkConfig{
				Network:           "custom-network",
				DefaultSubnetwork: "custom-subnetwork",
			},
		},
	}

	logger := klog.TODO()
	cloud, err := fake.NewGCEForProviderConfig(providerConfig, logger)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cloud == nil {
		t.Fatal("expected cloud instance, got nil")
	}
	if cloud.ProjectID() != providerConfig.Spec.ProjectID {
		t.Errorf("expected project id %q, got %q", providerConfig.Spec.ProjectID, cloud.ProjectID())
	}
}
