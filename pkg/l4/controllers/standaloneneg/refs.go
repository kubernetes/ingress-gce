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

type parsedForwardingRule struct {
	key     *meta.Key
	rawName string
}

func (c *Controller) parseForwardingRuleKeys(frNamesStr string, svcLogger klog.Logger) ([]parsedForwardingRule, []error) {
	if frNamesStr == "" {
		return nil, nil
	}
	var parsedRules []parsedForwardingRule
	var errs []error
	seenKeys := sets.New[meta.Key]()
	frNames := strings.Split(frNamesStr, ",")
	for _, frName := range frNames {
		frName = strings.TrimSpace(frName)
		if frName == "" {
			continue
		}
		var frKey *meta.Key
		if strings.Contains(frName, "/") {
			resourceID, err := cloud.ParseResourceURL(frName)
			if err != nil {
				svcLogger.Error(err, "Failed to parse forwarding rule reference URL", "frRef", frName)
				errs = append(errs, fmt.Errorf("failed to parse forwarding rule reference URL %s: %w", frName, err))
				continue
			}
			frKey = resourceID.Key
		} else {
			frKey = meta.RegionalKey(frName, c.ctx.Cloud.Region())
		}
		// The same forwarding rule can be referenced multiple times, e.g. by name and by URL.
		if seenKeys.Has(*frKey) {
			svcLogger.V(2).Info("Skipping duplicate forwarding rule reference", "frRef", frName)
			continue
		}
		seenKeys.Insert(*frKey)
		parsedRules = append(parsedRules, parsedForwardingRule{key: frKey, rawName: frName})
	}
	return parsedRules, errs
}

func parsedFRNames(frs []parsedForwardingRule) []string {
	names := make([]string, len(frs))
	for i, fr := range frs {
		names[i] = fr.rawName
	}
	return names
}

func sortParsedFRs(frs []parsedForwardingRule) {
	sort.Slice(frs, func(i, j int) bool {
		return frs[i].rawName < frs[j].rawName
	})
}
