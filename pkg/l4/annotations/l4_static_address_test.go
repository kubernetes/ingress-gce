package annotations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
)

func TestAddressFromAnnotation(t *testing.T) {
	vals := gce.DefaultTestClusterValues()

	ipv4AddressString := "123.0.124.1"
	ipv4AddressNoVersionString := "123.0.111.1"
	ipv6AddressString := "0::1"

	ipv4AddressName := "ipv4-address"
	ipv4AddressNoVersionName := "ipv4-address-no-version"
	ipv6AddressName := "ipv6-address"

	ipv4Address := compute.Address{
		Name:      ipv4AddressName,
		Address:   ipv4AddressString,
		IpVersion: IPv4Version,
	}
	ipv4AddressNoVersion := compute.Address{
		Name:    ipv4AddressNoVersionName,
		Address: ipv4AddressNoVersionString,
	}
	ipv6Address := compute.Address{
		Name:      ipv6AddressName,
		Address:   ipv6AddressString,
		IpVersion: IPv6Version,
	}
	testCases := []struct {
		desc              string
		reservedAddresses []compute.Address
		annotationVal     string
		wantIPv4Address   string
		wantIPv6Address   string
		wantIPv4Name      string
		wantIPv6Name      string
		expectErr         error
	}{
		{
			desc: "Single existing IPv4 address",
			reservedAddresses: []compute.Address{
				ipv4Address,
			},
			annotationVal:   fmt.Sprintf("%s", ipv4Address.Name),
			wantIPv4Address: ipv4Address.Address,
			wantIPv4Name:    ipv4Address.Name,
		},
		{
			desc: "Single existing IPv6 address",
			reservedAddresses: []compute.Address{
				ipv6Address,
			},
			annotationVal:   fmt.Sprintf("%s", ipv6Address.Name),
			wantIPv6Address: ipv6Address.Address,
			wantIPv6Name:    ipv6Address.Name,
		},
		{
			desc: "Single existing IPv4 address with no IpVersion",
			reservedAddresses: []compute.Address{
				ipv4AddressNoVersion,
			},
			annotationVal:   fmt.Sprintf("%s", ipv4AddressNoVersion.Name),
			wantIPv4Address: ipv4AddressNoVersion.Address,
			wantIPv4Name:    ipv4AddressNoVersion.Name,
		},
		{
			desc:              "Many non-existing IPv4 and IPv6 addresses",
			reservedAddresses: []compute.Address{},
			annotationVal:     fmt.Sprintf("%s, %s,%s, %s", ipv4Address.Name, ipv4Address.Name, ipv6Address.Name, ipv4AddressNoVersion.Name),
			wantIPv4Address:   "",
			wantIPv6Address:   "",
		},
		{
			desc:              "Empty annotation",
			reservedAddresses: []compute.Address{},
			annotationVal:     "",
			wantIPv4Address:   "",
			wantIPv6Address:   "",
		},
		{
			desc:              "Whitespace with comas",
			reservedAddresses: []compute.Address{},
			annotationVal:     " , , ,    ,,",
			wantIPv4Address:   "",
			wantIPv6Address:   "",
		},
		{
			desc: "Repeated existing IPv4 addresses",
			reservedAddresses: []compute.Address{
				ipv4Address,
			},
			annotationVal:   fmt.Sprintf("%s, %s", ipv4Address.Name, ipv4Address.Name),
			wantIPv4Address: ipv4Address.Address,
			wantIPv4Name:    ipv4Address.Name,
		},
		{
			desc: "IPv4 and IPv6 addresses",
			reservedAddresses: []compute.Address{
				ipv4Address,
				ipv6Address,
			},
			annotationVal:   fmt.Sprintf("%s, %s", ipv4Address.Name, ipv6Address.Name),
			wantIPv4Address: ipv4Address.Address,
			wantIPv6Address: ipv6Address.Address,
			wantIPv4Name:    ipv4Address.Name,
			wantIPv6Name:    ipv6Address.Name,
		},
		{
			desc:              "Invalid IPv4 literal address instead of name",
			reservedAddresses: []compute.Address{},
			annotationVal:     "1.2.3.4",
			expectErr:         ErrInvalidStaticIPName,
		},
		{
			desc:              "Invalid address name starting with dash",
			reservedAddresses: []compute.Address{},
			annotationVal:     "-my-address",
			expectErr:         ErrInvalidStaticIPName,
		},
		{
			desc:              "Invalid address name exceeding 63 characters",
			reservedAddresses: []compute.Address{},
			annotationVal:     strings.Repeat("a", 64),
			expectErr:         ErrInvalidStaticIPName,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc := newServiceWithStaticAddressAnnotation(tc.annotationVal)

			// Reserve existing addresses.
			fakeGCE := gce.NewFakeGCECloud(vals)
			for i := range tc.reservedAddresses {
				err := fakeGCE.ReserveRegionAddress(&tc.reservedAddresses[i], fakeGCE.Region())
				if err != nil {
					t.Fatalf("fakeGCE.ReserveRegionAddress(%v, %s) returned error %v", tc.reservedAddresses[i], fakeGCE.Region(), err)
				}
			}

			// Verify getting expected IPv4 address from annotation.
			ipv4Addr, ipv4Name, err := FromService(svc).IPv4AddressAnnotation(fakeGCE)
			if tc.expectErr != nil {
				if err == nil {
					t.Fatalf("IPv4AddressAnnotation(..., %s) expected error %v, got nil", tc.annotationVal, tc.expectErr)
				}
				if !errors.Is(err, tc.expectErr) {
					t.Errorf("IPv4AddressAnnotation(..., %s) error %v should wrap %v", tc.annotationVal, err, tc.expectErr)
				}
			} else {
				if err != nil {
					t.Fatalf("IPv4AddressAnnotation(..., %s) returned error %v", tc.annotationVal, err)
				}
				if ipv4Addr != tc.wantIPv4Address {
					t.Errorf("IPv4AddressAnnotation(..., %s) returned %s, not equal to expected = %s", tc.annotationVal, ipv4Addr, tc.wantIPv4Address)
				}
				if ipv4Name != tc.wantIPv4Name {
					t.Errorf("IPv4AddressAnnotation(..., %s) returned %s, not equal to expected = %s", tc.annotationVal, ipv4Name, tc.wantIPv4Name)
				}

				// Verify getting expected IPv6 address from annotation.
				ipv6Addr, ipv6Name, err := FromService(svc).IPv6AddressAnnotation(fakeGCE)
				if err != nil {
					t.Fatalf("IPv6AddressAnnotation(..., %s) returned error %v", tc.annotationVal, err)
				}
				if ipv6Addr != tc.wantIPv6Address {
					t.Errorf("IPv6AddressAnnotation(..., %s) returned %s, not equal to expected = %s", tc.annotationVal, ipv6Addr, tc.wantIPv6Address)
				}
				if ipv6Name != tc.wantIPv6Name {
					t.Errorf("IPv6AddressAnnotation(..., %s) returned %s, not equal to expected = %s", tc.annotationVal, ipv6Name, tc.wantIPv6Name)
				}
			}

		})
	}
}

func newServiceWithStaticAddressAnnotation(annotationVal string) *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				StaticL4AddressesAnnotationKey: annotationVal,
			},
		},
	}
}

func TestIPv6AddressIPCollection(t *testing.T) {
	vals := gce.DefaultTestClusterValues()

	byoipIPv6Address := compute.Address{
		Name:         "byoip-ipv6-address",
		Address:      "0::1",
		IpVersion:    IPv6Version,
		IpCollection: "projects/test/regions/us-central1/publicDelegatedPrefixes/my-collection",
	}
	googleIPv6Address := compute.Address{
		Name:      "google-ipv6-address",
		Address:   "0::2",
		IpVersion: IPv6Version,
	}
	byoipIPv4Address := compute.Address{
		Name:         "byoip-ipv4-address",
		Address:      "123.0.124.1",
		IpVersion:    IPv4Version,
		IpCollection: "projects/test/regions/us-central1/publicDelegatedPrefixes/my-v4-collection",
	}

	testCases := []struct {
		desc             string
		annotationVal    string
		reservedAddress  []compute.Address
		wantIPCollection string
		wantName         string
		getErr           error
		wantErr          bool
	}{
		{
			desc:          "no annotation",
			annotationVal: "",
		},
		{
			desc:          "address does not exist",
			annotationVal: "missing-address",
		},
		{
			desc:             "ipv6 address reserved from ip collection",
			annotationVal:    byoipIPv6Address.Name,
			reservedAddress:  []compute.Address{byoipIPv6Address},
			wantIPCollection: byoipIPv6Address.IpCollection,
			wantName:         byoipIPv6Address.Name,
		},
		{
			desc:            "ipv6 address not reserved from ip collection",
			annotationVal:   googleIPv6Address.Name,
			reservedAddress: []compute.Address{googleIPv6Address},
		},
		{
			desc:            "only ipv4 address reserved from ip collection",
			annotationVal:   byoipIPv4Address.Name,
			reservedAddress: []compute.Address{byoipIPv4Address},
		},
		{
			desc:             "ipv4 and ipv6 addresses, ipv6 reserved from ip collection",
			annotationVal:    fmt.Sprintf("%s,%s", byoipIPv4Address.Name, byoipIPv6Address.Name),
			reservedAddress:  []compute.Address{byoipIPv4Address, byoipIPv6Address},
			wantIPCollection: byoipIPv6Address.IpCollection,
			wantName:         byoipIPv6Address.Name,
		},
		{
			desc:             "missing address is skipped, later ipv6 address reserved from ip collection wins",
			annotationVal:    fmt.Sprintf("missing-address,%s", byoipIPv6Address.Name),
			reservedAddress:  []compute.Address{byoipIPv6Address},
			wantIPCollection: byoipIPv6Address.IpCollection,
			wantName:         byoipIPv6Address.Name,
		},
		{
			desc:            "transient GCE error is returned",
			annotationVal:   byoipIPv6Address.Name,
			reservedAddress: []compute.Address{byoipIPv6Address},
			getErr:          &googleapi.Error{Code: 500, Message: "backend error"},
			wantErr:         true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			svc := newServiceWithStaticAddressAnnotation(tc.annotationVal)
			if tc.annotationVal == "" {
				svc.Annotations = nil
			}

			fakeGCE := gce.NewFakeGCECloud(vals)
			for i := range tc.reservedAddress {
				if err := fakeGCE.ReserveRegionAddress(&tc.reservedAddress[i], fakeGCE.Region()); err != nil {
					t.Fatalf("fakeGCE.ReserveRegionAddress(%v, %s) returned error %v", tc.reservedAddress[i], fakeGCE.Region(), err)
				}
			}

			if tc.getErr != nil {
				fakeGCE.Compute().(*cloud.MockGCE).MockAddresses.GetHook = func(ctx context.Context, key *meta.Key, m *cloud.MockAddresses, options ...cloud.Option) (bool, *compute.Address, error) {
					return true, nil, tc.getErr
				}
			}

			gotIPCollection, gotName, err := FromService(svc).IPv6AddressIPCollection(fakeGCE)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("IPv6AddressIPCollection(..., %q) returned nil error, want error", tc.annotationVal)
				}
				return
			}
			if err != nil {
				t.Fatalf("IPv6AddressIPCollection(..., %q) returned error %v", tc.annotationVal, err)
			}
			if gotIPCollection != tc.wantIPCollection || gotName != tc.wantName {
				t.Errorf("IPv6AddressIPCollection(..., %q) = (%q, %q), want (%q, %q)", tc.annotationVal, gotIPCollection, gotName, tc.wantIPCollection, tc.wantName)
			}
		})
	}
}
