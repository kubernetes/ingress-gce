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
	"net/http"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/context"
	"k8s.io/ingress-gce/pkg/loadbalancers/features"
	"k8s.io/ingress-gce/pkg/utils/common"
	namer_util "k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/legacy-cloud-providers/gce"
)

const (
	testClusterName = "0123456789abcedf"
	kubeSystemUID   = "ksuid123"

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
	v1NamerHelper := l7sPool.v1NamerHelper
	namerFactory := l7sPool.namerFactory
	testCases := []struct {
		desc       string
		ingressLBs []string
		gcpLBs     []string
		expectLBs  []string
	}{
		{
			desc:       "empty",
			ingressLBs: []string{},
			gcpLBs:     []string{},
			expectLBs:  []string{},
		},
		{
			desc:       "remove all lbs",
			ingressLBs: []string{},
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

	otherNamer := namer_util.NewNamer("clusteruid", "fw1")
	otherFeNamerFactory := namer_util.NewFrontendNamerFactory(otherNamer, "")
	otherKeys := []string{
		"a/a",
		"namespace/name",
		generateKey(longName[:10], longName[:10]),
		generateKey(longName[:20], longName[:20]),
	}

	versions := features.GAResourceVersions

	for _, key := range otherKeys {
		namer := otherFeNamerFactory.NamerForLoadBalancer(otherNamer.LoadBalancer(key))
		createFakeLoadbalancer(cloud, namer, versions, defaultScope)
	}

	for _, tc := range testCases {
		for _, key := range tc.gcpLBs {
			namer := namerFactory.NamerForLoadBalancer(v1NamerHelper.LoadBalancer(key))
			createFakeLoadbalancer(cloud, namer, versions, defaultScope)
		}

		err := l7sPool.GCv1(tc.ingressLBs)
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		// check if other LB are not deleted
		for _, key := range otherKeys {
			namer := otherFeNamerFactory.NamerForLoadBalancer(otherNamer.LoadBalancer(key))
			if err := checkFakeLoadBalancer(cloud, namer, versions, defaultScope, true); err != nil {
				t.Errorf("For case %q and key %q, do not expect err: %v", tc.desc, key, err)
			}
		}

		// check if the total number of url maps are expected
		urlMaps, _ := l7sPool.cloud.ListURLMaps()
		if len(urlMaps) != len(tc.expectLBs)+len(otherKeys) {
			t.Errorf("For case %q, expect %d urlmaps, but got %d.", tc.desc, len(tc.expectLBs)+len(otherKeys), len(urlMaps))
		}

		// check if the ones that are expected to be GC is actually GCed.
		expectRemovedLBs := sets.NewString(tc.gcpLBs...).Difference(sets.NewString(tc.expectLBs...)).Difference(sets.NewString(tc.ingressLBs...))
		for _, key := range expectRemovedLBs.List() {
			namer := namerFactory.NamerForLoadBalancer(v1NamerHelper.LoadBalancer(key))
			if err := checkFakeLoadBalancer(cloud, namer, versions, defaultScope, false); err != nil {
				t.Errorf("For case %q and key %q, do not expect err: %v", tc.desc, key, err)
			}
		}

		// check if all expected LBs exists
		for _, key := range tc.expectLBs {
			namer := namerFactory.NamerForLoadBalancer(v1NamerHelper.LoadBalancer(key))
			if err := checkFakeLoadBalancer(cloud, namer, versions, defaultScope, true); err != nil {
				t.Errorf("For case %q and key %q, do not expect err: %v", tc.desc, key, err)
			}
			removeFakeLoadBalancer(cloud, namer, versions, defaultScope)
		}
	}
}

func TestDoNotGCWantedLB(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	type testCase struct {
		desc string
		key  string
	}
	testCases := []testCase{}

	for i := 3; i <= len(longName)*2+1; i++ {
		testCases = append(testCases, testCase{fmt.Sprintf("Ingress Key is %d characters long.", i), generateKeyWithLength(i)})
	}

	numOfExtraUrlMap := 2
	l7sPool.cloud.CreateURLMap(&compute.UrlMap{Name: "random--name1"})
	l7sPool.cloud.CreateURLMap(&compute.UrlMap{Name: "k8s-random-random--1111111111111111"})

	versions := features.GAResourceVersions

	for _, tc := range testCases {
		namer := l7sPool.namerFactory.NamerForLoadBalancer(l7sPool.v1NamerHelper.LoadBalancer(tc.key))
		createFakeLoadbalancer(l7sPool.cloud, namer, versions, defaultScope)
		err := l7sPool.GCv1([]string{tc.key})
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		if err := checkFakeLoadBalancer(l7sPool.cloud, namer, versions, defaultScope, true); err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}
		urlMaps, _ := l7sPool.cloud.ListURLMaps()
		if len(urlMaps) != 1+numOfExtraUrlMap {
			t.Errorf("For case %q, expect %d urlmaps, but got %d.", tc.desc, 1+numOfExtraUrlMap, len(urlMaps))
		}
		removeFakeLoadBalancer(l7sPool.cloud, namer, versions, defaultScope)
	}
}

// This should not leak at all, but verfies existing behavior
// TODO: remove this test after the GC resource leaking is fixed.
func TestGCToLeakLB(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	type testCase struct {
		desc string
		key  string
	}
	testCases := []testCase{}
	for i := 3; i <= len(longName)*2+1; i++ {
		testCases = append(testCases, testCase{fmt.Sprintf("Ingress Key is %d characters long.", i), generateKeyWithLength(i)})
	}

	versions := features.GAResourceVersions

	for _, tc := range testCases {
		namer := l7sPool.namerFactory.NamerForLoadBalancer(l7sPool.v1NamerHelper.LoadBalancer(tc.key))
		createFakeLoadbalancer(l7sPool.cloud, namer, versions, defaultScope)
		err := l7sPool.GCv1([]string{})
		if err != nil {
			t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
		}

		if len(tc.key) >= resourceLeakLimit {
			if err := checkFakeLoadBalancer(l7sPool.cloud, namer, versions, defaultScope, true); err != nil {
				t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
			}
			removeFakeLoadBalancer(l7sPool.cloud, namer, versions, defaultScope)
		} else {
			if err := checkFakeLoadBalancer(l7sPool.cloud, namer, versions, defaultScope, false); err != nil {
				t.Errorf("For case %q, do not expect err: %v", tc.desc, err)
			}
		}
	}
}

// TestV2GC asserts that GC workflow for v2 naming scheme deletes load balancer
// associated with given v2 ingress. This also checks that other v2 ingress
// associated LBs or v1 ingress associated LBs are not deleted.
func TestV2GC(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	cloud := l7sPool.cloud
	feNamerFactory := l7sPool.namerFactory
	testCases := []struct {
		desc            string
		ingressToDelete *networkingv1.Ingress
		addV1Ingresses  bool
		gcpLBs          []*networkingv1.Ingress
		expectedLBs     []*networkingv1.Ingress
	}{
		{
			desc:            "empty",
			ingressToDelete: newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKeyV2),
			addV1Ingresses:  false,
			gcpLBs:          []*networkingv1.Ingress{},
			expectedLBs:     []*networkingv1.Ingress{},
		},
		{
			desc:            "simple case v2 only",
			ingressToDelete: newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:15], longName[:20], common.FinalizerKeyV2),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:15], longName[:20], common.FinalizerKeyV2),
			},
		},
		{
			desc:            "simple case both v1 and v2",
			ingressToDelete: newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:11], common.FinalizerKey),
				newIngressWithFinalizer(longName[:20], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:11], common.FinalizerKey),
				newIngressWithFinalizer(longName[:20], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
		},
		{
			desc:            "63 characters v2 only",
			ingressToDelete: newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:19], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:19], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
			},
		},
		{
			desc:            "63 characters both v1 and v2",
			ingressToDelete: newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
		},
		{
			desc:            "longNameSpace v2 only",
			ingressToDelete: newIngressWithFinalizer(longName, longName[:1], common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName, longName[:1], common.FinalizerKeyV2),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
			},
		},
		{
			desc:            "longNameSpace both v1 and v2",
			ingressToDelete: newIngressWithFinalizer(longName, longName[:1], common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName, longName[:1], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
		},
		{
			desc:            "longName v2 only",
			ingressToDelete: newIngressWithFinalizer(longName[:1], longName, common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:1], longName, common.FinalizerKeyV2),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
			},
		},
		{
			desc:            "longName both v1 and v2",
			ingressToDelete: newIngressWithFinalizer(longName[:1], longName, common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:1], longName, common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
		},
		{
			desc:            "longNameSpace and longName v2 only",
			ingressToDelete: newIngressWithFinalizer(longName, longName, common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName, longName, common.FinalizerKeyV2),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:18], longName[:18], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
			},
		},
		{
			desc:            "longNameSpace and longName both v1 and v2",
			ingressToDelete: newIngressWithFinalizer(longName, longName, common.FinalizerKeyV2),
			gcpLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName, longName, common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
			expectedLBs: []*networkingv1.Ingress{
				newIngressWithFinalizer(longName[:10], longName[:15], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:30], longName[:30], common.FinalizerKeyV2),
				newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
				newIngressWithFinalizer(longName[:18], longName[:19], common.FinalizerKey),
				newIngressWithFinalizer(longName[:21], longName[:27], common.FinalizerKey),
				newIngressWithFinalizer(longName, longName, common.FinalizerKey),
			},
		},
	}

	// Add LBs owned by another cluster.
	otherNamer := namer_util.NewNamer("clusteruid", "fw1")
	otherFeNamerFactory := namer_util.NewFrontendNamerFactory(otherNamer, "ksuid234")
	otherIngresses := []*networkingv1.Ingress{
		// ingresses with v1 naming scheme.
		newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
		newIngressWithFinalizer(longName[:20], longName[:27], common.FinalizerKey),
		// ingresses with v2 naming scheme.
		newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName[:15], longName[:20], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName[:16], longName[:20], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName[:17], longName[:20], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName, longName, common.FinalizerKeyV2),
	}
	versions := features.GAResourceVersions

	for _, ing := range otherIngresses {
		createFakeLoadbalancer(cloud, otherFeNamerFactory.Namer(ing), versions, defaultScope)
	}

	for _, tc := range testCases {
		desc := fmt.Sprintf("%s namespaceLength %d nameLength %d", tc.desc, len(tc.ingressToDelete.Namespace), len(tc.ingressToDelete.Name))
		t.Run(desc, func(t *testing.T) {
			for _, ing := range tc.gcpLBs {
				createFakeLoadbalancer(cloud, feNamerFactory.Namer(ing), versions, defaultScope)
			}

			err := l7sPool.GCv2(tc.ingressToDelete, features.ScopeFromIngress(tc.ingressToDelete))
			if err != nil {
				t.Errorf("l7sPool.GC(%q) = %v, want nil for case %q", common.NamespacedName(tc.ingressToDelete), err, tc.desc)
			}

			// Check if LBs associated with other ingresses are not deleted.
			for _, ing := range otherIngresses {
				if err := checkFakeLoadBalancer(cloud, otherFeNamerFactory.Namer(ing), versions, defaultScope, true); err != nil {
					t.Errorf("checkFakeLoadBalancer(...) = %v, want nil for case %q and ingress %q", err, tc.desc, common.NamespacedName(ing))
				}
			}

			// Check if the total number of url maps is as expected.
			urlMaps, _ := l7sPool.cloud.ListURLMaps()
			if ingCount := len(tc.expectedLBs) + len(otherIngresses); ingCount != len(urlMaps) {
				t.Errorf("len(l7sPool.cloud.ListURLMaps()) = %d, want %d for case %q", len(urlMaps), ingCount, tc.desc)
			}

			// Check if the load balancer associated with ingress to be deleted is actually GCed.
			if err := checkFakeLoadBalancer(cloud, feNamerFactory.Namer(tc.ingressToDelete), versions, defaultScope, false); err != nil {
				t.Errorf("checkFakeLoadBalancer(...) = %v, want nil for case %q and ingress %q", err, tc.desc, common.NamespacedName(tc.ingressToDelete))
			}

			// Check if all expected LBs exist.
			for _, ing := range tc.expectedLBs {
				feNamer := feNamerFactory.Namer(ing)
				if err := checkFakeLoadBalancer(cloud, feNamer, versions, defaultScope, true); err != nil {
					t.Errorf("checkFakeLoadBalancer(...) = %v, want nil for case %q and ingress %q", err, tc.desc, common.NamespacedName(ing))
				}
				removeFakeLoadBalancer(cloud, feNamer, versions, defaultScope)
			}
		})
	}
}

// TestDoNotLeakV2LB asserts that GC workflow for v2 naming scheme does not leak
// GCE resources for different namespaced names.
func TestDoNotLeakV2LB(t *testing.T) {
	t.Parallel()
	pool := newTestLoadBalancerPool()
	l7sPool := pool.(*L7s)
	cloud := l7sPool.cloud
	feNamerFactory := l7sPool.namerFactory

	type testCase struct {
		desc string
		ing  *networkingv1.Ingress
	}
	var testCases []testCase

	for i := 3; i <= len(longName)*2; i++ {
		ing := newIngressWithFinalizer(longName[:i/2], longName[:(i-i/2)], common.FinalizerKeyV2)
		testCases = append(testCases, testCase{fmt.Sprintf("IngressKeyLength: %d", i), ing})
	}

	// Add LBs owned by another cluster.
	otherNamer := namer_util.NewNamer("clusteruid", "fw1")
	otherFeNamerFactory := namer_util.NewFrontendNamerFactory(otherNamer, "ksuid234")
	otherIngresses := []*networkingv1.Ingress{
		// ingresses with v1 naming scheme.
		newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKey),
		newIngressWithFinalizer(longName[:20], longName[:27], common.FinalizerKey),
		// ingresses with v2 naming scheme.
		newIngressWithFinalizer(longName[:10], longName[:10], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName[:15], longName[:20], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName[:16], longName[:20], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName[:17], longName[:20], common.FinalizerKeyV2),
		newIngressWithFinalizer(longName, longName, common.FinalizerKeyV2),
	}
	versions := features.GAResourceVersions

	for _, ing := range otherIngresses {
		createFakeLoadbalancer(cloud, otherFeNamerFactory.Namer(ing), versions, defaultScope)
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			feNamer := feNamerFactory.Namer(tc.ing)
			createFakeLoadbalancer(l7sPool.cloud, feNamer, versions, defaultScope)
			err := l7sPool.GCv2(tc.ing, features.ScopeFromIngress(tc.ing))
			if err != nil {
				t.Errorf("l7sPool.GC(%q) = %v, want nil for case %q", common.NamespacedName(tc.ing), err, tc.desc)
			}

			if err := checkFakeLoadBalancer(l7sPool.cloud, feNamer, versions, defaultScope, false); err != nil {
				t.Errorf("checkFakeLoadBalancer(...) = %v, want nil for case %q", err, tc.desc)
			}
			urlMaps, _ := l7sPool.cloud.ListURLMaps()
			if ingCount := len(otherIngresses); ingCount != len(urlMaps) {
				t.Errorf("len(l7sPool.cloud.ListURLMaps()) = %d, want %d for case %q", len(urlMaps), ingCount, tc.desc)
			}
			removeFakeLoadBalancer(l7sPool.cloud, feNamer, versions, defaultScope)
		})
	}
}

func newTestLoadBalancerPool() LoadBalancerPool {
	namer := namer_util.NewNamer(testClusterName, "fw1")
	fakeGCECloud := gce.NewFakeGCECloud(gce.DefaultTestClusterValues())
	ctx := &context.ControllerContext{}
	return NewLoadBalancerPool(fakeGCECloud, namer, ctx, namer_util.NewFrontendNamerFactory(namer, kubeSystemUID))
}

func createFakeLoadbalancer(cloud *gce.Cloud, namer namer_util.IngressFrontendNamer, versions *features.ResourceVersions, scope meta.KeyType) {
	key, _ := composite.CreateKey(cloud, "", scope)

	key.Name = namer.ForwardingRule(namer_util.HTTPProtocol)
	composite.CreateForwardingRule(cloud, key, &composite.ForwardingRule{Name: key.Name, Version: versions.ForwardingRule})

	key.Name = namer.TargetProxy(namer_util.HTTPProtocol)
	composite.CreateTargetHttpProxy(cloud, key, &composite.TargetHttpProxy{Name: key.Name, Version: versions.TargetHttpProxy})

	key.Name = namer.UrlMap()
	composite.CreateUrlMap(cloud, key, &composite.UrlMap{Name: key.Name, Version: versions.UrlMap})

	cloud.ReserveGlobalAddress(&compute.Address{Name: namer.ForwardingRule(namer_util.HTTPProtocol)})

}

func removeFakeLoadBalancer(cloud *gce.Cloud, namer namer_util.IngressFrontendNamer, versions *features.ResourceVersions, scope meta.KeyType) {
	key, _ := composite.CreateKey(cloud, "", scope)
	key.Name = namer.ForwardingRule(namer_util.HTTPProtocol)
	composite.DeleteForwardingRule(cloud, key, versions.ForwardingRule)

	key.Name = namer.TargetProxy(namer_util.HTTPProtocol)
	composite.DeleteTargetHttpProxy(cloud, key, versions.TargetHttpProxy)

	key.Name = namer.UrlMap()
	composite.DeleteUrlMap(cloud, key, versions.UrlMap)

	cloud.DeleteGlobalAddress(namer.ForwardingRule(namer_util.HTTPProtocol))
}

func checkFakeLoadBalancer(cloud *gce.Cloud, namer namer_util.IngressFrontendNamer, versions *features.ResourceVersions, scope meta.KeyType, expectPresent bool) error {
	if err := checkFakeLoadBalancerWithProtocol(cloud, namer, versions, scope, expectPresent, namer_util.HTTPProtocol); err != nil {
		return err
	}
	key, _ := composite.CreateKey(cloud, namer.UrlMap(), scope)
	_, err := composite.GetUrlMap(cloud, key, versions.UrlMap)
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && (err == nil || err.(*googleapi.Error).Code != http.StatusNotFound) {
		return fmt.Errorf("expect URLMap %q to not present: %v", key, err)
	}
	return nil
}

func checkBothFakeLoadBalancers(cloud *gce.Cloud, namer namer_util.IngressFrontendNamer, versions *features.ResourceVersions, scope meta.KeyType, expectHttp, expectHttps bool) error {
	if err := checkFakeLoadBalancerWithProtocol(cloud, namer, versions, scope, expectHttp, namer_util.HTTPProtocol); err != nil {
		return err
	}
	return checkFakeLoadBalancerWithProtocol(cloud, namer, versions, scope, expectHttps, namer_util.HTTPSProtocol)
}

func checkFakeLoadBalancerWithProtocol(cloud *gce.Cloud, namer namer_util.IngressFrontendNamer, versions *features.ResourceVersions, scope meta.KeyType, expectPresent bool, protocol namer_util.NamerProtocol) error {
	var err error
	key, _ := composite.CreateKey(cloud, namer.ForwardingRule(protocol), scope)

	_, err = composite.GetForwardingRule(cloud, key, versions.ForwardingRule)
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && (err == nil || err.(*googleapi.Error).Code != http.StatusNotFound) {
		return fmt.Errorf("expect %s GlobalForwardingRule %q to not present: %v", protocol, key, err)
	}

	key.Name = namer.TargetProxy(protocol)
	if protocol == namer_util.HTTPProtocol {
		_, err = composite.GetTargetHttpProxy(cloud, key, versions.TargetHttpProxy)
	} else {
		_, err = composite.GetTargetHttpsProxy(cloud, key, versions.TargetHttpsProxy)
	}
	if expectPresent && err != nil {
		return err
	}
	if !expectPresent && (err == nil || err.(*googleapi.Error).Code != http.StatusNotFound) {
		return fmt.Errorf("expect Target%sProxy %q to not present: %v", protocol, key, err)
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

func newIngressWithFinalizer(namespace, name, finalizer string) *networkingv1.Ingress {
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{finalizer},
		},
	}
}
