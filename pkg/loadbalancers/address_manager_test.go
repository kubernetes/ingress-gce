/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package loadbalancers

import (
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"k8s.io/ingress-gce/pkg/utils"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/legacy-cloud-providers/gce"
)

const testSvcName = "my-service"
const testSubnet = "/projects/x/testRegions/us-central1/testSubnetworks/customsub"
const testLBName = "a111111111111111"

var vals = gce.DefaultTestClusterValues()

// TestAddressManagerNoRequestedIP tests the typical case of passing in no requested IP
func TestAddressManagerNoRequestedIP(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := ""

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault)
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeInternal), cloud.NetworkTierDefault.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerBasic tests the typical case of reserving and unreserving an address.
func TestAddressManagerBasic(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault)
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeInternal), cloud.NetworkTierDefault.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerOrphaned tests the case where the address exists with the IP being equal
// to the requested address (forwarding rule or loadbalancer IP).
func TestAddressManagerOrphaned(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	addr := &compute.Address{Name: testLBName, Address: targetIP, AddressType: string(cloud.SchemeInternal)}
	err = svc.ReserveRegionAddress(addr, vals.Region)
	require.NoError(t, err)

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault)
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeInternal), cloud.NetworkTierDefault.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerStandardNetworkTier tests the case where the address does not exists
// and checks if created address is in standard network tier
func TestAddressManagerStandardNetworkTier(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeExternal, cloud.NetworkTierStandard)
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeExternal), cloud.NetworkTierStandard.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerStandardNetworkTierNotAvailableForInternalAddress that reserved internal IP address will alway be in Premium Network Tier
func TestAddressManagerStandardNetworkTierNotAvailableForInternalAddress(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierStandard)
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeInternal), cloud.NetworkTierPremium.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerOutdatedOrphan tests the case where an address exists but points to
// an IP other than the forwarding rule or loadbalancer IP.
func TestAddressManagerOutdatedOrphan(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	previousAddress := "1.1.0.0"
	targetIP := "1.1.1.1"

	addr := &compute.Address{Name: testLBName, Address: previousAddress, AddressType: string(cloud.SchemeExternal)}
	err = svc.ReserveRegionAddress(addr, vals.Region)
	require.NoError(t, err)

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault)
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeInternal), cloud.NetworkTierDefault.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerExternallyOwned tests the case where the address exists but isn't
// owned by the controller.
func TestAddressManagerExternallyOwned(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	addr := &compute.Address{Name: "my-important-address", Address: targetIP, AddressType: string(cloud.SchemeInternal)}
	err = svc.ReserveRegionAddress(addr, vals.Region)
	require.NoError(t, err)

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierPremium)
	ipToUse, ipType, err := mgr.HoldAddress()
	require.NoError(t, err)
	assert.NotEmpty(t, ipToUse)
	assert.Equal(t, IPAddrUnmanaged, ipType, "IP Address should not be marked as controller's managed")

	ad, err := svc.GetRegionAddress(testLBName, vals.Region)
	assert.True(t, utils.IsNotFoundError(err))
	require.Nil(t, ad)

	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerExternallyOwnedWrongNetworkTier tests the case where the address exists but isn't
// owned by the controller and it's network tier doesn't match expected.
func TestAddressManagerExternallyOwnedWrongNetworkTier(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	addr := &compute.Address{Name: "my-important-address", Address: targetIP, AddressType: string(cloud.SchemeInternal), NetworkTier: string(cloud.NetworkTierStandard)}
	err = svc.ReserveRegionAddress(addr, vals.Region)
	require.NoError(t, err, "")
	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierPremium)
	_, _, err = mgr.HoldAddress()
	require.Error(t, err, "does not have the expected network tier")
}

// TestAddressManagerExternallyOwned tests the case where the address exists but isn't
// owned by the controller. However, this address has the wrong type.
func TestAddressManagerBadExternallyOwned(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	addr := &compute.Address{Name: "my-important-address", Address: targetIP, AddressType: string(cloud.SchemeExternal)}
	err = svc.ReserveRegionAddress(addr, vals.Region)
	require.NoError(t, err)

	mgr := newAddressManager(svc, testSvcName, vals.Region, testSubnet, testLBName, targetIP, cloud.SchemeInternal, cloud.NetworkTierPremium)
	ad, _, err := mgr.HoldAddress()
	assert.NotNil(t, err) // FIXME
	require.Equal(t, ad, "")
}

func testHoldAddress(t *testing.T, mgr *addressManager, svc gce.CloudAddressService, name, region, targetIP, scheme, netTier string) {
	ipToUse, ipType, err := mgr.HoldAddress()
	require.NoError(t, err)
	assert.NotEmpty(t, ipToUse)
	assert.Equal(t, IPAddrManaged, ipType, "IP Address should be marked as controller's managed")

	addr, err := svc.GetRegionAddress(name, region)
	require.NoError(t, err)
	if targetIP != "" {
		assert.EqualValues(t, targetIP, addr.Address)
	}
	assert.EqualValues(t, scheme, addr.AddressType)
	assert.EqualValues(t, addr.NetworkTier, netTier)
}

func testReleaseAddress(t *testing.T, mgr *addressManager, svc gce.CloudAddressService, name, region string) {
	err := mgr.ReleaseAddress()
	require.NoError(t, err)
	_, err = svc.GetRegionAddress(name, region)
	assert.True(t, utils.IsNotFoundError(err))
}

func fakeGCECloud(vals gce.TestClusterValues) (*gce.Cloud, error) {
	gce := gce.NewFakeGCECloud(vals)

	mockGCE := gce.Compute().(*cloud.MockGCE)
	mockGCE.MockTargetPools.AddInstanceHook = mock.AddInstanceHook
	mockGCE.MockTargetPools.RemoveInstanceHook = mock.RemoveInstanceHook
	mockGCE.MockForwardingRules.InsertHook = mock.InsertFwdRuleHook
	mockGCE.MockAddresses.InsertHook = mock.InsertAddressHook
	mockGCE.MockAlphaAddresses.InsertHook = mock.InsertAlphaAddressHook
	mockGCE.MockAlphaAddresses.X = mock.AddressAttributes{}
	mockGCE.MockAddresses.X = mock.AddressAttributes{}
	return gce, nil
}
