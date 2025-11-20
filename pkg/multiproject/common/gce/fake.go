package gce

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	compute "google.golang.org/api/compute/v1"
	cloudgce "k8s.io/cloud-provider-gcp/providers/gce"
	v1 "k8s.io/ingress-gce/pkg/apis/providerconfig/v1"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/klog/v2"
)

type GCEFake struct {
	defaultTestClusterValues  cloudgce.TestClusterValues
	clientsForProviderConfigs map[string]*cloudgce.Cloud
}

func NewGCEFake() *GCEFake {
	return &GCEFake{
		defaultTestClusterValues:  cloudgce.DefaultTestClusterValues(),
		clientsForProviderConfigs: make(map[string]*cloudgce.Cloud),
	}
}

func providerConfigKey(providerConfig *v1.ProviderConfig) string {
	return fmt.Sprintf("%s/%s", providerConfig.Namespace, providerConfig.Name)
}

// GCEForProviderConfig returns a new Fake GCE client for the given provider config.
// It stores the client in the GCEFake and returns it if the same provider config is requested again.
func (g *GCEFake) GCEForProviderConfig(providerConfig *v1.ProviderConfig, logger klog.Logger) (*cloudgce.Cloud, error) {
	pcKey := providerConfigKey(providerConfig)
	if g.clientsForProviderConfigs[pcKey] != nil {
		return g.clientsForProviderConfigs[pcKey], nil
	}

	// Copy the default test cluster values
	updatedConfig := g.defaultTestClusterValues
	updatedConfig.ProjectID = providerConfig.Spec.ProjectID
	updatedConfig.NetworkURL = fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/global/networks/%s", providerConfig.Spec.ProjectID, providerConfig.Spec.NetworkConfig.Network)
	updatedConfig.SubnetworkURL = fmt.Sprintf("https://www.googleapis.com/compute/v1/projects/%s/regions/%s/subnetworks/%s", providerConfig.Spec.ProjectID, updatedConfig.Region, providerConfig.Spec.NetworkConfig.SubnetInfo.Subnetwork)
	logger.Info("Creating GCEFake for provider config", "providerConfig", providerConfig.Name, "updatedConfig", updatedConfig)
	fakeCloud := cloudgce.NewFakeGCECloud(updatedConfig)
	_, err := createNetwork(fakeCloud, providerConfig.Spec.NetworkConfig.Network)
	if err != nil {
		return nil, err
	}
	_, err = createSubnetwork(fakeCloud, providerConfig.Spec.NetworkConfig.SubnetInfo.Subnetwork, providerConfig.Spec.NetworkConfig.Network)
	if err != nil {
		return nil, err
	}
	if err := createAndInsertNodes(fakeCloud, []string{"test-node-1"}, updatedConfig.ZoneName); err != nil {
		return nil, err
	}

	g.clientsForProviderConfigs[pcKey] = fakeCloud
	return fakeCloud, nil
}

func createAndInsertNodes(cloud *cloudgce.Cloud, nodeNames []string, zone string) error {
	if _, err := test.CreateAndInsertNodes(cloud, nodeNames, zone); err != nil {
		return err
	}
	return nil
}

func createNetwork(c *cloudgce.Cloud, networkName string) (*compute.Network, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	key := meta.GlobalKey(networkName)
	err := c.Compute().Networks().Insert(ctx, key, &compute.Network{Name: networkName})
	if err != nil {
		return nil, err
	}

	network, err := c.Compute().Networks().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return network, nil
}

func createSubnetwork(c *cloudgce.Cloud, subnetworkName string, networkName string) (*compute.Subnetwork, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()

	key := meta.GlobalKey(subnetworkName)
	err := c.Compute().Subnetworks().Insert(ctx, key, &compute.Subnetwork{Name: subnetworkName, Network: networkName})
	if err != nil {
		return nil, err
	}

	subnetwork, err := c.Compute().Subnetworks().Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return subnetwork, nil
}

// assert that the GCEFake implements the GCECreator interface
var _ GCECreator = &GCEFake{}
