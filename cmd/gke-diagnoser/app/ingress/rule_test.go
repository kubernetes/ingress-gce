/*
Copyright 2023 The Kubernetes Authors.

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

package ingress

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/ingress-gce/cmd/gke-diagnoser/app/report"
)

func TestCheckServiceExistence(t *testing.T) {
	ns := "namespace1"
	client := fake.NewSimpleClientset()
	client.CoreV1().Services(ns).Create(context.TODO(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "service-1",
		},
	}, metav1.CreateOptions{})

	for _, tc := range []struct {
		desc      string
		namespace string
		name      string
		expect    string
	}{
		{
			desc:   "empty input",
			expect: report.Failed,
		},
		{
			desc:      "correct namespace and correct name",
			namespace: "namespace1",
			name:      "service-1",
			expect:    report.Passed,
		},
		{
			desc:      "correct namespace and wrong name",
			namespace: "namespace1",
			name:      "service-2",
			expect:    report.Failed,
		},
		{
			desc:      "wrong namespace and correct name",
			namespace: "namespace2",
			name:      "service-1",
			expect:    report.Failed,
		},
	} {
		_, res, _ := CheckServiceExistence(tc.namespace, tc.name, client)
		if res != tc.expect {
			t.Errorf("For test case %q, expect check result = %s, but got %s", tc.desc, tc.expect, res)
		}
	}
}
