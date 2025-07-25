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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/klog/v2"
)

const (
	// Every target https proxy accepts up to 10 ssl certificates.
	TargetProxyCertLimit = 10
)

// checkProxy ensures the correct TargetHttpProxy for a loadbalancer
func (l7 *L7) checkProxy() (err error) {
	// Get UrlMap Name, could be the url map or the redirect url map
	// TODO(shance): move to translator
	var umName string
	if l7.redirectUm != nil &&
		l7.runtimeInfo.FrontendConfig != nil &&
		l7.runtimeInfo.FrontendConfig.Spec.RedirectToHttps != nil &&
		l7.runtimeInfo.FrontendConfig.Spec.RedirectToHttps.Enabled {
		umName = l7.redirectUm.Name
	} else {
		umName = l7.um.Name
	}

	urlMapKey, err := l7.CreateKey(umName)
	if err != nil {
		return err
	}

	isL7ILB := utils.IsGCEL7ILBIngress(l7.runtimeInfo.Ingress)
	isL7XLBRegional := utils.IsGCEL7XLBRegionalIngress(l7.runtimeInfo.Ingress)
	tr := translator.NewTranslator(isL7ILB, isL7XLBRegional, l7.namer)

	description, err := l7.description()
	if err != nil {
		return err
	}

	version := l7.Versions().TargetHttpProxy
	proxy := tr.ToCompositeTargetHttpProxy(description, version, urlMapKey)

	key, err := l7.CreateKey(proxy.Name)
	if err != nil {
		return err
	}

	currentProxy, _ := composite.GetTargetHttpProxy(l7.cloud, key, version, l7.logger)
	if currentProxy == nil {
		l7.logger.V(3).Info("Creating new http proxy for urlmap", "urlMapName", l7.um.Name)
		key, err := l7.CreateKey(proxy.Name)
		if err != nil {
			return err
		}
		if err = composite.CreateTargetHttpProxy(l7.cloud, key, proxy, l7.logger); err != nil {
			return err
		}
		currentProxy, err = composite.GetTargetHttpProxy(l7.cloud, key, version, l7.logger)
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "TargetProxy %q created", key.Name)
		if err != nil {
			return err
		}
		l7.tp = currentProxy
		return nil
	}
	if !utils.EqualResourcePaths(currentProxy.UrlMap, proxy.UrlMap) {
		l7.logger.V(3).Info("Proxy has the wrong url map, overwriting",
			"proxyName", currentProxy.Name, "newUrlMap", proxy.UrlMap, "existingUrlMap", currentProxy.UrlMap)
		key, err := l7.CreateKey(currentProxy.Name)
		if err != nil {
			return err
		}
		if err := composite.SetUrlMapForTargetHttpProxy(l7.cloud, key, currentProxy, proxy.UrlMap, l7.logger); err != nil {
			return err
		}
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "TargetProxy %q updated", key.Name)
	}
	l7.tp = currentProxy
	return nil
}

func (l7 *L7) checkHttpsProxy() (err error) {
	isL7ILB := utils.IsGCEL7ILBIngress(l7.runtimeInfo.Ingress)
	isL7XLBRegional := utils.IsGCEL7XLBRegionalIngress(l7.runtimeInfo.Ingress)
	tr := translator.NewTranslator(isL7ILB, isL7XLBRegional, l7.namer)
	env := &translator.Env{FrontendConfig: l7.runtimeInfo.FrontendConfig, Region: l7.cloud.Region()}

	if len(l7.sslCerts) == 0 {
		l7.logger.V(2).Info("No SSL certificates for load-balancer, will not create HTTPS Proxy.", "l7", l7)
		return nil
	}

	urlMapKey, err := l7.CreateKey(l7.um.Name)
	if err != nil {
		return err
	}
	description, err := l7.description()
	version := l7.Versions().TargetHttpProxy
	proxy, sslPolicySet, err := tr.ToCompositeTargetHttpsProxy(env, description, version, urlMapKey, l7.sslCerts)
	if err != nil {
		return err
	}

	key, err := l7.CreateKey(proxy.Name)
	if err != nil {
		return err
	}

	currentProxy, _ := composite.GetTargetHttpsProxy(l7.cloud, key, version, l7.logger)
	if err != nil {
		return err
	}

	if currentProxy == nil {
		l7.logger.V(3).Info("Creating new https Proxy for urlmap", "urlMapName", l7.um.Name)

		if err = composite.CreateTargetHttpsProxy(l7.cloud, key, proxy, l7.logger); err != nil {
			return err
		}
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "TargetProxy %q created", key.Name)

		key, err = l7.CreateKey(proxy.Name)
		if err != nil {
			return err
		}
		currentProxy, err = composite.GetTargetHttpsProxy(l7.cloud, key, version, l7.logger)
		if err != nil {
			return err
		}

		l7.tps = currentProxy
		return nil
	}

	if !utils.EqualResourcePaths(currentProxy.UrlMap, proxy.UrlMap) {
		l7.logger.V(2).Info("Https Proxy has the wrong url map, overwriting", "proxyName", currentProxy.Name, "newUrlMap", proxy.UrlMap, "existingUrlMap", currentProxy.UrlMap)
		key, err := l7.CreateKey(currentProxy.Name)
		if err != nil {
			return err
		}
		if err := composite.SetUrlMapForTargetHttpsProxy(l7.cloud, key, currentProxy, proxy.UrlMap, l7.logger); err != nil {
			return err
		}
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "TargetProxy %q updated", key.Name)
	}

	if !l7.compareCerts(currentProxy.SslCertificates) {
		l7.logger.V(2).Info("Https Proxy has the wrong ssl certs, overwriting",
			"proxyName", currentProxy.Name, "newCerts", toCertNames(l7.sslCerts), "existingCerts", currentProxy.SslCertificates)
		var sslCertURLs []string
		for _, cert := range l7.sslCerts {
			sslCertURLs = append(sslCertURLs, cert.SelfLink)
		}
		key, err := l7.CreateKey(currentProxy.Name)
		if err != nil {
			return err
		}
		if err := composite.SetSslCertificateForTargetHttpsProxy(l7.cloud, key, currentProxy, sslCertURLs, l7.logger); err != nil {
			return err
		}
		l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "TargetProxy %q certs updated", key.Name)
	}

	if sslPolicySet {
		if err := l7.ensureSslPolicy(env, currentProxy, proxy.SslPolicy); err != nil {
			return err
		}
	}

	l7.tps = currentProxy
	return nil
}

func (l7 *L7) getSslCertLinkInUse() ([]string, error) {
	proxyName := l7.namer.TargetProxy(namer.HTTPSProtocol)
	key, err := l7.CreateKey(proxyName)
	if err != nil {
		return nil, err
	}
	proxy, err := composite.GetTargetHttpsProxy(l7.cloud, key, l7.Versions().TargetHttpsProxy, l7.logger)
	if err != nil {
		return nil, err
	}

	return proxy.SslCertificates, nil
}

// ensureSslPolicy ensures that the SslPolicy described in the frontendconfig is
// properly applied to the proxy.
func (l7 *L7) ensureSslPolicy(env *translator.Env, currentProxy *composite.TargetHttpsProxy, policyLink string) error {
	if !utils.EqualResourcePaths(policyLink, currentProxy.SslPolicy) {
		l7.logger.Info("ensureSslPolicy", "newPolicyLink", policyLink, "currentPolicyLink", currentProxy.SslPolicy)
		key, err := l7.CreateKey(currentProxy.Name)
		if err != nil {
			return err
		}
		if l7.scope == meta.Regional {
			if err := ensureRegionalSslPolicy(l7.cloud, key, currentProxy, policyLink, l7.logger); err != nil {
				l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "Regional TargetHttpsProxy %q SSLPolicy updated", key.Name)
				return err
			}
		} else {
			if err := composite.SetSslPolicyForTargetHttpsProxy(l7.cloud, key, currentProxy, policyLink, l7.logger); err != nil {
				l7.recorder.Eventf(l7.runtimeInfo.Ingress, corev1.EventTypeNormal, events.SyncIngress, "TargetProxy %q SSLPolicy updated", key.Name)
				return err
			}
		}
	}
	return nil
}

// ensureRegionalSslPolicy updates sslPolicy for regional HTTPs Proxy.
// Regional HTTPs Proxies do not support setSslPolicy, and require using patch
// method.
func ensureRegionalSslPolicy(cloud *gce.Cloud, key *meta.Key, currentProxy *composite.TargetHttpsProxy, policyLink string, ingLogger klog.Logger) error {
	ingLogger.Info("Ensuring regional ssl policy", "policyLink", policyLink)
	patchProxy := &composite.TargetHttpsProxy{
		Fingerprint: currentProxy.Fingerprint,
		SslPolicy:   policyLink,
	}
	if policyLink == "" {
		patchProxy.NullFields = []string{"SslPolicy"}
	}
	return composite.PatchRegionalTargetHttpsProxy(cloud, key, patchProxy, ingLogger)
}
