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

package firewalls

import (
	"encoding/json"
	"fmt"

	compute "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/test"
)

type fakeFirewallsProvider struct {
	fw               map[string]*compute.Firewall
	networkProjectID string
	networkURL       string
	onXPN            bool
	fwReadOnly       bool
	getFirewallHook  func(name string) (*compute.Firewall, error)
}

// NewFakeFirewallsProvider creates a fake for firewall rules.
func NewFakeFirewallsProvider(onXPN bool, fwReadOnly bool) *fakeFirewallsProvider {
	return &fakeFirewallsProvider{
		fw:               make(map[string]*compute.Firewall),
		networkProjectID: "test-network-project",
		networkURL:       "/path/to/my-network",
		onXPN:            onXPN,
		fwReadOnly:       fwReadOnly,
	}
}

func (ff *fakeFirewallsProvider) GetFirewall(name string) (*compute.Firewall, error) {
	if ff.getFirewallHook != nil {
		return ff.getFirewallHook(name)
	}

	rule, exists := ff.fw[name]
	if exists {
		return rule, nil
	}
	return nil, test.FakeGoogleAPINotFoundErr()
}

func (ff *fakeFirewallsProvider) doCreateFirewall(f *compute.Firewall) error {
	if _, exists := ff.fw[f.Name]; exists {
		return fmt.Errorf("firewall rule %v already exists", f.Name)
	}
	cf, err := copyFirewall(f)
	if err != nil {
		return err
	}
	ff.fw[f.Name] = cf
	return nil
}

func (ff *fakeFirewallsProvider) CreateFirewall(f *compute.Firewall) error {
	if ff.fwReadOnly {
		return test.FakeGoogleAPIForbiddenErr()
	}

	return ff.doCreateFirewall(f)
}

func (ff *fakeFirewallsProvider) doDeleteFirewall(name string) error {
	// We need the full name for the same reason as CreateFirewall.
	_, exists := ff.fw[name]
	if !exists {
		return test.FakeGoogleAPINotFoundErr()
	}

	delete(ff.fw, name)
	return nil
}

func (ff *fakeFirewallsProvider) DeleteFirewall(name string) error {
	if ff.fwReadOnly {
		return test.FakeGoogleAPIForbiddenErr()
	}

	return ff.doDeleteFirewall(name)
}

func (ff *fakeFirewallsProvider) doUpdateFirewall(f *compute.Firewall) error {
	// We need the full name for the same reason as CreateFirewall.
	_, exists := ff.fw[f.Name]
	if !exists {
		return fmt.Errorf("update failed for rule %v, srcRange %v ports %+v, rule not found", f.Name, f.SourceRanges, f.Allowed)
	}

	cf, err := copyFirewall(f)
	if err != nil {
		return err
	}
	ff.fw[f.Name] = cf
	return nil
}

func (ff *fakeFirewallsProvider) UpdateFirewall(f *compute.Firewall) error {
	if ff.fwReadOnly {
		return test.FakeGoogleAPIForbiddenErr()
	}

	return ff.doUpdateFirewall(f)
}

func (ff *fakeFirewallsProvider) NetworkProjectID() string {
	return ff.networkProjectID
}

func (ff *fakeFirewallsProvider) NetworkURL() string {
	return ff.networkURL
}

func (ff *fakeFirewallsProvider) OnXPN() bool {
	return ff.onXPN
}

func (ff *fakeFirewallsProvider) GetNodeTags(nodeNames []string) ([]string, error) {
	return nodeNames, nil
}

func copyFirewall(f *compute.Firewall) (*compute.Firewall, error) {
	enc, err := f.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var firewall compute.Firewall
	if err := json.Unmarshal(enc, &firewall); err != nil {
		return nil, err
	}
	return &firewall, nil
}
