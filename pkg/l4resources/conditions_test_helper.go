package l4resources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	api_v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/ingress-gce/pkg/forwardingrules"
	"k8s.io/ingress-gce/pkg/l4conditions"
)

// conditionMatcher represents the expected values for a condition
type conditionMatcher struct {
	Type    string
	Status  metav1.ConditionStatus
	Reason  string
	Message string // if empty, will not be checked
}

// assertConditionsMatch verifies that the actual conditions match the expected conditions
func assertConditionsMatch(t *testing.T, want []conditionMatcher, got []metav1.Condition) {
	t.Helper()

	var wantConditions []metav1.Condition
	for _, c := range want {
		wantConditions = append(wantConditions, metav1.Condition{
			Type:    c.Type,
			Status:  c.Status,
			Reason:  c.Reason,
			Message: c.Message,
		})
	}

	sortFn := func(a, b metav1.Condition) bool {
		return a.Type < b.Type
	}

	diff := cmp.Diff(wantConditions, got,
		cmpopts.SortSlices(sortFn),
		cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime", "ObservedGeneration"),
	)

	if diff != "" {
		t.Errorf("Conditions mismatch (-want +got):\n%s", diff)
	}
}

// Helper to build expected  IPv4 conditions
func expectedL4LBIPv4Conditions(ports []api_v1.ServicePort, bsName, frName, hcName, fwName, hcFwName string, isNetLB bool) []conditionMatcher {
	conditions := []conditionMatcher{
		{
			Type:    l4conditions.BackendServiceConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: bsName,
		},
		{
			Type:    l4conditions.HealthCheckConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: hcName,
		},
		{
			Type:    l4conditions.FirewallRuleConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: fwName,
		},
		{
			Type:    l4conditions.FirewallHealthCheckConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: hcFwName,
		},
	}

	if forwardingrules.NeedsMixed(ports) {
		if isNetLB {
			conditions = append(conditions,
				conditionMatcher{
					Type:    l4conditions.TCPForwardingRuleConditionType,
					Status:  metav1.ConditionTrue,
					Reason:  l4conditions.ConditionReason,
					Message: frName,
				},
				conditionMatcher{
					Type:    l4conditions.UDPForwardingRuleConditionType,
					Status:  metav1.ConditionTrue,
					Reason:  l4conditions.ConditionReason,
					Message: frName,
				},
			)
		} else {
			conditions = append(conditions,
				conditionMatcher{
					Type:    l4conditions.L3ForwardingRuleConditionType,
					Status:  metav1.ConditionTrue,
					Reason:  l4conditions.ConditionReason,
					Message: frName,
				},
			)
		}
	} else if forwardingrules.NeedsTCP(ports) {
		conditions = append(conditions,
			conditionMatcher{
				Type:    l4conditions.TCPForwardingRuleConditionType,
				Status:  metav1.ConditionTrue,
				Reason:  l4conditions.ConditionReason,
				Message: frName,
			},
		)
	} else if forwardingrules.NeedsUDP(ports) {
		conditions = append(conditions,
			conditionMatcher{
				Type:    l4conditions.UDPForwardingRuleConditionType,
				Status:  metav1.ConditionTrue,
				Reason:  l4conditions.ConditionReason,
				Message: frName,
			},
		)
	}

	return conditions
}

// Helper to build expected L4 LB IPv6 Only conditions
func expectedL4LBIPv6Conditions(ports []api_v1.ServicePort, bsName, ipv6FrName, hcName, ipv6FwName, hcFwIPv6Name string, isNetLB bool) []conditionMatcher {
	conditions := []conditionMatcher{
		{
			Type:    l4conditions.BackendServiceConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: bsName,
		},
		{
			Type:    l4conditions.HealthCheckConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: hcName,
		},
		{
			Type:    l4conditions.IPv6FirewallRuleConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: ipv6FwName,
		},
		{
			Type:    l4conditions.FirewallHealthCheckIPv6ConditionType,
			Status:  metav1.ConditionTrue,
			Reason:  l4conditions.ConditionReason,
			Message: hcFwIPv6Name,
		},
	}

	if forwardingrules.NeedsMixed(ports) {
		if isNetLB {
			conditions = append(conditions,
				conditionMatcher{
					Type:    l4conditions.TCPIPv6ForwardingRuleConditionType,
					Status:  metav1.ConditionTrue,
					Reason:  l4conditions.ConditionReason,
					Message: ipv6FrName,
				},
				conditionMatcher{
					Type:    l4conditions.UDPIPv6ForwardingRuleConditionType,
					Status:  metav1.ConditionTrue,
					Reason:  l4conditions.ConditionReason,
					Message: ipv6FrName,
				},
			)
		} else {
			conditions = append(conditions,
				conditionMatcher{
					Type:    l4conditions.L3IPv6ForwardingRuleConditionType,
					Status:  metav1.ConditionTrue,
					Reason:  l4conditions.ConditionReason,
					Message: ipv6FrName,
				},
			)
		}
	} else if forwardingrules.NeedsTCP(ports) {
		conditions = append(conditions,
			conditionMatcher{
				Type:    l4conditions.TCPIPv6ForwardingRuleConditionType,
				Status:  metav1.ConditionTrue,
				Reason:  l4conditions.ConditionReason,
				Message: ipv6FrName,
			},
		)
	} else if forwardingrules.NeedsUDP(ports) {
		conditions = append(conditions,
			conditionMatcher{
				Type:    l4conditions.UDPIPv6ForwardingRuleConditionType,
				Status:  metav1.ConditionTrue,
				Reason:  l4conditions.ConditionReason,
				Message: ipv6FrName,
			},
		)
	}

	return conditions
}

func mergeConditions(a, b []conditionMatcher) []conditionMatcher {
	existingIndices := make(map[string]int, len(a))
	for i, c := range a {
		existingIndices[c.Type] = i
	}

	for _, cond := range b {
		if _, found := existingIndices[cond.Type]; !found {
			a = append(a, cond)
		}
	}
	return a
}

// Helper to build expected  dual-stack conditions
func expectedL4LBDualStackConditions(ports []api_v1.ServicePort, bsName, ipv4FrName, ipv6FrName, hcName, ipv4FwName, ipv6FwName, hcFwName, hcFwIPv6Name string, isNetLB bool) []conditionMatcher {
	return mergeConditions(expectedL4LBIPv4Conditions(ports, bsName, ipv4FrName, hcName, ipv4FwName, hcFwName, isNetLB),
		expectedL4LBIPv6Conditions(ports, bsName, ipv6FrName, hcName, ipv6FwName, hcFwIPv6Name, isNetLB))
}
