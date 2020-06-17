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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
)

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

	isL7ILB := flags.F.EnableL7Ilb && utils.IsGCEL7ILBIngress(l.runtimeInfo.Ingress)
	tr := translator.NewTranslator(isL7ILB, l.namer)
	env := &translator.Env{VIP: ip, Network: l.cloud.NetworkURL(), Subnetwork: l.cloud.SubnetworkURL()}
	fr := tr.ToCompositeForwardingRule(env, protocol, version, proxyLink, description)

	existing, _ = composite.GetForwardingRule(l.cloud, key, version)
	if existing != nil && (fr.IPAddress != "" && existing.IPAddress != fr.IPAddress || existing.PortRange != fr.PortRange) {
		klog.Warningf("Recreating forwarding rule %v(%v), so it has %v(%v)",
			existing.IPAddress, existing.PortRange, fr.IPAddress, fr.PortRange)
		if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l.cloud, key, version)); err != nil {
			return nil, err
		}
		existing = nil
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
					ip = currentIP.Address
				}
			}
		}
		klog.V(3).Infof("Creating forwarding rule for proxy %q and ip %v:%v", proxyLink, ip, protocol)

		if err = composite.CreateForwardingRule(l.cloud, key, fr); err != nil {
			return nil, err
		}
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
		// Existing static IPs allocated to forwarding rules will get orphaned
		// till the Ingress is torn down.
		if ip, err := l.cloud.GetGlobalAddress(l.runtimeInfo.StaticIPName); err != nil || ip == nil {
			return "", false, fmt.Errorf("the given static IP name %v doesn't translate to an existing global static IP.",
				l.runtimeInfo.StaticIPName)
		} else {
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
func (l *L4) ensureForwardingRule(loadBalancerName, bsLink string, options gce.ILBOptions) (*composite.ForwardingRule, error) {
	key, err := l.CreateKey(loadBalancerName)
	if err != nil {
		return nil, err
	}
	desc := utils.L4ILBResourceDescription{}
	// version used for creating the existing forwarding rule.
	existingVersion := meta.VersionGA
	// version to use for the new forwarding rule
	newVersion := getAPIVersion(options)

	// Get the GA version forwarding rule, use the description to identify the version it was created with.
	existingFwdRule, err := composite.GetForwardingRule(l.cloud, key, meta.VersionGA)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return nil, err
	}
	if existingFwdRule != nil {
		if err = desc.Unmarshal(existingFwdRule.Description); err != nil {
			klog.Warningf("Failed to lookup forwarding rule version from description, err %v. Using GA Version.", err)
		} else {
			existingVersion = desc.APIVersion
		}
	}
	// Fetch the right forwarding rule in case it is not using GA
	if existingVersion != meta.VersionGA {
		existingFwdRule, err = composite.GetForwardingRule(l.cloud, key, existingVersion)
		if utils.IgnoreHTTPNotFound(err) != nil {
			klog.Errorf("Failed to lookup forwarding rule '%s' at version - %s, err %v", key.Name, existingVersion, err)
			return nil, err
		}
	}

	if l.cloud.IsLegacyNetwork() {
		l.recorder.Event(l.Service, v1.EventTypeWarning, "ILBOptionsIgnored", "Internal LoadBalancer options are not supported with Legacy Networks.")
		options = gce.ILBOptions{}
	}
	subnetworkURL := l.cloud.SubnetworkURL()

	if !l.cloud.AlphaFeatureGate.Enabled(gce.AlphaFeatureILBCustomSubnet) {
		if options.SubnetName != "" {
			l.recorder.Event(l.Service, v1.EventTypeWarning, "ILBCustomSubnetOptionIgnored", "Internal LoadBalancer CustomSubnet options ignored as the feature gate is disabled.")
			options.SubnetName = ""
		}
	}
	if l.cloud.AlphaFeatureGate.Enabled(gce.AlphaFeatureILBCustomSubnet) {
		// If this feature is enabled, changes to subnet annotation will be
		// picked up and reflected in the forwarding rule.
		// Removing the annotation will set the forwarding rule to use the default subnet.
		if options.SubnetName != "" {
			subnetKey := *key
			subnetKey.Name = options.SubnetName
			subnetworkURL = cloud.SelfLink(meta.VersionGA, l.cloud.ProjectID(), "subnetworks", &subnetKey)
		}
	} else {
		// TODO(84885) remove this once ILBCustomSubnet goes beta.
		if existingFwdRule != nil && existingFwdRule.Subnetwork != "" {
			// If the ILB already exists, continue using the subnet that it's already using.
			// This is to support existing ILBs that were setup using the wrong subnet - https://github.com/kubernetes/kubernetes/pull/57861
			subnetworkURL = existingFwdRule.Subnetwork
		}
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
	}

	ports, _, protocol := utils.GetPortsAndProtocol(l.Service.Spec.Ports)
	// Create the forwarding rule
	frDesc, err := utils.MakeL4ILBServiceDescription(utils.ServiceKeyFunc(l.Service.Namespace, l.Service.Name), ipToUse,
		newVersion)
	if err != nil {
		return nil, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %v", loadBalancerName,
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
		Version:             newVersion,
		BackendService:      bsLink,
		AllowGlobalAccess:   options.AllowGlobalAccess,
		Description:         frDesc,
	}

	if existingFwdRule != nil {
		if Equal(existingFwdRule, fr) {
			// nothing to do
			klog.V(2).Infof("ensureForwardingRule: Skipping update of unchanged forwarding rule - %s", fr.Name)
			return existingFwdRule, nil
		} else {
			klog.V(2).Infof("ensureForwardingRule: Deleting existing forwarding rule - %s, will be recreated", fr.Name)
			if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l.cloud, key, existingVersion)); err != nil {
				return nil, err
			}
		}
	}
	klog.V(2).Infof("ensureForwardingRule: Recreating forwarding rule - %s", fr.Name)
	if err = composite.CreateForwardingRule(l.cloud, key, fr); err != nil {
		return nil, err
	}

	return composite.GetForwardingRule(l.cloud, key, fr.Version)
}

func Equal(fr1, fr2 *composite.ForwardingRule) bool {
	// If one of the IP addresses is empty, do not consider it as an inequality.
	// If the IP address drops from a valid IP to empty, we do not want to apply
	// the change if it is the only change in the forwarding rule. Similarly, if
	// the forwarding rule changes from an empty IP to an allocated IP address, the
	// subnetwork will change as well.
	return (fr1.IPAddress == "" || fr2.IPAddress == "" || fr1.IPAddress == fr2.IPAddress) &&
		fr1.IPProtocol == fr2.IPProtocol &&
		fr1.LoadBalancingScheme == fr2.LoadBalancingScheme &&
		utils.EqualStringSets(fr1.Ports, fr2.Ports) &&
		fr1.BackendService == fr2.BackendService &&
		fr1.AllowGlobalAccess == fr2.AllowGlobalAccess &&
		fr1.Subnetwork == fr2.Subnetwork
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

// getAPIVersion returns the API version to use for CRUD of Forwarding rules, given the options enabled.
func getAPIVersion(options gce.ILBOptions) meta.Version {
	if options.AllowGlobalAccess {
		return meta.VersionBeta
	}
	return meta.VersionGA
}
