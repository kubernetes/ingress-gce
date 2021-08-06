/*
Copyright 2018 The Kubernetes Authors.

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

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

// maxL4ILBPorts is the maximum number of ports that can be specified in an L4 ILB Forwarding Rule
const maxL4ILBPorts = 5

func (l *L7) checkHttpForwardingRule() (err error) {
	if l.tp == nil {
		return fmt.Errorf("cannot create forwarding rule without proxy")
	}
	name := l.namer.ForwardingRule(namer.HTTPProtocol)
	address, _, err := l.getEffectiveIP()
	if err != nil {
		return err
	}
	fw, err := l.checkForwardingRule(namer.HTTPProtocol, name, l.tp.SelfLink, address)
	if err != nil {
		return err
	}
	l.fw = fw
	return nil
}

func (l *L7) checkHttpsForwardingRule() (err error) {
	if l.tps == nil {
		klog.V(3).Infof("No https target proxy for %v, not created https forwarding rule", l)
		return nil
	}
	name := l.namer.ForwardingRule(namer.HTTPSProtocol)
	address, _, err := l.getEffectiveIP()
	if err != nil {
		return err
	}
	fws, err := l.checkForwardingRule(namer.HTTPSProtocol, name, l.tps.SelfLink, address)
	if err != nil {
		return err
	}
	l.fws = fws
	return nil
}

func (l *L7) checkForwardingRule(protocol namer.NamerProtocol, name, proxyLink, ip string) (existing *composite.ForwardingRule, err error) {
	key, err := l.CreateKey(name)
	if err != nil {
		return nil, err
	}
	version := l.Versions().ForwardingRule
	description, err := l.description()
	if err != nil {
		return nil, err
	}

	isL7ILB := utils.IsGCEL7ILBIngress(l.runtimeInfo.Ingress)
	tr := translator.NewTranslator(isL7ILB, l.namer)
	env := &translator.Env{VIP: ip, Network: l.cloud.NetworkURL(), Subnetwork: l.cloud.SubnetworkURL()}
	fr := tr.ToCompositeForwardingRule(env, protocol, version, proxyLink, description, l.runtimeInfo.StaticIPSubnet)

	existing, _ = composite.GetForwardingRule(l.cloud, key, version)
	if existing != nil && (fr.IPAddress != "" && existing.IPAddress != fr.IPAddress || existing.PortRange != fr.PortRange) {
		klog.Warningf("Recreating forwarding rule %v(%v), so it has %v(%v)",
			existing.IPAddress, existing.PortRange, fr.IPAddress, fr.PortRange)
		if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l.cloud, key, version)); err != nil {
			return nil, err
		}
		existing = nil
		l.recorder.Eventf(l.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", key.Name)
	}
	if existing == nil {
		// This is a special case where exactly one of http or https forwarding rule
		// existed before and the existing forwarding rule uses ingress managed static ip address.
		// In this case, the forwarding rule needs to be created with the same static ip.
		// Note that this is not needed when user specifies a static IP.
		if ip == "" {
			managedStaticIPName := l.namer.ForwardingRule(namer.HTTPProtocol)
			// Get static IP address if ingress has static IP annotation.
			// Note that this Static IP annotation is applied by ingress controller.
			if currentIPName, exists := l.ingress.Annotations[annotations.StaticIPKey]; exists && currentIPName == managedStaticIPName {
				currentIP, _ := l.cloud.GetGlobalAddress(managedStaticIPName)
				if currentIP != nil {
					klog.V(3).Infof("Ingress managed static IP %s(%s) exists, using it to create forwarding rule %s", currentIPName, currentIP.Address, name)
					fr.IPAddress = currentIP.Address
				}
			}
		}
		klog.V(3).Infof("Creating forwarding rule for proxy %q and ip %v:%v", proxyLink, ip, protocol)

		if err = composite.CreateForwardingRule(l.cloud, key, fr); err != nil {
			return nil, err
		}
		l.recorder.Eventf(l.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q created", key.Name)

		key, err = l.CreateKey(name)
		if err != nil {
			return nil, err
		}
		existing, err = composite.GetForwardingRule(l.cloud, key, version)
		if err != nil {
			return nil, err
		}
	}
	// TODO: If the port range and protocol don't match, recreate the rule
	if utils.EqualResourceIDs(existing.Target, proxyLink) {
		klog.V(4).Infof("Forwarding rule %v already exists", existing.Name)
	} else {
		klog.V(3).Infof("Forwarding rule %v has the wrong proxy, setting %v overwriting %v",
			existing.Name, existing.Target, proxyLink)
		key, err := l.CreateKey(existing.Name)
		if err != nil {
			return nil, err
		}
		if err := composite.SetProxyForForwardingRule(l.cloud, key, existing, proxyLink); err != nil {
			return nil, err
		}
	}
	return existing, nil
}

// getEffectiveIP returns a string with the IP to use in the HTTP and HTTPS
// forwarding rules, a boolean indicating if this is an IP the controller
// should manage or not and an error if the specified IP was not found.
func (l *L7) getEffectiveIP() (string, bool, error) {

	// A note on IP management:
	// User specifies a different IP on startup:
	//	- We create a forwarding rule with the given IP.
	//		- If this ip doesn't exist in GCE, an error is thrown which fails ingress creation.
	//	- In the happy case, no static ip is created or deleted by this controller.
	// Controller allocates a staticIP/ephemeralIP, but user changes it:
	//  - We still delete the old static IP, but only when we tear down the
	//	  Ingress in Cleanup(). Till then the static IP stays around, but
	//    the forwarding rules get deleted/created with the new IP.
	//  - There will be a period of downtime as we flip IPs.
	// User specifies the same static IP to 2 Ingresses:
	//  - GCE will throw a 400, and the controller will keep trying to use
	//    the IP in the hope that the user manually resolves the conflict
	//    or deletes/modifies the Ingress.
	// TODO: Handle the last case better.

	if l.runtimeInfo.StaticIPName != "" {
		key, err := l.CreateKey(l.runtimeInfo.StaticIPName)
		if err != nil {
			return "", false, err
		}

		// Existing static IPs allocated to forwarding rules will get orphaned
		// till the Ingress is torn down.
		// TODO(shance): Replace version
		if ip, err := composite.GetAddress(l.cloud, key, meta.VersionGA); err != nil || ip == nil {
			return "", false, fmt.Errorf("the given static IP name %v doesn't translate to an existing static IP.",
				l.runtimeInfo.StaticIPName)
		} else {
			l.runtimeInfo.StaticIPSubnet = ip.Subnetwork
			return ip.Address, false, nil
		}
	}
	if l.ip != nil {
		return l.ip.Address, true, nil
	}
	return "", true, nil
}

// ensureForwardingRule creates a forwarding rule with the given name, if it does not exist. It updates the existing
// forwarding rule if needed.
func (l *L4) ensureForwardingRule(loadBalancerName, bsLink string, options gce.ILBOptions, existingFwdRule *composite.ForwardingRule) (*composite.ForwardingRule, error) {
	key, err := l.CreateKey(loadBalancerName)
	if err != nil {
		return nil, err
	}
	// version used for creating the existing forwarding rule.
	version := meta.VersionGA

	if l.cloud.IsLegacyNetwork() {
		l.recorder.Event(l.Service, v1.EventTypeWarning, "ILBOptionsIgnored", "Internal LoadBalancer options are not supported with Legacy Networks.")
		options = gce.ILBOptions{}
	}
	subnetworkURL := l.cloud.SubnetworkURL()

	// Custom subnet feature is always enabled when running L4 controller.
	// Changes to subnet annotation will be picked up and reflected in the forwarding rule.
	// Removing the annotation will set the forwarding rule to use the default subnet.
	if options.SubnetName != "" {
		subnetKey := *key
		subnetKey.Name = options.SubnetName
		subnetworkURL = cloud.SelfLink(meta.VersionGA, l.cloud.ProjectID(), "subnetworks", &subnetKey)
	}
	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then requestedIP = "".
	ipToUse := ilbIPToUse(l.Service, existingFwdRule, subnetworkURL)
	klog.V(2).Infof("ensureForwardingRule(%v): Using subnet %s for LoadBalancer IP %s", loadBalancerName, options.SubnetName, ipToUse)

	var addrMgr *addressManager
	// If the network is not a legacy network, use the address manager
	if !l.cloud.IsLegacyNetwork() {
		nm := types.NamespacedName{Namespace: l.Service.Namespace, Name: l.Service.Name}.String()
		addrMgr = newAddressManager(l.cloud, nm, l.cloud.Region(), subnetworkURL, loadBalancerName, ipToUse, cloud.SchemeInternal)
		ipToUse, err = addrMgr.HoldAddress()
		if err != nil {
			return nil, err
		}
		klog.V(2).Infof("ensureForwardingRule(%v): reserved IP %q for the forwarding rule", loadBalancerName, ipToUse)
		defer func() {
			// Release the address that was reserved, in all cases. If the forwarding rule was successfully created,
			// the ephemeral IP is not needed anymore. If it was not created, the address should be released to prevent leaks.
			if err := addrMgr.ReleaseAddress(); err != nil {
				klog.Errorf("ensureInternalLoadBalancer: failed to release address reservation, possibly causing an orphan: %v", err)
			}
		}()
	}

	ports, _, protocol := utils.GetPortsAndProtocol(l.Service.Spec.Ports)
	// Create the forwarding rule
	frDesc, err := utils.MakeL4ILBServiceDescription(utils.ServiceKeyFunc(l.Service.Namespace, l.Service.Name), ipToUse,
		version, false)
	if err != nil {
		return nil, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", loadBalancerName,
			err)
	}

	fr := &composite.ForwardingRule{
		Name:                loadBalancerName,
		IPAddress:           ipToUse,
		Ports:               ports,
		IPProtocol:          string(protocol),
		LoadBalancingScheme: string(cloud.SchemeInternal),
		Subnetwork:          subnetworkURL,
		Network:             l.cloud.NetworkURL(),
		Version:             version,
		BackendService:      bsLink,
		AllowGlobalAccess:   options.AllowGlobalAccess,
		Description:         frDesc,
	}
	if len(ports) > maxL4ILBPorts {
		fr.Ports = nil
		fr.AllPorts = true
	}

	if existingFwdRule != nil {
		equal, err := Equal(existingFwdRule, fr)
		if err != nil {
			return existingFwdRule, err
		}
		if equal {
			// nothing to do
			klog.V(2).Infof("ensureForwardingRule: Skipping update of unchanged forwarding rule - %s", fr.Name)
			return existingFwdRule, nil
		}
		frDiff := cmp.Diff(existingFwdRule, fr)
		// If the forwarding rule pointed to a backend service which does not match the controller naming scheme,
		// that resouce could be leaked. It is not being deleted here because that is a user-managed resource.
		klog.V(2).Infof("ensureForwardingRule: forwarding rule changed - Existing - %+v\n, New - %+v\n, Diff(-existing, +new) - %s\n. Deleting existing forwarding rule.", existingFwdRule, fr, frDiff)
		if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l.cloud, key, version)); err != nil {
			return nil, err
		}
		l.recorder.Eventf(l.Service, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", key.Name)
	}
	klog.V(2).Infof("ensureForwardingRule: Creating/Recreating forwarding rule - %s", fr.Name)
	if err = composite.CreateForwardingRule(l.cloud, key, fr); err != nil {
		return nil, err
	}
	return composite.GetForwardingRule(l.cloud, key, fr.Version)
}

func (l *L4) GetForwardingRule(name string, version meta.Version) *composite.ForwardingRule {
	key, err := l.CreateKey(name)
	if err != nil {
		klog.Errorf("Failed to create key for fetching existing forwarding rule %s, err: %v", name, err)
		return nil
	}
	fr, err := composite.GetForwardingRule(l.cloud, key, version)
	if utils.IgnoreHTTPNotFound(err) != nil {
		klog.Errorf("Failed to lookup existing forwarding rule %s, err: %v", name, err)
		return nil
	}
	return fr
}

func (l *L4) deleteForwardingRule(name string, version meta.Version) {
	key, err := l.CreateKey(name)
	if err != nil {
		klog.Errorf("Failed to create key for deleting forwarding rule %s, err: %v", name, err)
		return
	}
	if err := utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l.cloud, key, version)); err != nil {
		klog.Errorf("Failed to delete forwarding rule %s, err: %v", name, err)
	}
}

func Equal(fr1, fr2 *composite.ForwardingRule) (bool, error) {
	id1, err := cloud.ParseResourceURL(fr1.BackendService)
	if err != nil {
		return false, fmt.Errorf("forwardingRulesEqual(): failed to parse backend resource URL from FR, err - %w", err)
	}
	id2, err := cloud.ParseResourceURL(fr2.BackendService)
	if err != nil {
		return false, fmt.Errorf("forwardingRulesEqual(): failed to parse resource URL from FR, err - %w", err)
	}
	return fr1.IPAddress == fr2.IPAddress &&
		fr1.IPProtocol == fr2.IPProtocol &&
		fr1.LoadBalancingScheme == fr2.LoadBalancingScheme &&
		utils.EqualStringSets(fr1.Ports, fr2.Ports) &&
		id1.Equal(id2) &&
		fr1.AllowGlobalAccess == fr2.AllowGlobalAccess &&
		fr1.AllPorts == fr2.AllPorts &&
		fr1.Subnetwork == fr2.Subnetwork, nil
}

// ilbIPToUse determines which IP address needs to be used in the ForwardingRule. If an IP has been
// specified by the user, that is used. If there is an existing ForwardingRule, the ip address from
// that is reused. In case a subnetwork change is requested, the existing ForwardingRule IP is ignored.
func ilbIPToUse(svc *v1.Service, fwdRule *composite.ForwardingRule, requestedSubnet string) string {
	if svc.Spec.LoadBalancerIP != "" {
		return svc.Spec.LoadBalancerIP
	}
	if fwdRule == nil {
		return ""
	}
	if requestedSubnet != fwdRule.Subnetwork {
		// reset ip address since subnet is being changed.
		return ""
	}
	return fwdRule.IPAddress
}
