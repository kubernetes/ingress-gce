/*
Copyright 2021 The Kubernetes Authors.

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

package main

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/e2e"
	"k8s.io/legacy-cloud-providers/gce"
)

func TestPSCLifecycle(t *testing.T) {
	t.Parallel()

	Framework.RunWithSandbox("PSC Lifecycle", t, func(t *testing.T, s *e2e.Sandbox) {
		svcName := fmt.Sprintf("ilb-service-e2e-%x", s.RandInt)
		saName := fmt.Sprintf("service-attachment-e2e-%x", s.RandInt)

		// PSC requires subnet to have purpose PRIVATE_SERVICE_CONNECT
		err := e2e.CreateSubnet(s, e2e.PSCSubnetName, e2e.PSCSubnetPurpose)
		defer e2e.DeleteSubnet(s, e2e.PSCSubnetName)
		if err != nil {
			// Subnet Creation could fail because of a leaked subnet from a previous run.
			// Instead of failing the test, attempt to reuse subnet.
			t.Logf("error creating subnet %s: %q", e2e.PSCSubnetName, err)
		} else {
			t.Logf("created subnet for PSC")
		}

		l4ILBAnnotation := map[string]string{gce.ServiceAnnotationLoadBalancerType: "Internal"}
		if _, err := e2e.EnsureEchoService(s, svcName, l4ILBAnnotation, v1.ServiceTypeLoadBalancer, 1); err != nil {
			t.Fatalf("error ensuring echo service %s/%s: %q", s.Namespace, svcName, err)
		}
		t.Logf("ensured echo service %s/%s", s.Namespace, svcName)

		if _, err := e2e.EnsureServiceAttachment(s, saName, svcName, e2e.PSCSubnetName); err != nil {
			t.Fatalf("error creating service attachment cr %s/%s: %q", s.Namespace, saName, err)
		}

		gceSAURL, err := e2e.WaitForServiceAttachment(s, saName)
		if err != nil {
			t.Fatalf("failed waiting for service attachment: %q", err)
		}

		if err := e2e.DeleteServiceAttachment(s, saName); err != nil {
			t.Fatalf("failed deleting service attachment %s/%s: %q", s.Namespace, saName, err)
		}

		if err := e2e.WaitForServiceAttachmentDeletion(s, saName, gceSAURL); err != nil {
			t.Fatalf("failed while waiting for service attachment deletion")
		}
	})
}
