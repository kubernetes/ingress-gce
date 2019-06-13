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
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

const (
	// The gce api uses the name of a path rule to match a host rule.
	hostRulePrefix = "host"
)

// ensureComputeURLMap retrieves the current URLMap and overwrites it if incorrect. If the resource
// does not exist, the map is created.
func (l *L7) ensureComputeURLMap() error {
	if l.runtimeInfo.UrlMap == nil {
		return fmt.Errorf("cannot create urlmap without internal representation")
	}

	// Every update replaces the entire urlmap.
	key := l.CreateKey("")
	expectedMap := toCompositeURLMap(l.Name, l.runtimeInfo.UrlMap, l.namer, key)
	key.Name = expectedMap.Name

	// Update URLMap for L7-ILB
	if key.Region != "" {
		expectedMap.Version = meta.VersionAlpha
		expectedMap.Region = key.Region
	}

	currentMap, err := l.cloud.GetUrlMap(l.version, key)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return err
	}

	if currentMap == nil {
		klog.V(3).Infof("Creating URLMap %q", expectedMap.Name)
		if err := l.cloud.CreateUrlMap(expectedMap, key); err != nil {
			return fmt.Errorf("CreateUrlMap: %v", err)
		}
		l.um = expectedMap
		return nil
	}

	if mapsEqual(currentMap, expectedMap) {
		klog.V(4).Infof("URLMap for %q is unchanged", l.Name)
		l.um = currentMap
		return nil
	}

	klog.V(3).Infof("Updating URLMap for %q", l.Name)
	expectedMap.Fingerprint = currentMap.Fingerprint
	if err := l.cloud.UpdateUrlMap(expectedMap, key); err != nil {
		return fmt.Errorf("UpdateURLMap: %v", err)
	}

	l.um = expectedMap
	return nil
}

// getBackendNames returns the names of backends in this L7 urlmap.
func getBackendNames(computeURLMap *composite.UrlMap) ([]string, error) {
	beNames := sets.NewString()
	for _, pathMatcher := range computeURLMap.PathMatchers {
		name, err := utils.KeyName(pathMatcher.DefaultService)
		if err != nil {
			return nil, err
		}
		beNames.Insert(name)

		for _, pathRule := range pathMatcher.PathRules {
			name, err = utils.KeyName(pathRule.Service)
			if err != nil {
				return nil, err
			}
			beNames.Insert(name)
		}
	}
	// The default Service recorded in the urlMap is a link to the backend.
	// Note that this can either be user specified, or the L7 controller's
	// global default.
	name, err := utils.KeyName(computeURLMap.DefaultService)
	if err != nil {
		return nil, err
	}
	beNames.Insert(name)
	return beNames.List(), nil
}

// mapsEqual compares the structure of two compute.UrlMaps.
// The service strings are parsed and compared as resource paths (such as
// "global/backendServices/my-service") to ignore variables: endpoint, version, and project.
func mapsEqual(a, b *composite.UrlMap) bool {
	if !utils.EqualResourcePaths(a.DefaultService, b.DefaultService) {
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
		if !utils.EqualResourcePaths(a.DefaultService, b.DefaultService) {
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
			if !utils.EqualResourcePaths(a.Service, b.Service) {
				return false
			}
		}
	}
	return true
}

// toCompositeURLMap translates the given hostname: endpoint->port mapping into a gce url map.
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
func toCompositeURLMap(lbName string, g *utils.GCEURLMap, namer *utils.Namer, key *meta.Key) *composite.UrlMap {
	defaultBackendName := g.DefaultBackend.BackendName(namer)
	key.Name = defaultBackendName
	//resourceID := cloud.ResourceID{"", "backendServices", meta.GlobalKey(defaultBackendName)}
	resourceID := cloud.ResourceID{ProjectID: "", Resource: "backendServices", Key: key}
	m := &composite.UrlMap{
		Name:           namer.UrlMap(lbName),
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
			beName := rule.Backend.BackendName(namer)
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

// getNameForPathMatcher returns a name for a pathMatcher based on the given host rule.
// The host rule can be a regex, the path matcher name used to associate the 2 cannot.
func getNameForPathMatcher(hostRule string) string {
	hasher := md5.New()
	hasher.Write([]byte(hostRule))
	return fmt.Sprintf("%v%v", hostRulePrefix, hex.EncodeToString(hasher.Sum(nil)))
}
