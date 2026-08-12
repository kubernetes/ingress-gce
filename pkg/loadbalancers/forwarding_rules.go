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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

func (l7 *L7) checkHttpForwardingRule() (err error) {
	if l7.tp == nil {
		return fmt.Errorf("cannot create forwarding rule without proxy")
	}
	name := l7.namer.ForwardingRule(namer.HTTPProtocol)
	address, _, err := l7.getEffectiveIP()
	if err != nil {
		return err
	}
	fw, err := l7.checkForwardingRule(namer.HTTPProtocol, name, l7.tp.SelfLink, address)
	if err != nil {
		return err
	}
	l7.fw = fw
	return nil
}

func (l7 *L7) checkHttpsForwardingRule() (err error) {
	if l7.tps == nil {
		l7.logger.V(3).Info("No https target proxy for l7, not created https forwarding rule", "l7", l7)
		return nil
	}
	name := l7.namer.ForwardingRule(namer.HTTPSProtocol)
	address, _, err := l7.getEffectiveIP()
	if err != nil {
		return err
	}
	fws, err := l7.checkForwardingRule(namer.HTTPSProtocol, name, l7.tps.SelfLink, address)
	if err != nil {
		return err
	}
	l7.fws = fws
	return nil
}

func (l7 *L7) checkForwardingRule(protocol namer.NamerProtocol, name, proxyLink, ip string) (existing *composite.ForwardingRule, err error) {
	key, err := l7.CreateKey(name)
	if err != nil {
		return nil, err
	}
	version := l7.Versions().ForwardingRule
	description, err := l7.description()
	if err != nil {
		return nil, err
	}

	isL7ILB := utils.IsGCEL7ILBIngress(l7.runtimeInfo.Ingress)
	isL7XLBRegional := utils.IsGCEL7XLBRegionalIngress(&l7.ingress)
	tr := translator.NewTranslator(isL7ILB, isL7XLBRegional, l7.namer)
	env := &translator.Env{VIP: ip, Network: l7.cloud.NetworkURL(), Subnetwork: l7.cloud.SubnetworkURL()}
	fr := tr.ToCompositeForwardingRule(env, protocol, version, proxyLink, description, l7.runtimeInfo.StaticIPSubnet)

	fr.AllowGlobalAccess = utils.IsGCEL7ILBIngressGlobalAccessEnabled(&l7.ingress)
	
	existing, _ = composite.GetForwardingRule(l7.cloud, key, version, l7.logger)
	if existing != nil && (fr.IPAddress != "" && existing.IPAddress != fr.IPAddress || existing.PortRange != fr.PortRange || existing.AllowGlobalAccess != fr.AllowGlobalAccess) {
		l7.logger.Info("Recreating forwarding rule %v(%v), so it has %v(%v)",
			"existingIp", existing.IPAddress, "existingPortRange", existing.PortRange,
			"targetIp", fr.IPAddress, "targetPortRange", fr.PortRange)
		if err = utils.IgnoreHTTPNotFound(composite.DeleteForwardingRule(l7.cloud, key, version, l7.logger)); err != nil {
			return nil, err
		}
		existing = nil
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q deleted", key.Name)
	}
	if existing == nil {
		// This is a special case where exactly one of http or https forwarding rule
		// existed before and the existing forwarding rule uses ingress managed static ip address.
		// In this case, the forwarding rule needs to be created with the same static ip.
		// Note that this is not needed when user specifies a static IP.
		if ip == "" {
			managedStaticIPName := l7.namer.ForwardingRule(namer.HTTPProtocol)
			// Get static IP address if ingress has static IP annotation.
			// Note that this Static IP annotation is applied by ingress controller.
			if currentIPName, exists := l7.ingress.Annotations[annotations.StaticIPKey]; exists && currentIPName == managedStaticIPName {
				currentIP, _ := l7.cloud.GetGlobalAddress(managedStaticIPName)
				if currentIP != nil {
					l7.logger.V(3).Info("Ingress managed static IP %s(%s) exists, using it to create forwarding rule %s", "currentIpName", currentIPName, "currentIp", currentIP.Address, "forwardingRuleName", name)
					fr.IPAddress = currentIP.Address
				}
			}
		}
		l7.logger.V(3).Info("Creating forwarding rule for proxy and ip", "proxy", proxyLink, "ip", ip, "protocol", protocol)

		if err = composite.CreateForwardingRule(l7.cloud, key, fr, l7.logger); err != nil {
			return nil, err
		}
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "ForwardingRule %q created", key.Name)

		key, err = l7.CreateKey(name)
		if err != nil {
			return nil, err
		}
		existing, err = composite.GetForwardingRule(l7.cloud, key, version, l7.logger)
		if err != nil {
			return nil, err
		}
	}
	// TODO: If the port range and protocol don't match, recreate the rule
	if utils.EqualResourceIDs(existing.Target, proxyLink) {
		l7.logger.V(4).Info("Forwarding rule already exists", "forwardingRuleName", existing.Name)
	} else {
		l7.logger.V(3).Info("Forwarding rule has the wrong proxy, overwriting",
			"forwardingRuleName", existing.Name, "existingTarget", existing.Target, "proxy", proxyLink)
		key, err := l7.CreateKey(existing.Name)
		if err != nil {
			return nil, err
		}
		if err := composite.SetProxyForForwardingRule(l7.cloud, key, existing, proxyLink, l7.logger); err != nil {
			return nil, err
		}
	}
	return existing, nil
}

// getEffectiveIP returns a string with the IP to use in the HTTP and HTTPS
// forwarding rules, a boolean indicating if this is an IP the controller
// should manage or not and an error if the specified IP was not found.
func (l7 *L7) getEffectiveIP() (string, bool, error) {
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

	if l7.runtimeInfo.StaticIPName != "" {
		key, err := l7.CreateKey(l7.runtimeInfo.StaticIPName)
		if err != nil {
			return "", false, err
		}

		// Existing static IPs allocated to forwarding rules will get orphaned
		// till the Ingress is torn down.
		// TODO(shance): Replace version
		if ip, err := composite.GetAddress(l7.cloud, key, meta.VersionGA, l7.logger); err != nil || ip == nil {
			return "", false, fmt.Errorf("the given static IP name %v doesn't translate to an existing static IP.",
				l7.runtimeInfo.StaticIPName)
		} else {
			l7.runtimeInfo.StaticIPSubnet = ip.Subnetwork
			return ip.Address, false, nil
		}
	}
	if l7.ip != nil {
		return l7.ip.Address, true, nil
	}
	return "", true, nil
}
