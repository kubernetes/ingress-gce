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
	"k8s.io/ingress-gce/pkg/composite"

	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	httpDefaultPortRange  = "80-80"
	httpsDefaultPortRange = "443-443"
)

func (l *L7) checkHttpForwardingRule() (err error) {
	if l.tp == nil {
		return fmt.Errorf("cannot create forwarding rule without proxy")
	}
	name := l.namer.ForwardingRule(l.Name, utils.HTTPProtocol)
	address, _ := l.getEffectiveIP()
	fw, err := l.checkForwardingRule(name, l.tp.SelfLink, address, httpDefaultPortRange)
	if err != nil {
		return err
	}
	l.fw = fw
	return nil
}

func (l *L7) checkHttpsForwardingRule() (err error) {
	if l.tps == nil {
		klog.V(3).Infof("No https target proxy for %v, not created https forwarding rule", l.Name)
		return nil
	}
	name := l.namer.ForwardingRule(l.Name, utils.HTTPSProtocol)
	address, _ := l.getEffectiveIP()
	fws, err := l.checkForwardingRule(name, l.tps.SelfLink, address, httpsDefaultPortRange)
	if err != nil {
		return err
	}
	l.fws = fws
	return nil
}

func (l *L7) checkForwardingRule(name, proxyLink, ip, portRange string) (fw *composite.ForwardingRule, err error) {
	fw, _ = l.cloud.GetForwardingRule(l.version, l.CreateKey(name))
	if fw != nil && (ip != "" && fw.IPAddress != ip || fw.PortRange != portRange) {
		klog.Warningf("Recreating forwarding rule %v(%v), so it has %v(%v)",
			fw.IPAddress, fw.PortRange, ip, portRange)
		if err = utils.IgnoreHTTPNotFound(l.cloud.DeleteForwardingRule(l.version, l.CreateKey(name))); err != nil {
			return nil, err
		}
		fw = nil
	}
	if fw == nil {
		klog.V(3).Infof("Creating forwarding rule for proxy %q and ip %v:%v", proxyLink, ip, portRange)
		description, err := l.description()
		if err != nil {
			return nil, err
		}
		rule := &composite.ForwardingRule{
			Name:        name,
			IPAddress:   ip,
			Target:      proxyLink,
			PortRange:   portRange,
			IPProtocol:  "TCP",
			Description: description,
			Version:     l.version,
		}

		// Update rule for L7-ILB
		if l.resourceType == meta.Regional {
			rule.LoadBalancingScheme = "INTERNAL_MANAGED"
		}

		if err = l.cloud.CreateForwardingRule(rule, l.CreateKey(rule.Name)); err != nil {
			return nil, err
		}
		fw, err = l.cloud.GetForwardingRule(l.version, l.CreateKey(name))
		if err != nil {
			return nil, err
		}
	}
	// TODO: If the port range and protocol don't match, recreate the rule
	if utils.EqualResourceIDs(fw.Target, proxyLink) {
		klog.V(4).Infof("Forwarding rule %v already exists", fw.Name)
	} else {
		klog.V(3).Infof("Forwarding rule %v has the wrong proxy, setting %v overwriting %v",
			fw.Name, fw.Target, proxyLink)
		if err := l.cloud.SetProxyForForwardingRule(fw, l.CreateKey(fw.Name), proxyLink); err != nil {
			return nil, err
		}
	}
	return fw, nil
}

// getEffectiveIP returns a string with the IP to use in the HTTP and HTTPS
// forwarding rules, and a boolean indicating if this is an IP the controller
// should manage or not.
func (l *L7) getEffectiveIP() (string, bool) {

	// A note on IP management:
	// User specifies a different IP on startup:
	//	- We create a forwarding rule with the given IP.
	//		- If this ip doesn't exist in GCE, we create another one in the hope
	//		  that they will rectify it later on.
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
			klog.Warningf("The given static IP name %v doesn't translate to an existing global static IP, ignoring it and allocating a new IP: %v",
				l.runtimeInfo.StaticIPName, err)
		} else {
			return ip.Address, false
		}
	}
	if l.ip != nil {
		return l.ip.Address, true
	}
	return "", true
}
