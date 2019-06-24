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

	"google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// checkStaticIP reserves a static IP allocated to the Forwarding Rule.
func (l *L7) checkStaticIP() (err error) {
	if l.fw == nil || l.fw.IPAddress == "" {
		return fmt.Errorf("will not create static IP without a forwarding rule")
	}
	// Don't manage staticIPs if the user has specified an IP.
	if address, manageStaticIP := l.getEffectiveIP(); !manageStaticIP {
		klog.V(3).Infof("Not managing user specified static IP %v", address)
		return nil
	}
	staticIPName := l.namer.ForwardingRule(l.Name, utils.HTTPProtocol)
	ip, _ := l.cloud.GetGlobalAddress(staticIPName)
	if ip == nil {
		klog.V(3).Infof("Creating static ip %v", staticIPName)
		err = l.cloud.ReserveGlobalAddress(&compute.Address{Name: staticIPName, Address: l.fw.IPAddress})
		if err != nil {
			if utils.IsHTTPErrorCode(err, http.StatusConflict) ||
				utils.IsHTTPErrorCode(err, http.StatusBadRequest) {
				klog.V(3).Infof("IP %v(%v) is already reserved, assuming it is OK to use.",
					l.fw.IPAddress, staticIPName)
				return nil
			}
			return err
		}
		ip, err = l.cloud.GetGlobalAddress(staticIPName)
		if err != nil {
			return err
		}
	}
	l.ip = ip
	return nil
}
