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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/golang/glog"
	compute "google.golang.org/api/compute/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// The gce api uses the name of a path rule to match a host rule.
	hostRulePrefix = "host"
)

func (l *L7) assertUrlMapExists() (err error) {
	if l.runtimeInfo.UrlMap == nil {
		return fmt.Errorf("cannot create urlmap without internal representation")
	}

	defaultBackendName := l.runtimeInfo.UrlMap.DefaultBackend.BackendName(l.namer)
	urlMapName := l.namer.UrlMap(l.Name)
	urlMap, _ := l.cloud.GetUrlMap(urlMapName)
	if urlMap != nil {
		glog.V(3).Infof("Url map %v already exists", urlMap.Name)
		l.um = urlMap
		return nil
	}

	glog.V(3).Infof("Creating url map %v for backend %v", urlMapName, defaultBackendName)
	newUrlMap := &compute.UrlMap{
		Name:           urlMapName,
		DefaultService: utils.BackendServiceRelativeResourcePath(defaultBackendName),
	}
	if err = l.cloud.CreateUrlMap(newUrlMap); err != nil {
		return err
	}
	urlMap, err = l.cloud.GetUrlMap(urlMapName)
	if err != nil {
		return err
	}
	l.um = urlMap
	return nil
}

// getNameForPathMatcher returns a name for a pathMatcher based on the given host rule.
// The host rule can be a regex, the path matcher name used to associate the 2 cannot.
func getNameForPathMatcher(hostRule string) string {
	hasher := md5.New()
	hasher.Write([]byte(hostRule))
	return fmt.Sprintf("%v%v", hostRulePrefix, hex.EncodeToString(hasher.Sum(nil)))
}

// UpdateUrlMap translates the given hostname: endpoint->port mapping into a gce url map.
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
func (l *L7) UpdateUrlMap() error {
	if l.um == nil {
		return fmt.Errorf("cannot update GCE urlmap without an existing GCE urlmap.")
	}
	if l.runtimeInfo.UrlMap == nil {
		return fmt.Errorf("cannot update GCE urlmap without internal representation")
	}

	urlMap := l.runtimeInfo.UrlMap
	defaultBackendName := urlMap.DefaultBackend.BackendName(l.namer)
	l.um.DefaultService = utils.BackendServiceRelativeResourcePath(defaultBackendName)

	// Every update replaces the entire urlmap.
	// TODO:  when we have multiple loadbalancers point to a single gce url map
	// this needs modification. For now, there is a 1:1 mapping of urlmaps to
	// Ingresses, so if the given Ingress doesn't have a host rule we should
	// delete the path to that backend.
	l.um.HostRules = []*compute.HostRule{}
	l.um.PathMatchers = []*compute.PathMatcher{}

	for hostname, rules := range urlMap.HostRules {
		// Create a host rule
		// Create a path matcher
		// Add all given endpoint:backends to pathRules in path matcher
		pmName := getNameForPathMatcher(hostname)
		l.um.HostRules = append(l.um.HostRules, &compute.HostRule{
			Hosts:       []string{hostname},
			PathMatcher: pmName,
		})

		pathMatcher := &compute.PathMatcher{
			Name:           pmName,
			DefaultService: l.um.DefaultService,
			PathRules:      []*compute.PathRule{},
		}

		// GCE ensures that matched rule with longest prefix wins.
		for _, rule := range rules {
			beName := rule.Backend.BackendName(l.namer)
			beLink := utils.BackendServiceRelativeResourcePath(beName)
			pathMatcher.PathRules = append(
				pathMatcher.PathRules, &compute.PathRule{Paths: []string{rule.Path}, Service: beLink})
		}
		l.um.PathMatchers = append(l.um.PathMatchers, pathMatcher)
	}
	oldMap, _ := l.cloud.GetUrlMap(l.um.Name)
	if oldMap != nil && mapsEqual(oldMap, l.um) {
		glog.V(4).Infof("URLMap for l7 %v is unchanged", l.Name)
		return nil
	}

	glog.V(3).Infof("Updating URLMap: %q", l.um.Name)
	if err := l.cloud.UpdateUrlMap(l.um); err != nil {
		return err
	}

	um, err := l.cloud.GetUrlMap(l.um.Name)
	if err != nil {
		return err
	}

	l.um = um
	return nil
}

// getBackendNames returns the names of backends in this L7 urlmap.
func (l *L7) getBackendNames() []string {
	if l.um == nil {
		return []string{}
	}
	beNames := sets.NewString()
	for _, pathMatcher := range l.um.PathMatchers {
		for _, pathRule := range pathMatcher.PathRules {
			// This is gross, but the urlmap only has links to backend services.
			parts := strings.Split(pathRule.Service, "/")
			name := parts[len(parts)-1]
			if name != "" {
				beNames.Insert(name)
			}
		}
	}
	// The default Service recorded in the urlMap is a link to the backend.
	// Note that this can either be user specified, or the L7 controller's
	// global default.
	parts := strings.Split(l.um.DefaultService, "/")
	defaultBackendName := parts[len(parts)-1]
	if defaultBackendName != "" {
		beNames.Insert(defaultBackendName)
	}
	return beNames.List()
}

func mapsEqual(a, b *compute.UrlMap) bool {
	if utils.BackendServiceComparablePath(a.DefaultService) != utils.BackendServiceComparablePath(b.DefaultService) {
		return false
	}
	if len(a.HostRules) != len(b.HostRules) {
		return false
	}
	for i := range a.HostRules {
		a := a.HostRules[i]
		b := b.HostRules[i]
		if a.Description != b.Description {
			return false
		}
		if len(a.Hosts) != len(b.Hosts) {
			return false
		}
		for i := range a.Hosts {
			if a.Hosts[i] != b.Hosts[i] {
				return false
			}
		}
		if a.PathMatcher != b.PathMatcher {
			return false
		}
	}
	if len(a.PathMatchers) != len(b.PathMatchers) {
		return false
	}
	for i := range a.PathMatchers {
		a := a.PathMatchers[i]
		b := b.PathMatchers[i]
		if utils.BackendServiceComparablePath(a.DefaultService) != utils.BackendServiceComparablePath(b.DefaultService) {
			return false
		}
		if a.Description != b.Description {
			return false
		}
		if a.Name != b.Name {
			return false
		}
		if len(a.PathRules) != len(b.PathRules) {
			return false
		}
		for i := range a.PathRules {
			a := a.PathRules[i]
			b := b.PathRules[i]
			if len(a.Paths) != len(b.Paths) {
				return false
			}
			for i := range a.Paths {
				if a.Paths[i] != b.Paths[i] {
					return false
				}
			}
			// Trim down the url's for a.Service and b.Service to a comparable structure
			// We do this because we update the UrlMap with relative links (not full) to backends.
			if utils.BackendServiceComparablePath(a.Service) != utils.BackendServiceComparablePath(b.Service) {
				return false
			}
		}
	}
	return true
}
