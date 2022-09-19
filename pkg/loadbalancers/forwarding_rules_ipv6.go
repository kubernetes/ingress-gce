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

package loadbalancers

import (
	"fmt"
	"time"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

func (l4 *L4) ensureIPv6ForwardingRule(bsLink string, options gce.ILBOptions) (*composite.ForwardingRule, error) {
	expectedIPv6FwdRule, err := l4.buildExpectedIPv6ForwardingRule(bsLink, options)
	if err != nil {
		return nil, fmt.Errorf("l4.buildExpectedIPv6ForwardingRule(%s, %v) returned error %w, want nil", bsLink, options, err)
	}

	start := time.Now()
	klog.V(2).Infof("Ensuring internal ipv6 forwarding rule %s for L4 ILB Service %s/%s, backend service link: %s", expectedIPv6FwdRule.Name, l4.Service.Namespace, l4.Service.Name, bsLink)
	defer func() {
		klog.V(2).Infof("Finished ensuring internal ipv6 forwarding rule %s for L4 ILB Service %s/%s, time taken: %v", expectedIPv6FwdRule.Name, l4.Service.Namespace, l4.Service.Name, time.Since(start))
	}()

	existingIPv6FwdRule, err := l4.forwardingRules.Get(expectedIPv6FwdRule.Name)
	if err != nil {
		return nil, fmt.Errorf("l4.forwardingRules.GetForwardingRule(%s) returned error %w, want nil", expectedIPv6FwdRule.Name, err)
	}

	if existingIPv6FwdRule != nil {
		equal, err := EqualIPv6ForwardingRules(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return existingIPv6FwdRule, err
		}
		if equal {
			klog.V(2).Infof("ensureIPv6ForwardingRule: Skipping update of unchanged ipv6 forwarding rule - %s", expectedIPv6FwdRule.Name)
			return existingIPv6FwdRule, nil
		}
		err = l4.deleteChangedIPv6ForwardingRule(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return nil, err
		}
	}

	klog.V(2).Infof("ensureIPv6ForwardingRule: Creating/Recreating forwarding rule - %s", expectedIPv6FwdRule.Name)
	err = l4.forwardingRules.Create(expectedIPv6FwdRule)
	if err != nil {
		return nil, err
	}

	createdFr, err := l4.forwardingRules.Get(expectedIPv6FwdRule.Name)
	return createdFr, err
}

func (l4 *L4) buildExpectedIPv6ForwardingRule(bsLink string, options gce.ILBOptions) (*composite.ForwardingRule, error) {
	frName := l4.getIPv6FRName()

	frDesc, err := utils.MakeL4IPv6ForwardingRuleDescription(l4.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to compute description for forwarding rule %s, err: %w", frName, err)
	}

	subnetworkURL := l4.cloud.SubnetworkURL()

	if options.SubnetName != "" {
		subnetworkURL, err = l4.getSubnetworkURLByName(options.SubnetName)
		if err != nil {
			return nil, err
		}
	}

	svcPorts := l4.Service.Spec.Ports
	ports := utils.GetPorts(svcPorts)
	protocol := utils.GetProtocol(svcPorts)

	fr := &composite.ForwardingRule{
		Name:                frName,
		Description:         frDesc,
		IPProtocol:          string(protocol),
		Ports:               ports,
		LoadBalancingScheme: string(cloud.SchemeInternal),
		BackendService:      bsLink,
		IpVersion:           "IPV6",
		Network:             l4.cloud.NetworkURL(),
		Subnetwork:          subnetworkURL,
		AllowGlobalAccess:   options.AllowGlobalAccess,
		NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
	}
	if len(ports) > maxL4ILBPorts {
		fr.Ports = nil
		fr.AllPorts = true
	}

	return fr, nil
}

func (l4 *L4) deleteChangedIPv6ForwardingRule(existingFwdRule *composite.ForwardingRule, expectedFwdRule *composite.ForwardingRule) error {
	frDiff := cmp.Diff(existingFwdRule, expectedFwdRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "IPAddress"))
	klog.V(2).Infof("IPv6 forwarding rule changed - Existing - %+v\n, New - %+v\n, Diff(-existing, +new) - %s\n. Deleting existing ipv6 forwarding rule.", existingFwdRule, expectedFwdRule, frDiff)

	err := l4.forwardingRules.Delete(existingFwdRule.Name)
	if err != nil {
		return err
	}
	l4.recorder.Eventf(l4.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", existingFwdRule.Name)
	return nil
}

func (l4netlb *L4NetLB) ensureIPv6ForwardingRule(bsLink string) (*composite.ForwardingRule, error) {
	expectedIPv6FwdRule, err := l4netlb.buildExpectedIPv6ForwardingRule(bsLink)
	if err != nil {
		return nil, fmt.Errorf("l4netlb.buildExpectedIPv6ForwardingRule(%s) returned error %w, want nil", bsLink, err)
	}

	existingIPv6FwdRule, err := l4netlb.forwardingRules.Get(expectedIPv6FwdRule.Name)
	if err != nil {
		return nil, fmt.Errorf("l4netlb.forwardingRules.GetForwardingRule(%s) returned error %w, want nil", expectedIPv6FwdRule.Name, err)
	}

	if existingIPv6FwdRule != nil {
		equal, err := EqualIPv6ForwardingRules(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return existingIPv6FwdRule, err
		}
		if equal {
			klog.V(2).Infof("ensureIPv6ForwardingRule: Skipping update of unchanged ipv6 forwarding rule - %s", expectedIPv6FwdRule.Name)
			return existingIPv6FwdRule, nil
		}
		err = l4netlb.deleteChangedIPv6ForwardingRule(existingIPv6FwdRule, expectedIPv6FwdRule)
		if err != nil {
			return nil, err
		}
	}
	klog.V(2).Infof("ensureIPv6ForwardingRule: Creating/Recreating forwarding rule - %s", expectedIPv6FwdRule.Name)
	err = l4netlb.forwardingRules.Create(expectedIPv6FwdRule)
	if err != nil {
		return nil, err
	}

	createdFr, err := l4netlb.forwardingRules.Get(expectedIPv6FwdRule.Name)
	return createdFr, err
}

func (l4netlb *L4NetLB) buildExpectedIPv6ForwardingRule(bsLink string) (*composite.ForwardingRule, error) {
	frName := l4netlb.ipv6FRName()

	frDesc, err := utils.MakeL4IPv6ForwardingRuleDescription(l4netlb.Service)
	if err != nil {
		return nil, fmt.Errorf("failed to compute description for forwarding rule %s, err: %w", frName, err)
	}

	svcPorts := l4netlb.Service.Spec.Ports
	portRange, protocol := utils.MinMaxPortRangeAndProtocol(svcPorts)

	fr := &composite.ForwardingRule{
		Name:                frName,
		Description:         frDesc,
		IPProtocol:          protocol,
		PortRange:           portRange,
		LoadBalancingScheme: string(cloud.SchemeExternal),
		BackendService:      bsLink,
		IpVersion:           "IPV6",
		NetworkTier:         cloud.NetworkTierPremium.ToGCEValue(),
		Subnetwork:          l4netlb.cloud.SubnetworkURL(),
	}

	return fr, nil
}

func (l4netlb *L4NetLB) deleteChangedIPv6ForwardingRule(existingFwdRule *composite.ForwardingRule, expectedFwdRule *composite.ForwardingRule) error {
	frDiff := cmp.Diff(existingFwdRule, expectedFwdRule, cmpopts.IgnoreFields(composite.ForwardingRule{}, "IPAddress"))
	klog.V(2).Infof("IPv6 External forwarding rule changed - Existing - %+v\n, New - %+v\n, Diff(-existing, +new) - %s\n. Deleting existing ipv6 forwarding rule.", existingFwdRule, expectedFwdRule, frDiff)

	err := l4netlb.forwardingRules.Delete(existingFwdRule.Name)
	if err != nil {
		return err
	}
	l4netlb.recorder.Eventf(l4netlb.Service, corev1.EventTypeNormal, events.SyncIngress, "External ForwardingRule %q deleted", existingFwdRule.Name)
	return nil
}

func EqualIPv6ForwardingRules(fr1, fr2 *composite.ForwardingRule) (bool, error) {
	id1, err := cloud.ParseResourceURL(fr1.BackendService)
	if err != nil {
		return false, fmt.Errorf("EqualIPv6ForwardingRules(): failed to parse backend resource URL from FR, err - %w", err)
	}
	id2, err := cloud.ParseResourceURL(fr2.BackendService)
	if err != nil {
		return false, fmt.Errorf("EqualIPv6ForwardingRules(): failed to parse resource URL from FR, err - %w", err)
	}
	return fr1.IPProtocol == fr2.IPProtocol &&
		fr1.LoadBalancingScheme == fr2.LoadBalancingScheme &&
		utils.EqualStringSets(fr1.Ports, fr2.Ports) &&
		id1.Equal(id2) &&
		fr1.AllowGlobalAccess == fr2.AllowGlobalAccess &&
		fr1.AllPorts == fr2.AllPorts &&
		fr1.Subnetwork == fr2.Subnetwork &&
		fr1.NetworkTier == fr2.NetworkTier, nil
}
