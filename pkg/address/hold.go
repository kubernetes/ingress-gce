package address

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/annotations"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"
)

type HoldConfig struct {
	Cloud                 *gce.Cloud
	Recorder              record.EventRecorder
	Logger                klog.Logger
	Service               *api_v1.Service
	ExistingRules         []*composite.ForwardingRule
	ForwardingRuleDeleter ForwardingRuleDeleter
}

type ForwardingRuleDeleter interface {
	Delete(name string) error
}

type HoldResult struct {
	IP      string
	Managed IPAddressType
	Release func() error
}

// HoldExternalIPv4 will determine which IP to use for forwarding rules
// and will hold it for future forwarding rules. After binding
// IP to a forwarding rule call Release to prevent leaks.
func HoldExternalIPv4(cfg HoldConfig) (HoldResult, error) {
	var err error
	res := HoldResult{
		Release: func() error { return nil },
	}
	log := cfg.Logger.WithName("HoldIPv4")

	// external specific
	subnet := ""
	name := utils.LegacyForwardingRuleName(cfg.Service)

	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then requestedIP = "".
	rule := pickForwardingRuleToInferIP(cfg.ExistingRules)
	res.IP, err = IPv4ToUse(cfg.Cloud, cfg.Recorder, cfg.Service, rule, subnet)
	if err != nil {
		log.Error(err, "IPv4ToUse for service returned error")
		return res, err
	}

	// We can't use manager for legacy networks
	if cfg.Cloud.IsLegacyNetwork() {
		return res, nil
	}

	netTier, isFromAnnotation := annotations.NetworkTier(cfg.Service)
	nm := types.NamespacedName{
		Namespace: cfg.Service.Namespace,
		Name:      cfg.Service.Name,
	}.String()

	addrMgr := NewManager(
		cfg.Cloud, nm, cfg.Cloud.Region(),
		subnet, name, res.IP,
		cloud.SchemeExternal, netTier, IPv4Version, cfg.Logger,
	)

	// If network tier annotation in Service Spec is present
	// check if it matches network tiers from forwarding rule and external ip Address.
	// If they do not match, tear down the existing resources with the wrong tier.
	if isFromAnnotation {
		if err := tearDownRulesIfNetworkTierMismatch(cfg.ForwardingRuleDeleter, cfg.ExistingRules, netTier); err != nil {
			log.Error(err, "TearDownRulesIfNetworkTierMismatch returned error")
			return res, err
		}

		if err := addrMgr.TearDownAddressIPIfNetworkTierMismatch(); err != nil {
			log.Error(err, "TearDownAddressIPIfNetworkTierMismatch returned error")
			return res, err
		}
	}

	res.IP, res.Managed, err = addrMgr.HoldAddress()
	if err != nil {
		log.Error(err, "HoldAddress returned error")
		return res, err
	}

	res.Release = func() error {
		return addrMgr.ReleaseAddress()
	}
	return res, nil
}

// pickForwardingRuleToInferIP will pick first non nil forwarding rule
func pickForwardingRuleToInferIP(existingRules []*composite.ForwardingRule) *composite.ForwardingRule {
	for _, rule := range existingRules {
		if rule != nil && rule.IPAddress != "" {
			return rule
		}
	}
	return nil
}

func tearDownRulesIfNetworkTierMismatch(deleter ForwardingRuleDeleter, existingRules []*composite.ForwardingRule, tier cloud.NetworkTier) error {
	for _, rule := range existingRules {
		if rule == nil {
			continue
		}
		tierMatches := rule.NetworkTier == tier.ToGCEValue()
		if tierMatches {
			continue
		}

		if err := deleter.Delete(rule.Name); err != nil {
			return err
		}
	}
	return nil
}
