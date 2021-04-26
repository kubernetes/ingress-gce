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

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/translator"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog"
)

// ensureComputeURLMap retrieves the current URLMap and overwrites it if incorrect. If the resource
// does not exist, the map is created.
func (l *L7) ensureComputeURLMap() error {
	if l.runtimeInfo.UrlMap == nil {
		return fmt.Errorf("cannot create urlmap without internal representation")
	}

	// Every update replaces the entire urlmap.
	// Use an empty name parameter since we only care about the scope
	// TODO: (shance) refactor this so we don't need an empty arg
	key, err := l.CreateKey("")
	if err != nil {
		return err
	}
	expectedMap := translator.ToCompositeURLMap(l.runtimeInfo.UrlMap, l.namer, key)
	key.Name = expectedMap.Name

	expectedMap.Version = l.Versions().UrlMap
	currentMap, err := composite.GetUrlMap(l.cloud, key, expectedMap.Version)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return err
	}

	if currentMap == nil {
		// Check for transitions between elb and ilb

		klog.V(2).Infof("Creating URLMap %q", expectedMap.Name)
		if err := composite.CreateUrlMap(l.cloud, key, expectedMap); err != nil {
			return fmt.Errorf("CreateUrlMap: %v", err)
		}
		l.recorder.Eventf(&l.ingress, apiv1.EventTypeNormal, events.SyncIngress, "UrlMap %q created", key.Name)
		l.um = expectedMap

		return nil
	}

	if mapsEqual(currentMap, expectedMap) {
		klog.V(4).Infof("URLMap for %q is unchanged", l)
		l.um = currentMap
		return nil
	}

	klog.V(2).Infof("Updating URLMap for %q", l)
	expectedMap.Fingerprint = currentMap.Fingerprint
	if err := composite.UpdateUrlMap(l.cloud, key, expectedMap); err != nil {
		return fmt.Errorf("UpdateURLMap: %v", err)
	}

	l.recorder.Eventf(&l.ingress, apiv1.EventTypeNormal, events.SyncIngress, "UrlMap %q updated", key.Name)
	l.um = expectedMap

	return nil
}

func (l *L7) ensureRedirectURLMap() error {
	feConfig := l.runtimeInfo.FrontendConfig
	isL7ILB := utils.IsGCEL7ILBIngress(&l.ingress)

	t := translator.NewTranslator(isL7ILB, l.namer)
	env := &translator.Env{FrontendConfig: feConfig, Ing: &l.ingress}

	name, namerSupported := l.namer.RedirectUrlMap()
	expectedMap := t.ToRedirectUrlMap(env, l.Versions().UrlMap)

	// Cannot enable for internal ingress
	if expectedMap != nil && isL7ILB {
		return fmt.Errorf("error: cannot enable HTTPS Redirects with L7 ILB")
	}

	// Cannot enable on older naming schemes
	if !namerSupported {
		if expectedMap != nil {
			return fmt.Errorf("error: cannot enable HTTPS Redirects with the V1 Ingress naming scheme.  Please recreate your ingress to use the newest naming scheme.")
		}
		return nil
	}

	key, err := l.CreateKey(name)
	if err != nil {
		return err
	}

	// Do not expect to have a RedirectUrlMap
	if expectedMap == nil {
		// Check if we need to GC
		status, ok := l.ingress.Annotations[annotations.RedirectUrlMapKey]
		if !ok || status == "" {
			return nil
		} else {
			if err := composite.DeleteUrlMap(l.cloud, key, l.Versions().UrlMap); err != nil {
				return err
			}
		}
		return nil
	}

	currentMap, err := composite.GetUrlMap(l.cloud, key, l.Versions().UrlMap)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return err
	}

	if currentMap == nil {
		if err := composite.CreateUrlMap(l.cloud, key, expectedMap); err != nil {
			return err
		}
	} else if compareRedirectUrlMaps(expectedMap, currentMap) {
		expectedMap.Fingerprint = currentMap.Fingerprint
		if err := composite.UpdateUrlMap(l.cloud, key, expectedMap); err != nil {
			return err
		}
	}

	l.redirectUm = expectedMap
	return nil
}

// compareRedirectUrlMaps() compares the fields specified on the url map by the frontendconfig and returns true
// if there's a diff, false otherwise
func compareRedirectUrlMaps(a, b *composite.UrlMap) bool {
	if a.DefaultUrlRedirect.HttpsRedirect != b.DefaultUrlRedirect.HttpsRedirect ||
		a.DefaultUrlRedirect.RedirectResponseCode != b.DefaultUrlRedirect.RedirectResponseCode {
		return true
	}
	return false
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
