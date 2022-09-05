package forwardingrules

import (
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
	"k8s.io/legacy-cloud-providers/gce"
)

type ForwardingRules struct {
	cloud   *gce.Cloud
	version meta.Version
	scope   meta.KeyType
}

func New(cloud *gce.Cloud, version meta.Version, scope meta.KeyType) *ForwardingRules {
	return &ForwardingRules{
		cloud:   cloud,
		version: version,
		scope:   scope,
	}
}

func (frc *ForwardingRules) Create(forwardingRule *composite.ForwardingRule) error {
	key, err := frc.createKey(forwardingRule.Name)
	if err != nil {
		klog.Errorf("Failed to create key for creating forwarding rule %s, err: %v", forwardingRule.Name, err)
		return nil
	}
	return composite.CreateForwardingRule(frc.cloud, key, forwardingRule)
}

func (frc *ForwardingRules) Get(name string) (*composite.ForwardingRule, error) {
	key, err := frc.createKey(name)
	if err != nil {
		return nil, fmt.Errorf("Failed to create key for fetching forwarding rule %s, err: %w", name, err)
	}
	fr, err := composite.GetForwardingRule(frc.cloud, key, frc.version)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return nil, fmt.Errorf("Failed to get existing forwarding rule %s, err: %w", name, err)
	}
	return fr, nil
}

func (frc *ForwardingRules) Delete(name string) error {
	key, err := frc.createKey(name)
	if err != nil {
		return fmt.Errorf("Failed to create key for deleting forwarding rule %s, err: %w", name, err)
	}
	err = composite.DeleteForwardingRule(frc.cloud, key, frc.version)
	if utils.IgnoreHTTPNotFound(err) != nil {
		return fmt.Errorf("Failed to delete forwarding rule %s, err: %w", name, err)
	}
	return nil
}

func (frc *ForwardingRules) createKey(name string) (*meta.Key, error) {
	return composite.CreateKey(frc.cloud, name, frc.scope)
}
