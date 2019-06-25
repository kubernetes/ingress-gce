/*
Copyright 2015 The Kubernetes Authors.

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
	"context"
	"fmt"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
)

const FakeCertQuota = 15

var testIPManager = testIP{}

type testIP struct {
	start int
}

func (t *testIP) ip() string {
	t.start++
	return fmt.Sprintf("0.0.0.%v", t.start)
}

func InsertGlobalForwardingRuleHook(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockGlobalForwardingRules) (b bool, e error) {
	if obj.IPAddress == "" {
		obj.IPAddress = "0.0.0.1"
	}
	return false, nil
}
