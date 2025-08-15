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

package address_test

import (
	"net"
	"testing"

	"k8s.io/klog/v2"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/mock"
	"k8s.io/ingress-gce/pkg/address"
	"k8s.io/ingress-gce/pkg/test"
	"k8s.io/ingress-gce/pkg/utils"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
)

const (
	testSvcName = "my-service"
	testSubnet  = "/projects/x/testRegions/us-central1/testSubnetworks/customsub"
	testLBName  = "a111111111111111"
)

var vals = gce.DefaultTestClusterValues()

// TestAddressManagerNoRequestedIP tests the typical case of passing in no requested IP
func TestAddressManagerNoRequestedIP(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := ""

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault, address.IPv4Version, klog.TODO())
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeInternal), cloud.NetworkTierDefault.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerBasic tests the typical case of reserving and unreserving an address.
func TestAddressManagerBasic(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault, address.IPv4Version, klog.TODO())
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

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault, address.IPv4Version, klog.TODO())
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeInternal), cloud.NetworkTierDefault.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerStandardNetworkTier tests the case where the address does not exists
// and checks if created address is in standard network tier
func TestAddressManagerStandardNetworkTier(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeExternal, cloud.NetworkTierStandard, address.IPv4Version, klog.TODO())
	testHoldAddress(t, mgr, svc, testLBName, vals.Region, targetIP, string(cloud.SchemeExternal), cloud.NetworkTierStandard.ToGCEValue())
	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerStandardNetworkTierNotAvailableForInternalAddress that reserved internal IP address will alway be in Premium Network Tier
func TestAddressManagerStandardNetworkTierNotAvailableForInternalAddress(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierStandard, address.IPv4Version, klog.TODO())
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

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault, address.IPv4Version, klog.TODO())
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

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierPremium, address.IPv4Version, klog.TODO())
	ipToUse, ipType, err := mgr.HoldAddress()
	require.NoError(t, err)
	assert.NotEmpty(t, ipToUse)
	assert.Equal(t, address.IPAddrUnmanaged, ipType, "IP Address should not be marked as controller's managed")

	ad, err := svc.GetRegionAddress(testLBName, vals.Region)
	assert.True(t, utils.IsNotFoundError(err))
	require.Nil(t, ad)

	testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
}

// TestAddressManagerExternallyOwnedAndOrphaned tests the case where extrenal address is used
// and obsolete orphaned address is removed
func TestAddressManagerExternallyOwnedAndOrphaned(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)

	// ExternallyOwned IP
	externalName := "my-important-address"
	externalIP := "1.1.1.1"
	addr := &compute.Address{Name: externalName, Address: externalIP, AddressType: string(cloud.SchemeInternal)}
	err = svc.ReserveRegionAddress(addr, vals.Region)
	require.NoError(t, err)

	// Orphaned IP with default LBName name
	orphanedName := testLBName
	orphanedIP := "1.1.1.100"
	orphaned_addr := &compute.Address{Name: orphanedName, Address: orphanedIP, AddressType: string(cloud.SchemeInternal)}
	err = svc.ReserveRegionAddress(orphaned_addr, vals.Region)
	require.NoError(t, err)

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, externalName, externalIP, cloud.SchemeInternal, cloud.NetworkTierPremium, address.IPv4Version, klog.TODO())
	ipToUse, ipType, err := mgr.HoldAddress()
	require.NoError(t, err)
	assert.Equal(t, ipToUse, externalIP)
	assert.Equal(t, address.IPAddrUnmanaged, ipType, "IP Address should not be marked as controller's managed")

	// Orphaned IP should be removed
	_, err = svc.GetRegionAddress(testLBName, vals.Region)
	assert.True(t, utils.IsNotFoundError(err), "Orphaned Address should be removed")

	err = mgr.ReleaseAddress()
	require.NoError(t, err)

	// ExternallyOwned IP should stay untouched
	addr, _ = svc.GetRegionAddress(externalName, vals.Region)
	assert.NotNil(t, addr)

}

// TestAddressManagerNonExisting tests the case where the address can't be reserved
// automatically and was not reserved by the user (external address case).
func TestAddressManagerNonExisting(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeExternal, cloud.NetworkTierPremium, address.IPv4Version, klog.TODO())

	svc.Compute().(*cloud.MockGCE).MockAddresses.InsertHook = test.InsertAddressNotAllocatedToProjectErrorHook
	_, _, err = mgr.HoldAddress()
	require.Error(t, err)
	assert.True(t, utils.IsIPConfigurationError(err))
}

// TestAddressManagerWrongTypeReserved tests the case where the address was reserved by the user but it is of the wrong type.
func TestAddressManagerWrongTypeReserved(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"

	addr := &compute.Address{Name: "my-important-address", Address: targetIP, AddressType: string(cloud.SchemeInternal)}
	err = svc.ReserveRegionAddress(addr, vals.Region)
	if err != nil {
		t.Errorf("svc.ReserveRegionAddress returned err: %v", err)
	}

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeExternal, cloud.NetworkTierPremium, address.IPv4Version, klog.TODO())

	_, _, err = mgr.HoldAddress()
	require.Error(t, err)
	assert.True(t, utils.IsIPConfigurationError(err))
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
	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierPremium, address.IPv4Version, klog.TODO())
	svc.Compute().(*cloud.MockGCE).MockAddresses.InsertHook = test.InsertAddressNetworkErrorHook
	_, _, err = mgr.HoldAddress()
	if err == nil || !utils.IsNetworkTierError(err) {
		t.Fatalf("mgr.HoldAddress() = %v, utils.IsNetworkTierError(err) = %t, want %t", err, utils.IsNetworkTierError(err), true)
	}
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

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", targetIP, cloud.SchemeInternal, cloud.NetworkTierPremium, address.IPv4Version, klog.TODO())
	ad, _, err := mgr.HoldAddress()
	assert.NotNil(t, err) // FIXME
	require.Equal(t, ad, "")
}

// TestAddressManagerBadExternallyOwnedFromAnnotation tests the case where the address exists but isn't
// owned by the controller. However, this address has the wrong type.
func TestAddressManagerBadExternallyOwnedFromAnnotation(t *testing.T) {
	svc, err := fakeGCECloud(vals)
	require.NoError(t, err)
	targetIP := "1.1.1.1"
	addrName := "my-important-address"

	addrExternal := &compute.Address{Name: addrName, Address: targetIP, AddressType: string(cloud.SchemeExternal)}
	err = svc.ReserveRegionAddress(addrExternal, vals.Region)
	require.NoError(t, err)

	addrDefault := &compute.Address{Name: testLBName, Address: "1.1.1.100", AddressType: string(cloud.SchemeInternal)}
	err = svc.ReserveRegionAddress(addrDefault, vals.Region)
	require.NoError(t, err)

	mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, addrName, targetIP, cloud.SchemeInternal, cloud.NetworkTierPremium, address.IPv4Version, klog.TODO())
	ad, _, err := mgr.HoldAddress()
	assert.NotNil(t, err) // FIXME
	require.Equal(t, ad, "")

	addrExternal, _ = svc.GetRegionAddress(addrName, vals.Region)
	assert.NotNil(t, addrExternal, "ExternallyOwned IP should stay untouched")

	_, err = svc.GetRegionAddress(testLBName, vals.Region)
	assert.True(t, utils.IsNotFoundError(err), "Orphaned Address should be removed")
}

// TestAddressManagerIPv6 tests the typical case of reserving and releasing an IPv6 address.
func TestAddressManagerIPv6(t *testing.T) {
	testCases := []struct {
		desc     string
		targetIP string
	}{
		{
			desc:     "Defined IPv6 address",
			targetIP: "1111:2222:3333:4444:5555:0:0:0",
		},
		{
			desc:     "Empty IPv6 address",
			targetIP: "",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc, err := fakeGCECloud(vals)
			if err != nil {
				t.Fatalf("fakeGCECloud(%v) returned error %v", vals, err)
			}

			mgr := address.NewManager(svc, testSvcName, vals.Region, testSubnet, testLBName, "", tc.targetIP, cloud.SchemeInternal, cloud.NetworkTierDefault, address.IPv6Version, klog.TODO())
			testHoldAddress(t, mgr, svc, testLBName, vals.Region, tc.targetIP, string(cloud.SchemeInternal), cloud.NetworkTierDefault.ToGCEValue())
			testReleaseAddress(t, mgr, svc, testLBName, vals.Region)
		})
	}
}

func testHoldAddress(t *testing.T, mgr *address.Manager, svc gce.CloudAddressService, name, region, targetIP, scheme, netTier string) {
	ipToUse, ipType, err := mgr.HoldAddress()
	require.NoError(t, err)
	assert.NotEmpty(t, ipToUse)
	assert.Equal(t, address.IPAddrManaged, ipType, "IP Address should be marked as controller's managed")

	addr, err := svc.GetRegionAddress(name, region)
	require.NoError(t, err)
	if targetIP != "" {
		expectedTargetIP := net.ParseIP(targetIP).String()
		assert.EqualValues(t, expectedTargetIP, addr.Address)
	}
	assert.EqualValues(t, scheme, addr.AddressType)
	assert.EqualValues(t, addr.NetworkTier, netTier)
}

func testReleaseAddress(t *testing.T, mgr *address.Manager, svc gce.CloudAddressService, name, region string) {
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
