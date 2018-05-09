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
	DefaultBackend ServicePort
	// hostRules is a mapping from hostnames to path rules for that host.
	hostRules map[string][]PathRule
}

// PathRule encapsulates the information for a single path -> backend mapping.
type PathRule struct {
	Path    string
	Backend ServicePort
}

// NewGCEURLMap returns an empty GCEURLMap
func NewGCEURLMap() *GCEURLMap {
	return &GCEURLMap{hostRules: make(map[string][]PathRule)}
}

// PutPathRulesForHost adds path rules for a single hostname.
// This function ensures the invariants of the GCEURLMap are maintained.
// It will log if an invariant violation was found and reconciled.
// TODO(rramkumar): Surface an error instead of logging.
func (g *GCEURLMap) PutPathRulesForHost(hostname string, pathRules []PathRule) {
	// Convert the path rules to a map to filter out two equal paths
	// Note that the if two paths are equal, the one later in the list is the winner.
	uniquePaths := make(map[string]PathRule)
	for _, pathRule := range pathRules {
		if _, ok := uniquePaths[pathRule.Path]; ok {
			glog.V(4).Infof("Equal paths (%v) for host %v. Using backend %+v", pathRule.Path, hostname, pathRule.Backend)
		}
		uniquePaths[pathRule.Path] = pathRule
	}
	uniquePathRules := make([]PathRule, 0)
	for _, pathRule := range uniquePaths {
		uniquePathRules = append(uniquePathRules, pathRule)
	}
	if g.HostExists(hostname) {
		glog.V(4).Infof("Overwriting path rules for host %v", hostname)
	}
	g.hostRules[hostname] = uniquePathRules
}

// AllRules returns every list of PathRule's for each hostname.
// Note: Return value is a copy to ensure invariants are not broken mistakenly.
// TODO(rramkumar): Build an iterator for this?
func (g *GCEURLMap) AllRules() map[string][]PathRule {
	retVal := g.hostRules
	return retVal
}

// AllServicePorts return a list of all ServicePorts contained in the GCEURLMap.
func (g *GCEURLMap) AllServicePorts() (svcPorts []ServicePort) {
	for _, rules := range g.hostRules {
		for _, rule := range rules {
			svcPorts = append(svcPorts, rule.Backend)
		}
	}
	if g.DefaultBackend != (ServicePort{}) {
		svcPorts = append(svcPorts, g.DefaultBackend)
	}
	return
}

// HostExists returns true if the given hostname is specified in the GCEURLMap.
func (g *GCEURLMap) HostExists(hostname string) bool {
	_, ok := g.hostRules[hostname]
	return ok
}

// PathExists returns true if the given path exists for the given hostname.
// It will also return the backend associated with that path.
func (g *GCEURLMap) PathExists(hostname, path string) (bool, ServicePort) {
	pathRules, ok := g.hostRules[hostname]
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
	msg := ""
	for host, rules := range g.hostRules {
		msg += fmt.Sprintf("%v\n", host)
		for _, rule := range rules {
			msg += fmt.Sprintf("\t%v: ", rule.Path)
			msg += fmt.Sprintf("%+v\n", rule.Backend)
		}
	}
	msg += fmt.Sprintf("Default Backend: %+v", g.DefaultBackend)
	return msg
}
