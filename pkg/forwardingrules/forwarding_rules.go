package forwardingrules

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type ForwardingRules struct {
	cloud   *gce.Cloud
	version meta.Version
	scope   meta.KeyType

	logger klog.Logger
}

func New(cloud *gce.Cloud, version meta.Version, scope meta.KeyType, logger klog.Logger) *ForwardingRules {
	return &ForwardingRules{
		cloud:   cloud,
		version: version,
		scope:   scope,
		logger:  logger.WithName("ForwardingRules"),
	}
}

func (frc *ForwardingRules) Create(forwardingRule *composite.ForwardingRule) error {
	key, err := frc.createKey(forwardingRule.Name)
	if err != nil {
		frc.logger.Error(err, "Failed to create key for creating forwarding rule", "forwardingRuleName", forwardingRule.Name)
		return nil
	}
	return composite.CreateForwardingRule(frc.cloud, key, forwardingRule, frc.logger)
}

func (frc *ForwardingRules) Patch(forwardingRule *composite.ForwardingRule) error {
	key, err := frc.createKey(forwardingRule.Name)
	if err != nil {
		frc.logger.Error(err, "Failed to create key for creating forwarding rule", "forwardingRuleName", forwardingRule.Name)
		return nil
	}
	return composite.PatchForwardingRule(frc.cloud, key, forwardingRule, frc.logger)
}

func (frc *ForwardingRules) Get(name string) (*composite.ForwardingRule, error) {
	key, err := frc.createKey(name)
	if err != nil {
		return nil, fmt.Errorf("Failed to create key for fetching forwarding rule %s, err: %w", name, err)
	}
	fr, err := composite.GetForwardingRule(frc.cloud, key, frc.version, frc.logger)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return nil, fmt.Errorf("Failed to get existing forwarding rule %s, err: %w", name, err)
	}
	return fr, nil
}

// List will list all of the Forwarding Rules in GCE matching the filter.
func (frc *ForwardingRules) List(filter *filter.F) ([]*composite.ForwardingRule, error) {
	key, err := frc.createKey("")
	if err != nil {
		return nil, fmt.Errorf("failed to create key for listing forwarding rules, err: %w", err)
	}

	frs, err := composite.ListForwardingRules(frc.cloud, key, frc.version, frc.logger, filter)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return nil, fmt.Errorf("failed to list forwarding rules with filter %v: %w", filter, err)
	}
	return frs, nil
}

func (frc *ForwardingRules) Delete(name string) error {
	key, err := frc.createKey(name)
	if err != nil {
		return fmt.Errorf("Failed to create key for deleting forwarding rule %s, err: %w", name, err)
	}
	err = composite.DeleteForwardingRule(frc.cloud, key, frc.version, frc.logger)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return fmt.Errorf("Failed to delete forwarding rule %s, err: %w", name, err)
	}
	return nil
}

func (frc *ForwardingRules) createKey(name string) (*meta.Key, error) {
	return composite.CreateKey(frc.cloud, name, frc.scope)
}
