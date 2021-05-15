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
	"strings"

	"k8s.io/klog"
)

// GCEURLMap is a simplified representation of a UrlMap somewhere
// in the middle of a compute.UrlMap and rules in an Ingress spec.
// This representation maintains three invariants/rules:
//       1. All hostnames are unique
//       2. All paths for a specific host are unique.
//       3. Adding paths for a hostname replaces existing for that host.
type GCEURLMap struct {
	DefaultBackend *ServicePort
	// HostRules is an ordered list of hostnames, path rule tuples.
	HostRules []HostRule
	// hosts is a map of existing hosts.
	hosts map[string]bool
}

// HostRule encapsulates the Hostname and its list of PathRules.
type HostRule struct {
	Hostname string
	Paths    []PathRule
}

// PathRule encapsulates the information for a single path -> backend mapping.
type PathRule struct {
	Path    string
	Backend ServicePort
}

// NewGCEURLMap returns an empty GCEURLMap
func NewGCEURLMap() *GCEURLMap {
	return &GCEURLMap{hosts: make(map[string]bool)}
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

	for i, aRules := range a.HostRules {
		bRules := b.HostRules[i]
		if aRules.Hostname != bRules.Hostname {
			return false
		}

		if len(aRules.Paths) != len(bRules.Paths) {
			return false
		}

		for i, aPath := range aRules.Paths {
			bPath := bRules.Paths[i]
			if aPath.Path != bPath.Path {
				return false
			}
			if aPath.Backend.ID != bPath.Backend.ID {
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
			klog.V(4).Infof("Path %q was duplicated", pathRule.Path)
			continue
		} else {
			seen[pathRule.Path] = true
		}

		uniquePathRules = append([]PathRule{pathRule}, uniquePathRules...)
	}
	hr := HostRule{
		Hostname: hostname,
		Paths:    uniquePathRules,
	}

	_, exists := g.hosts[hostname]
	if exists {
		klog.V(4).Infof("Overwriting path rules for host %v", hostname)
		g.deleteHost(hostname)
	}

	g.HostRules = append(g.HostRules, hr)
	g.hosts[hostname] = true
	return
}

// AllServicePorts return a list of all ServicePorts contained in the GCEURLMap.
func (g *GCEURLMap) AllServicePorts() (svcPorts []ServicePort) {

	uniqueServerPorts := make(map[ServicePortID]bool)
	if g.DefaultBackend != nil {
		svcPorts = append(svcPorts, *g.DefaultBackend)
		uniqueServerPorts[*&g.DefaultBackend.ID] = true
	}

	for _, rules := range g.HostRules {
		for _, rule := range rules.Paths {
			if !uniqueServerPorts[rule.Backend.ID] {
				svcPorts = append(svcPorts, rule.Backend)
				uniqueServerPorts[rule.Backend.ID] = true
			}
		}
	}

	return
}

func (g *GCEURLMap) deleteHost(hostname string) {
	// Iterate HostRules and remove any (should only be zero or one) with the provided hostname.
	for i := len(g.HostRules) - 1; i >= 0; i-- {
		if g.HostRules[i].Hostname == hostname {
			g.HostRules = append(g.HostRules[:i], g.HostRules[i+1:]...)
		}
	}
	delete(g.hosts, hostname)
}

// HostExists returns true if the given hostname is specified in the GCEURLMap.
func (g *GCEURLMap) HostExists(hostname string) bool {
	return g.hosts[hostname]
}

// PathExists returns true if the given path exists for the given hostname.
// It will also return the backend associated with that path.
func (g *GCEURLMap) PathExists(hostname, path string) (ServicePort, bool) {
	if !g.hosts[hostname] {
		return ServicePort{}, false
	}

	for _, h := range g.HostRules {
		if h.Hostname != hostname {
			continue
		}

		for _, p := range h.Paths {
			if p.Path == path {
				return p.Backend, true
			}
		}
	}

	return ServicePort{}, false
}

// String dumps a readable version of the GCEURLMap.
func (g *GCEURLMap) String() string {
	var b strings.Builder
	for _, hostRule := range g.HostRules {
		b.WriteString(fmt.Sprintf("%v\n", hostRule.Hostname))
		for _, rule := range hostRule.Paths {
			b.WriteString(fmt.Sprintf("\t%v: ", rule.Path))
			b.WriteString(fmt.Sprintf("%+v\n", rule.Backend))
		}
	}
	b.WriteString(fmt.Sprintf("Default Backend: %+v", g.DefaultBackend))
	return b.String()
}
