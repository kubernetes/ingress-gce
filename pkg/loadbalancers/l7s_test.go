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
	"fmt"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/kubernetes/pkg/cloudprovider/providers/gce"
)

const (
	testClusterName = "0123456789abcedf"

	// resourceLeakLimit is the limit when ingress namespace and name are too long and ingress
	// GC will leak the LB resources because the cluster uid got truncated.
	resourceLeakLimit = 49
)

var longName = strings.Repeat("0123456789", 10)

func TestGC(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	cloud := l7sPool.cloud
	testCases := []struct {
		desc       string
		ingressLBs []string
		regional   []bool
		gcpLBs     []string
		expectLBs  []string
	}{
		{
			desc:       "empty",
			ingressLBs: []string{},
			regional:   []bool{},
			gcpLBs:     []string{},
			expectLBs:  []string{},
		},
		{
			desc:       "remove all lbs",
			ingressLBs: []string{},
			regional:   []bool{},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
			},
			expectLBs: []string{},
		},
		{
			// resource leakage is due to the GC logic is doing naming matching.
			// if the name is too long, it truncates the suffix which contains the cluster uid
			// if cluster uid section of a name is too short or completely truncated,
			// ingress controller will leak the resource instead.
			desc:       "remove all lbs but with leaks",
			ingressLBs: []string{},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
				generateKey(longName[:21], longName[:27]),
				generateKey(longName[:22], longName[:27]),
				generateKey(longName[:50], longName[:30]),
			},
			expectLBs: []string{
				generateKey(longName[:21], longName[:27]),
				generateKey(longName[:22], longName[:27]),
				generateKey(longName[:50], longName[:30]),
			},
		},
		{
			desc: "no LB exists",
			ingressLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
				generateKey(longName[:50], longName[:30]),
			},
			regional:  []bool{false, false, false, false},
			gcpLBs:    []string{},
			expectLBs: []string{},
		},
		{
			desc: "no LB get GCed",
			ingressLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:30]),
				generateKey(longName, longName),
			},
			regional: []bool{false, false, false, false},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:30]),
				generateKey(longName, longName),
			},
			expectLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:30]),
				generateKey(longName, longName),
			},
		},
		{
			desc: "some LB get GCed",
			ingressLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName, longName),
			},
			regional: []bool{false, false},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
				generateKey(longName, longName),
			},
			expectLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName, longName),
			},
		},
		{
			desc: "some LB get GCed and some leaked",
			ingressLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName, longName),
			},
			regional: []bool{false, false},
			gcpLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:20], longName[:20]),
				generateKey(longName[:20], longName[:27]),
				generateKey(longName[:21], longName[:27]),
				generateKey(longName, longName),
			},
			expectLBs: []string{
				generateKey(longName[:10], longName[:10]),
				generateKey(longName[:21], longName[:27]),
				generateKey(longName, longName),
			},
		},
	}

	otherNamer := utils.NewNamer("clusteruid", "fw1")
	otherKeys := []string{
		"a/a",
		"namespace/name",
		generateKey(longName[:10], longName[:10]),
		generateKey(longName[:20], longName[:20]),
	}

	for _, key := range otherKeys {
		createFakeLoadbalancer(cloud, otherNamer, key)
	}

	for _, tc := range testCases {
		for _, key := range tc.gcpLBs {
			createFakeLoadbalancer(l7sPool.cloud, l7sPool.namer, key)
		}

		err := l7sPool.GC(tc.ingressLBs, tc.regional)
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		// check if other LB are not deleted
		for _, key := range otherKeys {
			if err := checkFakeLoadBalancer(l7sPool.cloud, otherNamer, key, true); err != nil {
				t.Errorf("For case %q and key %q, do not expect err: %v", tc.desc, key, err)
			}
		}

		// check if the total number of url maps are expected
		urlMaps, _ := l7sPool.cloud.ListUrlMaps(meta.VersionGA, meta.GlobalKey(""))
		if len(urlMaps) != len(tc.expectLBs)+len(otherKeys) {
			t.Errorf("For case %q, expect %d urlmaps, but got %d.", tc.desc, len(tc.expectLBs)+len(otherKeys), len(urlMaps))
		}

		// check if the ones that are expected to be GC is actually GCed.
		expectRemovedLBs := sets.NewString(tc.gcpLBs...).Difference(sets.NewString(tc.expectLBs...)).Difference(sets.NewString(tc.ingressLBs...))
		for _, key := range expectRemovedLBs.List() {
			if err := checkFakeLoadBalancer(l7sPool.cloud, l7sPool.namer, key, false); err != nil {
				t.Errorf("For case %q and key %q, do not expect err: %v", tc.desc, key, err)
			}
		}

		// check if all expected LBs exists
		for _, key := range tc.expectLBs {
			if err := checkFakeLoadBalancer(l7sPool.cloud, l7sPool.namer, key, true); err != nil {
				t.Errorf("For case %q and key %q, do not expect err: %v", tc.desc, key, err)
			}
			removeFakeLoadBalancer(l7sPool.cloud, l7sPool.namer, key)
		}
	}
}

func TestDoNotGCWantedLB(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	namer := l7sPool.namer
	type testCase struct {
		desc string
		key  string
	}
	testCases := []testCase{}

	for i := 3; i <= len(longName)*2+1; i++ {
		testCases = append(testCases, testCase{fmt.Sprintf("Ingress Key is %d characters long.", i), generateKeyWithLength(i)})
	}

	numOfExtraUrlMap := 2
	l7sPool.cloud.CreateUrlMap(&composite.UrlMap{Name: "random--name1"}, meta.GlobalKey("random-name1"))
	l7sPool.cloud.CreateUrlMap(&composite.UrlMap{Name: "k8s-random-random--1111111111111111"},
		meta.GlobalKey("k8s-random-random--1111111111111111"))

	for _, tc := range testCases {
		createFakeLoadbalancer(l7sPool.cloud, namer, tc.key)
		err := l7sPool.GC([]string{tc.key}, []bool{false})
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		if err := checkFakeLoadBalancer(l7sPool.cloud, namer, tc.key, true); err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}
		urlMaps, _ := l7sPool.cloud.ListUrlMaps(meta.VersionGA, meta.GlobalKey(""))
		if len(urlMaps) != 1+numOfExtraUrlMap {
			t.Errorf("For case %q, expect %d urlmaps, but got %d.", tc.desc, 1+numOfExtraUrlMap, len(urlMaps))
		}
		removeFakeLoadBalancer(l7sPool.cloud, namer, tc.key)
	}
}

// This should not leak at all, but verfies existing behavior
// TODO: remove this test after the GC resource leaking is fixed.
func TestGCToLeakLB(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	namer := l7sPool.namer
	type testCase struct {
		desc string
		key  string
	}
	testCases := []testCase{}
	for i := 3; i <= len(longName)*2+1; i++ {
		testCases = append(testCases, testCase{fmt.Sprintf("Ingress Key is %d characters long.", i), generateKeyWithLength(i)})
	}

	for _, tc := range testCases {
		createFakeLoadbalancer(l7sPool.cloud, namer, tc.key)
		err := l7sPool.GC([]string{}, []bool{})
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		if len(tc.key) >= resourceLeakLimit {
			if err := checkFakeLoadBalancer(l7sPool.cloud, namer, tc.key, true); err != nil {
				t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
			}
			removeFakeLoadBalancer(l7sPool.cloud, namer, tc.key)
		} else {
			if err := checkFakeLoadBalancer(l7sPool.cloud, namer, tc.key, false); err != nil {
				t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
			}
		}
	}
}

func newTestLoadBalancerPool() LoadBalancerPool {
	namer := utils.NewNamer(testClusterName, "fw1")
	fakeGCECloud := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	compositeCloud := composite.NewCloud(fakeGCECloud)
	ctx := &context.ControllerContext{}
	return NewLoadBalancerPool(compositeCloud, namer, ctx)
}

func createFakeLoadbalancer(cloud LoadBalancers, namer *utils.Namer, key string) {
	lbName := namer.LoadBalancer(key)
	fwName := namer.ForwardingRule(lbName, utils.HTTPProtocol)
	tpName := namer.TargetProxy(lbName, utils.HTTPProtocol)
	umName := namer.UrlMap(lbName)
	cloud.CreateForwardingRule(&composite.ForwardingRule{Name: fwName}, meta.GlobalKey(fwName))
	cloud.CreateTargetHttpProxy(&composite.TargetHttpProxy{Name: tpName}, meta.GlobalKey(tpName))
	cloud.CreateUrlMap(&composite.UrlMap{Name: umName}, meta.GlobalKey(umName))
	cloud.ReserveGlobalAddress(&compute.Address{Name: namer.ForwardingRule(lbName, utils.HTTPProtocol)})
}

func removeFakeLoadBalancer(cloud LoadBalancers, namer *utils.Namer, key string) {
	lbName := namer.LoadBalancer(key)
	cloud.DeleteForwardingRule(meta.VersionGA, meta.GlobalKey(namer.ForwardingRule(lbName, utils.HTTPProtocol)))
	cloud.DeleteTargetHttpProxy(meta.VersionGA, meta.GlobalKey(namer.TargetProxy(lbName, utils.HTTPProtocol)))
	cloud.DeleteUrlMap(meta.VersionGA, meta.GlobalKey(namer.UrlMap(lbName)))
	cloud.DeleteGlobalAddress(namer.ForwardingRule(lbName, utils.HTTPProtocol))
}

func checkFakeLoadBalancer(cloud LoadBalancers, namer *utils.Namer, key string, expectPresent bool) error {
	var err error
	lbName := namer.LoadBalancer(key)
	_, err = cloud.GetForwardingRule(meta.VersionGA, meta.GlobalKey(namer.ForwardingRule(lbName, utils.HTTPProtocol)))
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && err.(*googleapi.Error).Code != http.StatusNotFound {
		return fmt.Errorf("expect GlobalForwardingRule %q to not present: %v", key, err)
	}
	_, err = cloud.GetTargetHttpProxy(meta.VersionGA, meta.GlobalKey(namer.TargetProxy(lbName, utils.HTTPProtocol)))
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && err.(*googleapi.Error).Code != http.StatusNotFound {
		return fmt.Errorf("expect TargetHTTPProxy %q to not present: %v", key, err)
	}
	_, err = cloud.GetUrlMap(meta.VersionGA, meta.GlobalKey(namer.UrlMap(lbName)))
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && err.(*googleapi.Error).Code != http.StatusNotFound {
		return fmt.Errorf("expect URLMap %q to not present: %v", key, err)
	}
	return nil
}

func generateKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func generateKeyWithLength(length int) string {
	if length < 3 {
		length = 3
	}
	if length > 201 {
		length = 201
	}
	length = length - 1
	return fmt.Sprintf("%s/%s", longName[:length/2], longName[:length-length/2])
}
