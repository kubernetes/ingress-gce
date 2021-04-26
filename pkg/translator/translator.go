/*
Copyright 2020 The Kubernetes Authors.
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

package translator

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	api_v1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	frontendconfigv1beta1 "k8s.io/ingress-gce/pkg/apis/frontendconfig/v1beta1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/flags"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/ingress-gce/pkg/utils/namer"
)

// Env contains all k8s & GCP configuration needed to perform the translation.
type Env struct {
	// Ing is the Ingress we are translating.
	Ing *v1.Ingress
	// TODO(shance): this should be a map, similar to SecretsMap
	// FrontendConfig is the frontendconfig associated with the Ingress
	FrontendConfig *frontendconfigv1beta1.FrontendConfig
	// SecretsMap contains a mapping from Secret name to the actual resource.
	// It is assumed that the map contains resources from a single namespace.
	// This is the same namespace as the Ingress namespace.
	SecretsMap map[string]*api_v1.Secret
	// VIP is the IP address assigned to the Ingress. This could be a raw IP address in GCP or the
	// name of an Address resource.
	VIP        string
	Network    string
	Subnetwork string
	Region     string
	Project    string
}

// NewEnv returns an Env for the given Ingress.
func NewEnv(ing *v1.Ingress, client kubernetes.Interface, vip, net, subnet string) (*Env, error) {
	ret := &Env{Ing: ing, SecretsMap: make(map[string]*api_v1.Secret), VIP: vip, Network: net, Subnetwork: subnet}
	for _, tlsSpec := range ing.Spec.TLS {
		if len(tlsSpec.SecretName) == 0 {
			// doesn't use a secret
			continue
		}
		if _, ok := ret.SecretsMap[tlsSpec.SecretName]; ok {
			// already fetched
			continue
		}
		secret, err := client.CoreV1().Secrets(ing.Namespace).Get(context.TODO(), tlsSpec.SecretName, meta_v1.GetOptions{})
		if err != nil {
			return nil, err
		}
		ret.SecretsMap[tlsSpec.SecretName] = secret
	}
	return ret, nil
}

// Translator implements the mapping between an Ingress and its corresponding GCE resources.
type Translator struct {
	// IsL7ILB is true if the Ingress will be translated into an L7 ILB (as opposed to an XLB).
	IsL7ILB bool
	// FrontendNamer generates names for frontend resources.
	FrontendNamer namer.IngressFrontendNamer
}

// NewTranslator returns a new Translator.
func NewTranslator(isL7ILB bool, frontendNamer namer.IngressFrontendNamer) *Translator {
	return &Translator{IsL7ILB: isL7ILB, FrontendNamer: frontendNamer}
}

// TLSCerts encapsulates .pem encoded TLS information.
// TODO(shance): Remove this intermediate representation
type TLSCerts struct {
	// Key is private key.
	Key string
	// Cert is a public key.
	Cert string
	// Chain is a certificate chain.
	Chain string
	Name  string
	// md5 hash(first 8 bytes) of the cert contents
	CertHash string
}

// Secrets returns the Secrets from the environment which are specified in the Ingress.
func secrets(env *Env) ([]*api_v1.Secret, []error) {
	var ret []*api_v1.Secret
	var errors []error
	spec := env.Ing.Spec
	for _, tlsSpec := range spec.TLS {
		secret, ok := env.SecretsMap[tlsSpec.SecretName]
		if !ok {
			errors = append(errors, fmt.Errorf("secret %q does not exist", tlsSpec.SecretName))
			break
		}
		// Fail-fast if the user's secret does not have the proper fields specified.
		if secret.Data[api_v1.TLSCertKey] == nil {
			errors = append(errors, fmt.Errorf("secret %q does not specify cert as string data", tlsSpec.SecretName))
			break
		}
		if secret.Data[api_v1.TLSPrivateKeyKey] == nil {
			errors = append(errors, fmt.Errorf("secret %q does not specify private key as string data", tlsSpec.SecretName))
			break
		}
		ret = append(ret, secret)
	}

	return ret, errors
}

// The gce api uses the name of a path rule to match a host rule.
const hostRulePrefix = "host"

// ToCompositeURLMap translates the given hostname: endpoint->port mapping into a gce url map.
//
// HostRule: Conceptually contains all PathRules for a given host.
// PathMatcher: Associates a path rule with a host rule. Mostly an optimization.
// PathRule: Maps a single path regex to a backend.
//
// The GCE url map allows multiple hosts to share url->backend mappings without duplication, eg:
//   Host: foo(PathMatcher1), bar(PathMatcher1,2)
//   PathMatcher1:
//     /a -> b1
//     /b -> b2
//   PathMatcher2:
//     /c -> b1
// This leads to a lot of complexity in the common case, where all we want is a mapping of
// host->{/path: backend}.
//
// Consider some alternatives:
// 1. Using a single backend per PathMatcher:
//   Host: foo(PathMatcher1,3) bar(PathMatcher1,2,3)
//   PathMatcher1:
//     /a -> b1
//   PathMatcher2:
//     /c -> b1
//   PathMatcher3:
//     /b -> b2
// 2. Using a single host per PathMatcher:
//   Host: foo(PathMatcher1)
//   PathMatcher1:
//     /a -> b1
//     /b -> b2
//   Host: bar(PathMatcher2)
//   PathMatcher2:
//     /a -> b1
//     /b -> b2
//     /c -> b1
// In the context of kubernetes services, 2 makes more sense, because we
// rarely want to lookup backends (service:nodeport). When a service is
// deleted, we need to find all host PathMatchers that have the backend
// and remove the mapping. When a new path is added to a host (happens
// more frequently than service deletion) we just need to lookup the 1
// pathmatcher of the host.
func ToCompositeURLMap(g *utils.GCEURLMap, namer namer.IngressFrontendNamer, key *meta.Key) *composite.UrlMap {
	defaultBackendName := g.DefaultBackend.BackendName()
	key.Name = defaultBackendName
	resourceID := cloud.ResourceID{ProjectID: "", Resource: "backendServices", Key: key}
	m := &composite.UrlMap{
		Name:           namer.UrlMap(),
		DefaultService: resourceID.ResourcePath(),
	}

	for _, hostRule := range g.HostRules {
		// Create a host rule
		// Create a path matcher
		// Add all given endpoint:backends to pathRules in path matcher
		pmName := getNameForPathMatcher(hostRule.Hostname)
		m.HostRules = append(m.HostRules, &composite.HostRule{
			Hosts:       []string{hostRule.Hostname},
			PathMatcher: pmName,
		})

		pathMatcher := &composite.PathMatcher{
			Name:           pmName,
			DefaultService: m.DefaultService,
			PathRules:      []*composite.PathRule{},
		}

		// GCE ensures that matched rule with longest prefix wins.
		for _, rule := range hostRule.Paths {
			beName := rule.Backend.BackendName()
			key.Name = beName
			resourceID := cloud.ResourceID{ProjectID: "", Resource: "backendServices", Key: key}
			beLink := resourceID.ResourcePath()
			pathMatcher.PathRules = append(pathMatcher.PathRules, &composite.PathRule{
				Paths:   []string{rule.Path},
				Service: beLink,
			})
		}
		m.PathMatchers = append(m.PathMatchers, pathMatcher)
	}
	return m
}

// ToRedirectUrlMap returns the UrlMap used for HTTPS Redirects on a L7 ELB
// This function returns nil if no url map needs to be created
func (t *Translator) ToRedirectUrlMap(env *Env, version meta.Version) *composite.UrlMap {
	if env.FrontendConfig == nil || env.FrontendConfig.Spec.RedirectToHttps == nil {
		return nil
	}

	if !env.FrontendConfig.Spec.RedirectToHttps.Enabled {
		return nil
	}

	// Second arg is handled upstream
	name, _ := t.FrontendNamer.RedirectUrlMap()
	redirectConfig := env.FrontendConfig.Spec.RedirectToHttps
	expectedMap := &composite.UrlMap{
		Name:               name,
		DefaultUrlRedirect: &composite.HttpRedirectAction{HttpsRedirect: redirectConfig.Enabled, RedirectResponseCode: redirectConfig.ResponseCodeName},
		Version:            version,
	}
	return expectedMap
}

// getNameForPathMatcher returns a name for a pathMatcher based on the given host rule.
// The host rule can be a regex, the path matcher name used to associate the 2 cannot.
func getNameForPathMatcher(hostRule string) string {
	hasher := md5.New()
	hasher.Write([]byte(hostRule))
	return fmt.Sprintf("%v%v", hostRulePrefix, hex.EncodeToString(hasher.Sum(nil)))
}

const (
	httpDefaultPortRange  = "80-80"
	httpsDefaultPortRange = "443-443"
)

// ToCompositeForwardingRule returns a composite.ForwardingRule of type HTTP or HTTPS.
func (t *Translator) ToCompositeForwardingRule(env *Env, protocol namer.NamerProtocol, version meta.Version, proxyLink, description, fwSubnet string) *composite.ForwardingRule {
	var portRange string
	if protocol == namer.HTTPProtocol {
		portRange = httpDefaultPortRange
	} else {
		portRange = httpsDefaultPortRange
	}

	fr := &composite.ForwardingRule{
		Name:        t.FrontendNamer.ForwardingRule(protocol),
		IPAddress:   env.VIP,
		Target:      proxyLink,
		PortRange:   portRange,
		IPProtocol:  "TCP",
		Description: description,
		Version:     version,
	}

	if t.IsL7ILB {
		fr.LoadBalancingScheme = "INTERNAL_MANAGED"
		fr.Network = env.Network
		if fwSubnet != "" {
			fr.Subnetwork = fwSubnet
		} else {
			fr.Subnetwork = env.Subnetwork
		}
	}

	return fr
}

func (t *Translator) ToCompositeTargetHttpProxy(description string, version meta.Version, urlMapKey *meta.Key) *composite.TargetHttpProxy {
	resourceID := cloud.ResourceID{ProjectID: "", Resource: "urlMaps", Key: urlMapKey}
	urlMapLink := resourceID.ResourcePath()
	proxyName := t.FrontendNamer.TargetProxy(namer.HTTPProtocol)

	proxy := &composite.TargetHttpProxy{
		Name:        proxyName,
		UrlMap:      urlMapLink,
		Description: description,
		Version:     version,
	}

	return proxy
}

//TODO(shance): find a way to remove the second return value for sslPolicySet.  We currently need to this to maintain the behavior where we do not update the policy if the frontendconfig is empty/deleted
func (t *Translator) ToCompositeTargetHttpsProxy(env *Env, description string, version meta.Version, urlMapKey *meta.Key, sslCerts []*composite.SslCertificate) (*composite.TargetHttpsProxy, bool, error) {
	resourceID := cloud.ResourceID{ProjectID: "", Resource: "urlMaps", Key: urlMapKey}
	urlMapLink := resourceID.ResourcePath()
	proxyName := t.FrontendNamer.TargetProxy(namer.HTTPSProtocol)

	var certs []string
	for _, c := range sslCerts {
		certs = append(certs, c.SelfLink)
	}

	proxy := &composite.TargetHttpsProxy{
		Name:            proxyName,
		UrlMap:          urlMapLink,
		Description:     description,
		SslCertificates: certs,
		Version:         version,
	}
	var sslPolicySet bool
	if flags.F.EnableFrontendConfig {
		sslPolicy, err := sslPolicyLink(env)
		if err != nil {
			return nil, sslPolicySet, err
		}
		if sslPolicy != nil {
			proxy.SslPolicy = *sslPolicy
			sslPolicySet = true
		}
	}

	return proxy, sslPolicySet, nil
}

func (t *Translator) ToCompositeSSLCertificates(env *Env, tlsName string, tls []*TLSCerts, version meta.Version) []*composite.SslCertificate {
	var certs []*composite.SslCertificate

	// Pre-shared certs
	tlsNames := utils.SplitAnnotation(tlsName)
	for _, name := range tlsNames {
		resID := cloud.ResourceID{Resource: "sslCertificates", Key: &meta.Key{Name: name}, ProjectID: env.Project}
		if t.IsL7ILB {
			resID.Key.Region = env.Region
		}
		preSharedCert := &composite.SslCertificate{
			Name:     name,
			SelfLink: resID.SelfLink(version),
		}
		certs = append(certs, preSharedCert)
	}

	for _, tlsCert := range tls {
		ingCert := tlsCert.Cert
		ingKey := tlsCert.Key
		gcpCertName := t.FrontendNamer.SSLCertName(tlsCert.CertHash)
		resID := cloud.ResourceID{Resource: "sslCertificates", Key: &meta.Key{Name: gcpCertName}, ProjectID: env.Project}
		if t.IsL7ILB {
			resID.Key.Region = env.Region
		}
		cert := &composite.SslCertificate{
			Name:        gcpCertName,
			Certificate: ingCert,
			PrivateKey:  ingKey,
			SelfLink:    resID.SelfLink(version),
		}
		certs = append(certs, cert)
	}

	return certs
}

// sslPolicyLink returns the ref to the ssl policy that is described by the
// frontend config.  Since Ssl Policy is a *string, there are three possible I/O situations
// 1) policy is nil -> this returns nil
// 2) policy is an empty string -> this returns an empty string
// 3) policy is non-empty -> this constructs the resource path and returns it
func sslPolicyLink(env *Env) (*string, error) {
	var link string

	if env.FrontendConfig == nil {
		return nil, nil
	}

	policyName := env.FrontendConfig.Spec.SslPolicy
	if policyName == nil {
		return nil, nil
	}
	if *policyName == "" {
		return &link, nil
	}

	resourceID := cloud.ResourceID{
		Resource: "sslPolicies",
		Key:      meta.GlobalKey(*policyName),
	}
	resID := resourceID.ResourcePath()

	return &resID, nil
}

// TODO(shance): find a way to unexport this
func GetCertHash(contents string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(contents)))[:16]
}

func ToTLSCerts(env *Env) ([]*TLSCerts, []error) {
	var certs []*TLSCerts
	var errors []error

	secrets, errors := secrets(env)
	for _, secret := range secrets {
		cert := string(secret.Data[api_v1.TLSCertKey])
		newCert := &TLSCerts{
			Key:      string(secret.Data[api_v1.TLSPrivateKeyKey]),
			Cert:     cert,
			Name:     secret.Name,
			CertHash: GetCertHash(cert),
		}
		certs = append(certs, newCert)
	}
	return certs, errors
}
