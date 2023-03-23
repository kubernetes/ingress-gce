/*
Copyright 2022 The Kubernetes Authors.
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

package firewalls

import (
	compute "google.golang.org/api/compute/v1"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite/metrics"
)

// firewallAdapter is a temporary shim to consolidate accesses to
// Cloud and push them outside of this package.
type firewallAdapter struct {
	gc *gce.Cloud
}

// NewFirewallAdapter takes a Cloud and construct a firewallAdapter
func NewFirewallAdapter(g *gce.Cloud) *firewallAdapter {
	return &firewallAdapter{
		gc: g,
	}
}

// GetFirewall returns the Firewall by name.
func (fa *firewallAdapter) GetFirewall(name string) (*compute.Firewall, error) {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("firewall", "get", "<n/a>", "<n/a>", "<n/a>")
	v, err := fa.gc.Compute().Firewalls().Get(ctx, meta.GlobalKey(name))
	return v, mc.Observe(err)
}

// CreateFirewall creates the passed firewall
func (fa *firewallAdapter) CreateFirewall(f *compute.Firewall) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("firewall", "create", "<n/a>", "<n/a>", "<n/a>")
	return mc.Observe(fa.gc.Compute().Firewalls().Insert(ctx, meta.GlobalKey(f.Name), f))
}

// DeleteFirewall deletes the given firewall rule.
func (fa *firewallAdapter) DeleteFirewall(name string) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("firewall", "delete", "<n/a>", "<n/a>", "<n/a>")
	return mc.Observe(fa.gc.Compute().Firewalls().Delete(ctx, meta.GlobalKey(name)))
}

// PatchFirewall applies the given firewall as a patch to an existing service.
func (fa *firewallAdapter) PatchFirewall(f *compute.Firewall) error {
	ctx, cancel := cloud.ContextWithCallTimeout()
	defer cancel()
	mc := metrics.NewMetricContext("firewall", "patch", "<n/a>", "<n/a>", "<n/a>")
	return mc.Observe(fa.gc.Compute().Firewalls().Patch(ctx, meta.GlobalKey(f.Name), f))
}
