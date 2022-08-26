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
	"net/http"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

// checkStaticIP reserves a regional or global static IP allocated to the Forwarding Rule.
func (l7 *L7) checkStaticIP() (err error) {
	if l7.fw == nil || l7.fw.IPAddress == "" {
		return fmt.Errorf("will not create static IP without a forwarding rule")
	}
	managedStaticIPName := l7.namer.ForwardingRule(namer.HTTPProtocol)
	// Don't manage staticIPs if the user has specified an IP.
	address, manageStaticIP, err := l7.getEffectiveIP()
	if err != nil {
		return err
	}
	if !manageStaticIP {
		klog.V(3).Infof("Not managing user specified static IP %v", address)
		if flags.F.EnableDeleteUnusedFrontends {
			// Delete ingress controller managed static ip if exists.
			if ip, ok := l7.ingress.Annotations[annotations.StaticIPKey]; ok && ip == managedStaticIPName {
				return l7.deleteStaticIP()
			}
		}
		return nil
	}

	key, err := l7.CreateKey(managedStaticIPName)
	if err != nil {
		return err
	}

	ip, _ := composite.GetAddress(l7.cloud, key, meta.VersionGA)
	if ip == nil {
		klog.V(3).Infof("Creating static ip %v", managedStaticIPName)
		address := l7.newStaticAddress(managedStaticIPName)

		err = composite.CreateAddress(l7.cloud, key, address)
		if err != nil {
			if utils.IsHTTPErrorCode(err, http.StatusConflict) ||
				utils.IsHTTPErrorCode(err, http.StatusBadRequest) {
				klog.V(3).Infof("IP %v(%v) is already reserved, assuming it is OK to use.",
					l7.fw.IPAddress, managedStaticIPName)
				return nil
			}
			return err
		}
		ip, err = composite.GetAddress(l7.cloud, key, meta.VersionGA)
		if err != nil {
			return err
		}
	}
	l7.ip = ip
	return nil
}

func (l7 *L7) newStaticAddress(name string) *composite.Address {
	isInternal := utils.IsGCEL7ILBIngress(&l7.ingress)
	address := &composite.Address{Name: name, Address: l7.fw.IPAddress, Version: meta.VersionGA}
	if isInternal {
		// Used for L7 ILB
		address.AddressType = "INTERNAL"
	}

	return address
}
