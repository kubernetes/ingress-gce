package optimized

import "k8s.io/ingress-gce/pkg/composite"

// APIOperations is a struct that contains the operations that need to be performed later to get desired state of Forwarding Rules.
type APIOperations struct {
	Create []*composite.ForwardingRule
	Update []*composite.ForwardingRule
	Delete []string
}

// Equal is a function that compares two forwarding rules.
// It returns true if they are equal, false otherwise.
// The error is returned if the comparison fails.
type Equal func(got, want *composite.ForwardingRule) (bool, error)

// Patchable is a function that checks if got can be patched to want.
// It returns true if they can be patched along with the diff of the updated fields.
// Otherwise it returns false and nil.
type Patchable func(got, want *composite.ForwardingRule) (bool, *composite.ForwardingRule)

// Compare compares two lists of forwarding rules and returns the operations
// that need to be performed to reconcile the differences.
//
// equal is used to determine if two Forwarding Rules are equal.
// patchable checks if two Forwarding Rules can be patched.
// Both of these are parameters as equal and patchable funcs are IP specific.
func Compare(got, want []*composite.ForwardingRule, equal Equal, patchable Patchable) (*APIOperations, error) {
	// We use maps to have O(1) lookups.
	gotMap := keyByNameMap(got)
	wantMap := keyByNameMap(want)

	// Update existing Forwarding Rules (both in "got" and "want")
	//
	// This handles both update and recreate (Delete and then Create) operations
	// and this is why we need to return all operations (Create, Update, Delete)
	// in the operations struct.
	ops, err := updateOps(gotMap, wantMap, equal, patchable)
	if err != nil {
		return nil, err
	}

	// We add other delete and create operations:
	// Delete is when there are forwarding rules in "got" that are not in "want".
	ops.Delete = append(ops.Delete, deleteOps(gotMap, wantMap)...)
	// Create is when there are forwarding rules in "want" that are not in "got".
	ops.Create = append(ops.Create, createOps(gotMap, wantMap)...)

	return ops, nil
}

func keyByNameMap(frs []*composite.ForwardingRule) map[string]*composite.ForwardingRule {
	m := make(map[string]*composite.ForwardingRule, len(frs))
	for _, fr := range frs {
		m[fr.Name] = fr
	}
	return m
}

func updateOps(gotMap, wantMap map[string]*composite.ForwardingRule, equal Equal, patchable Patchable) (*APIOperations, error) {
	ops := &APIOperations{}

	for name, got := range gotMap {
		want, ok := wantMap[name]
		if !ok {
			continue
		}

		if eq, err := equal(got, want); err != nil {
			return nil, err
		} else if eq {
			continue
		}

		if patch, diff := patchable(got, want); patch {
			ops.Update = append(ops.Update, diff)
			continue
		}

		// If we reach here, it means that the forwarding rule needs to be recreated.
		ops.Delete = append(ops.Delete, name)
		ops.Create = append(ops.Create, want)
	}

	return ops, nil
}

func createOps(got, want map[string]*composite.ForwardingRule) []*composite.ForwardingRule {
	var ops []*composite.ForwardingRule
	for name, fr := range want {
		if _, ok := got[name]; !ok {
			ops = append(ops, fr)
		}
	}
	return ops
}

func deleteOps(got, want map[string]*composite.ForwardingRule) []string {
	var ops []string
	for name := range got {
		if _, ok := want[name]; !ok {
			ops = append(ops, name)
		}
	}
	return ops
}
