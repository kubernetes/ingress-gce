package address

import (
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/utils"
	"k8s.io/klog/v2"

	"k8s.io/cloud-provider-gcp/providers/gce"
)

type HoldConfig struct {
	Cloud                 *gce.Cloud
	Logger                klog.Logger
	Service               *api_v1.Service
	ExistingRules         []*composite.ForwardingRule
	ForwardingRuleDeleter ForwardingRuleDeleter

	// IP is the IPv4 address to hold, as returned by ExternalIPv4ToUse.
	// An empty IP means that a new address will be allocated.
	IP string
	// AddressName is the name of the static address reserved by the user that
	// IP comes from, as returned by ExternalIPv4ToUse. It is empty when the IP
	// does not come from a named address.
	AddressName string
}

type ForwardingRuleDeleter interface {
	Delete(name string) error
}

type HoldResult struct {
	IP      string
	Managed IPAddressType
	// Reserved reports whether IP is held by an address (reserved by the controller
	// or by the user) until Release is called. It is false in legacy networks, where
	// addresses can't be reserved. Forwarding rules using IP must only be deleted to
	// be re-created while it is true, see forwardingrules.CheckRecreateAllowed.
	Reserved bool
	Release  func() error
}

// ExternalIPv4ToUse determines which IPv4 address should be used by the external
// forwarding rules of the Service. If no forwarding rule has been established and
// no address is specified in the Service spec, the returned IP is "".
// See IPv4ToUse for the precedence rules.
//
// This is read-only: callers can use the result to decide whether the existing
// forwarding rules are already up to date before reserving the address with
// HoldExternalIPv4.
func ExternalIPv4ToUse(cloud *gce.Cloud, recorder record.EventRecorder, svc *api_v1.Service, existingRules []*composite.ForwardingRule) (ip, addressName string, err error) {
	// External forwarding rules don't use a subnet.
	const subnet = ""
	rule := pickForwardingRuleToInferIP(existingRules)
	return IPv4ToUse(cloud, recorder, svc, rule, subnet)
}

// ManagementTypeFromSpec reports, without calling the Compute API, whether the IPv4
// address of the Service is expected to be managed by the controller (IPAddrManaged)
// or brought by the user (IPAddrUnmanaged). addressName is the name of the static
// address referenced by the Service annotation, as returned by ExternalIPv4ToUse.
//
// This is a best-effort inference from the Service spec only, meant for syncs that
// don't reserve the address (and so can't observe it like Manager.HoldAddress does).
// It differs from Manager.HoldAddress only when the spec and the cloud disagree, e.g.
// when .Spec.LoadBalancerIP references an IP that is not reserved, or when a static
// address that was once referenced by the spec is still in use after the reference
// has been removed.
func ManagementTypeFromSpec(svc *api_v1.Service, addressName string) IPAddressType {
	if addressName != "" || svc.Spec.LoadBalancerIP != "" {
		return IPAddrUnmanaged
	}
	return IPAddrManaged
}

// HoldExternalIPv4 will hold the IP selected with ExternalIPv4ToUse for future
// forwarding rules. After binding IP to a forwarding rule call Release to prevent leaks.
func HoldExternalIPv4(cfg HoldConfig) (HoldResult, error) {
	var err error
	res := HoldResult{
		IP:      cfg.IP,
		Release: func() error { return nil },
	}
	log := cfg.Logger.WithName("HoldIPv4")

	// external specific
	subnet := ""
	name := utils.LegacyForwardingRuleName(cfg.Service)

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
		subnet, name, cfg.AddressName, cfg.IP,
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
	res.Reserved = true

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
