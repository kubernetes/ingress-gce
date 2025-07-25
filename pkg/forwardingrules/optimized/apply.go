package optimized

import (
	"fmt"

	"k8s.io/ingress-gce/pkg/composite"

	"golang.org/x/sync/errgroup"
)

// ForwardingRulesModifier is used to not create a direct dependency on forwardingrules
type ForwardingRulesModifier interface {
	Create(fr *composite.ForwardingRule) error
	Patch(fr *composite.ForwardingRule) error
	Delete(name string) error
}

// Apply given APIOperations to the Forwarding Rules using the given ForwardingRulesModifier.
//
// To avoid port collisions we need to make sure all deletes happen before patches, and all patches happen between creates.
// There are no dependencies between individual operations at each step, so this func runs individual operations in each step in parallel.
func Apply(repo ForwardingRulesModifier, ops *APIOperations) error {
	if err := applyDeletes(repo, ops.Delete); err != nil {
		return err
	}

	if err := applyPatches(repo, ops.Update); err != nil {
		return err
	}

	if err := applyCreates(repo, ops.Create); err != nil {
		return err
	}

	return nil
}

func applyDeletes(repo ForwardingRulesModifier, names []string) error {
	g := new(errgroup.Group)
	for _, name := range names {
		name := name
		g.Go(func() error {
			return repo.Delete(name)
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to delete forwarding rules: %w", err)
	}
	return nil
}

func applyPatches(repo ForwardingRulesModifier, patches []*composite.ForwardingRule) error {
	g := new(errgroup.Group)
	for _, fr := range patches {
		fr := fr
		g.Go(func() error {
			return repo.Patch(fr)
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to patch forwarding rules: %w", err)
	}
	return nil
}

func applyCreates(repo ForwardingRulesModifier, frs []*composite.ForwardingRule) error {
	g := new(errgroup.Group)
	for _, fr := range frs {
		fr := fr
		g.Go(func() error {
			return repo.Create(fr)
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to create forwarding rules: %w", err)
	}
	return nil
}
