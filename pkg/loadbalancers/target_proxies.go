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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	// Every target https proxy accepts upto 10 ssl certificates.
	TargetProxyCertLimit = 10
)

func (l *L7) checkProxy() (err error) {
	urlMapLink := cloud.NewUrlMapsResourceID("", l.um.Name).ResourcePath()
	proxyName := l.namer.TargetProxy(l.Name, utils.HTTPProtocol)
	proxy, _ := l.cloud.GetTargetHTTPProxy(proxyName)
	if proxy == nil {
		klog.V(3).Infof("Creating new http proxy for urlmap %v", l.um.Name)
		newProxy := &compute.TargetHttpProxy{
			Name:   proxyName,
			UrlMap: urlMapLink,
		}
		if err = l.cloud.CreateTargetHTTPProxy(newProxy); err != nil {
			return err
		}
		proxy, err = l.cloud.GetTargetHTTPProxy(proxyName)
		if err != nil {
			return err
		}
		l.tp = proxy
		return nil
	}
	if !utils.EqualResourcePaths(proxy.UrlMap, urlMapLink) {
		klog.V(3).Infof("Proxy %v has the wrong url map, setting %v overwriting %v",
			proxy.Name, urlMapLink, proxy.UrlMap)
		if err := l.cloud.SetURLMapForTargetHTTPProxy(proxy, urlMapLink); err != nil {
			return err
		}
	}
	l.tp = proxy
	return nil
}

func (l *L7) checkHttpsProxy() (err error) {
	if len(l.sslCerts) == 0 {
		klog.V(3).Infof("No SSL certificates for %q, will not create HTTPS proxy.", l.Name)
		return nil
	}

	urlMapLink := cloud.NewUrlMapsResourceID("", l.um.Name).ResourcePath()
	proxyName := l.namer.TargetProxy(l.Name, utils.HTTPSProtocol)
	proxy, _ := l.cloud.GetTargetHTTPSProxy(proxyName)
	if proxy == nil {
		klog.V(3).Infof("Creating new https proxy for urlmap %q", l.um.Name)
		newProxy := &compute.TargetHttpsProxy{
			Name:   proxyName,
			UrlMap: urlMapLink,
		}

		for _, c := range l.sslCerts {
			newProxy.SslCertificates = append(newProxy.SslCertificates, c.SelfLink)
		}

		if err = l.cloud.CreateTargetHTTPSProxy(newProxy); err != nil {
			return err
		}

		proxy, err = l.cloud.GetTargetHTTPSProxy(proxyName)
		if err != nil {
			return err
		}

		l.tps = proxy
		return nil
	}
	if !utils.EqualResourcePaths(proxy.UrlMap, urlMapLink) {
		klog.V(3).Infof("Https proxy %v has the wrong url map, setting %v overwriting %v",
			proxy.Name, urlMapLink, proxy.UrlMap)
		if err := l.cloud.SetURLMapForTargetHTTPSProxy(proxy, urlMapLink); err != nil {
			return err
		}
	}

	if !l.compareCerts(proxy.SslCertificates) {
		klog.V(3).Infof("Https proxy %q has the wrong ssl certs, setting %v overwriting %v",
			proxy.Name, toCertNames(l.sslCerts), proxy.SslCertificates)
		var sslCertURLs []string
		for _, cert := range l.sslCerts {
			sslCertURLs = append(sslCertURLs, cert.SelfLink)
		}
		if err := l.cloud.SetSslCertificateForTargetHTTPSProxy(proxy, sslCertURLs); err != nil {
			return err
		}

	}
	l.tps = proxy
	return nil
}

func (l *L7) getSslCertLinkInUse() ([]string, error) {
	proxyName := l.namer.TargetProxy(l.Name, utils.HTTPSProtocol)
	proxy, err := l.cloud.GetTargetHTTPSProxy(proxyName)
	if err != nil {
		return nil, err
	}

	return proxy.SslCertificates, nil
}
