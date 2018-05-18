/*
Copyright 2017 The Kubernetes Authors.
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

package utils

import (
	"fmt"

	"github.com/golang/glog"
)

// GCEURLMap is a simplified representation of a UrlMap somewhere
// in the middle of a compute.UrlMap and rules in an Ingress spec.
// This representation maintains three invariants/rules:
//       1. All hostnames are unique
//       2. All paths for a specific host are unique.
//       3. Adding paths for a hostname replaces existing for that host.
type GCEURLMap struct {
	DefaultBackend *ServicePort
	// hostRules is a mapping from hostnames to path rules for that host.
	HostRules map[string][]PathRule
}

// PathRule encapsulates the information for a single path -> backend mapping.
type PathRule struct {
	Path    string
	Backend ServicePort
}

// NewGCEURLMap returns an empty GCEURLMap
func NewGCEURLMap() *GCEURLMap {
	return &GCEURLMap{HostRules: make(map[string][]PathRule)}
}

// EqualMapping returns true if both maps point to the same ServicePortIDs.
// ServicePort settings are *not* included in this comparison.
func EqualMapping(a, b *GCEURLMap) bool {
	if (a.DefaultBackend != nil) != (b.DefaultBackend != nil) {
		return false
	}
	if a.DefaultBackend != nil && a.DefaultBackend.ID != b.DefaultBackend.ID {
		return false
	}

	if len(a.HostRules) != len(b.HostRules) {
		return false
	}

	for host, aRules := range a.HostRules {
		bRules, ok := b.HostRules[host]
		if !ok {
			return false
		}

		if len(aRules) != len(bRules) {
			return false
		}

		for x, r := range aRules {
			if r.Path != bRules[x].Path {
				return false
			}
			if r.Backend.ID != bRules[x].Backend.ID {
				return false
			}
		}
	}
	return true
}

// PutPathRulesForHost adds path rules for a single hostname.
// This function ensures the invariants of the GCEURLMap are maintained.
// It will log if an invariant violation was found and reconciled.
// TODO(rramkumar): Surface an error instead of logging.
func (g *GCEURLMap) PutPathRulesForHost(hostname string, pathRules []PathRule) {
	// Convert the path rules to a map to filter out two equal paths
	// Note that the if two paths are equal, the one later in the list is the winner.
	seen := make(map[string]bool)
	var uniquePathRules []PathRule
	for x := len(pathRules) - 1; x >= 0; x-- {
		pathRule := pathRules[x]

		if seen[pathRule.Path] {
			glog.V(4).Infof("Path %q was duplicated", pathRule.Path)
			continue
		} else {
			seen[pathRule.Path] = true
		}

		uniquePathRules = append([]PathRule{pathRule}, uniquePathRules...)
	}
	if g.HostExists(hostname) {
		glog.V(4).Infof("Overwriting path rules for host %v", hostname)
	}
	g.HostRules[hostname] = uniquePathRules
}

// AllServicePorts return a list of all ServicePorts contained in the GCEURLMap.
func (g *GCEURLMap) AllServicePorts() (svcPorts []ServicePort) {
	if g == nil {
		return nil
	}

	if g.DefaultBackend != nil {
		svcPorts = append(svcPorts, *g.DefaultBackend)
	}

	for _, rules := range g.HostRules {
		for _, rule := range rules {
			svcPorts = append(svcPorts, rule.Backend)
		}
	}

	return
}

// HostExists returns true if the given hostname is specified in the GCEURLMap.
func (g *GCEURLMap) HostExists(hostname string) bool {
	if g == nil {
		return false
	}

	_, ok := g.HostRules[hostname]
	return ok
}

// PathExists returns true if the given path exists for the given hostname.
// It will also return the backend associated with that path.
func (g *GCEURLMap) PathExists(hostname, path string) (bool, ServicePort) {
	if g == nil {
		return false, ServicePort{}
	}

	pathRules, ok := g.HostRules[hostname]
	if !ok {
		return ok, ServicePort{}
	}
	for _, pathRule := range pathRules {
		if pathRule.Path == path {
			return true, pathRule.Backend
		}
	}
	return false, ServicePort{}
}

// String dumps a readable version of the GCEURLMap.
func (g *GCEURLMap) String() string {
	if g == nil {
		return ""
	}

	msg := ""
	for host, rules := range g.HostRules {
		msg += fmt.Sprintf("%v\n", host)
		for _, rule := range rules {
			msg += fmt.Sprintf("\t%v: ", rule.Path)
			msg += fmt.Sprintf("%+v\n", rule.Backend)
		}
	}
	msg += fmt.Sprintf("Default Backend: %+v", g.DefaultBackend)
	return msg
}
