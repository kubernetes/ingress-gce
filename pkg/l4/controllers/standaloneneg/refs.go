/*
Copyright 2026 The Kubernetes Authors.

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

package standaloneneg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

// forwardingRulesLimit is the maximum number of forwarding rules of a service.
const forwardingRulesLimit = 10

// frRef is a forwarding rule reference from the service annotation.
type frRef struct {
	key *meta.Key
	// raw is the reference as written in the annotation: a name or a URL.
	raw string
}

// parseFRRefs parses the comma separated forwarding rule references of the
// annotation. A reference is either a forwarding rule name, in the given
// region, or a forwarding rule URL. Duplicate references are skipped.
func parseFRRefs(annotation, region string, svcLogger klog.Logger) ([]frRef, []error) {
	if annotation == "" {
		return nil, nil
	}
	var refs []frRef
	var errs []error
	seenKeys := sets.New[meta.Key]()
	for _, raw := range strings.Split(annotation, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var key *meta.Key
		if strings.Contains(raw, "/") {
			resourceID, err := cloud.ParseResourceURL(raw)
			if err != nil {
				svcLogger.Error(err, "Failed to parse forwarding rule reference URL", "frRef", raw)
				errs = append(errs, fmt.Errorf("failed to parse forwarding rule reference URL %s: %w", raw, err))
				continue
			}
			key = resourceID.Key
		} else {
			key = meta.RegionalKey(raw, region)
		}
		// The same forwarding rule can be referenced multiple times, e.g. by name and by URL.
		if seenKeys.Has(*key) {
			svcLogger.V(2).Info("Skipping duplicate forwarding rule reference", "frRef", raw)
			continue
		}
		seenKeys.Insert(*key)
		refs = append(refs, frRef{key: key, raw: raw})
	}
	return refs, errs
}

// sortFRRefs sorts the references alphabetically, so that the forwarding
// rules and the status ingress IPs are deterministic between syncs.
func sortFRRefs(refs []frRef) {
	sort.Slice(refs, func(i, j int) bool {
		return refs[i].raw < refs[j].raw
	})
}

func frRefNames(refs []frRef) []string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = ref.raw
	}
	return names
}
