package forwardingrules

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/address"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
)

const (
	// maxForwardedPorts is the maximum number of ports that can be specified in an Forwarding Rule
	maxForwardedPorts = 5
)

// Namer is used to get names for forwarding rules.
type Namer interface {
	L4ForwardingRule(namespace, name, protocol string) string
}

// Provider is the interface for the ForwardingRules provider.
// We can't use the *ForwardingRules directly, since L4NetLB uses interface.
//
// It is assumed that delete doesn't return 404 errors when forwarding rule doesn't exist.
type Provider interface {
	Get(name string) (*composite.ForwardingRule, error)
	Create(forwardingRule *composite.ForwardingRule) error
	Delete(name string) error
	Patch(forwardingRule *composite.ForwardingRule) error
}

// MixedManagerNetLB is responsible for Ensuring and Deleting Forwarding Rules
// for mixed protocol NetLBs.
type MixedManagerNetLB struct {
	Namer    Namer
	Provider Provider
	Recorder record.EventRecorder
	Logger   logr.Logger
	Cloud    *gce.Cloud

	Service *api_v1.Service
}

// EnsureNetLBConfig contains fields specific to ensuring proper Forwarding Rules
// for mixed protocol NetLBs.
type EnsureNetLBConfig struct {
	// BackendServiceLink to the L3 (UNDEFINED) Protocol Backend Service.
	BackendServiceLink string
}

// EnsureNetLBResult contains relevant results for Ensure method
type EnsureNetLBResult struct {
	UDPFwdRule *composite.ForwardingRule
	TCPFwdRule *composite.ForwardingRule
	IPManaged  address.IPAddressType
	SyncStatus utils.ResourceSyncStatus
}

// EnsureIPv4 will try to create or update forwarding rules for mixed protocol service.
func (m *MixedManagerNetLB) EnsureIPv4(cfg EnsureNetLBConfig) (EnsureNetLBResult, error) {
	svcPorts := m.Service.Spec.Ports
	res := EnsureNetLBResult{
		SyncStatus: utils.ResourceResync,
	}

	if !NeedsMixed(svcPorts) {
		return res, fmt.Errorf("MixedManagerELB shouldn't be used to ensure single protocol forwarding rules to be backwards compatible")
	}

	existing, err := m.AllRules()
	if err != nil {
		return res, err
	}

	addressHandle, err := m.Address(existing)
	if err != nil {
		return res, err
	}
	defer func() {
		err = addressHandle.Release()
		if err != nil {
			m.Logger.Error(err, "failed to release address reservation, possibly causing an orphan")
		}
	}()
	res.IPManaged = addressHandle.Managed

	// We need to delete legacy named forwarding rule to avoid port collisions
	if err := m.deleteLegacy(); err != nil {
		return res, err
	}

	var tcpErr, udpErr error
	var tcpSync, udpSync utils.ResourceSyncStatus
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		res.TCPFwdRule, tcpSync, tcpErr = m.ensure(cfg, existing.TCP, "TCP", addressHandle.IP)
	}()
	go func() {
		defer wg.Done()
		res.UDPFwdRule, udpSync, udpErr = m.ensure(cfg, existing.UDP, "UDP", addressHandle.IP)
	}()

	wg.Wait()
	if tcpSync == utils.ResourceUpdate || udpSync == utils.ResourceUpdate {
		res.SyncStatus = utils.ResourceUpdate
	}
	err = errors.Join(tcpErr, udpErr)

	return res, err
}

// ensure has similar implementation to the L4NetLB.ensureIPv4ForwardingRule,
// but can use multiple names for fwd rule.
// This will:
// * compare existing rule to wanted
// * if doesnt exist 	-> create
// * if equal 			-> do nothing
// * if can be patched 	-> patch
// * else 				-> delete and recreate
func (m *MixedManagerNetLB) ensure(cfg EnsureNetLBConfig, existing *composite.ForwardingRule, protocol, ip string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	name := m.name(protocol)
	start := time.Now()
	log := m.Logger.
		WithValues("forwardingRuleName", name).
		WithValues("protocol", protocol).V(2)
	log.Info("Ensuring external forwarding rule for L4 NetLB Service", "backendServiceLink", cfg.BackendServiceLink)
	defer func() {
		log.Info("Finished ensuring external forwarding rule for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	wanted, err := m.buildWanted(cfg, name, protocol, ip)
	if err != nil {
		log.Error(err, "buildWanted returned error")
		return nil, utils.ResourceResync, err
	}

	// Exists
	if existing == nil {
		if err := m.Provider.Create(wanted); err != nil {
			log.Error(err, "Provider.Create returned error")
			return nil, utils.ResourceUpdate, err
		}
		return m.getAfterUpdate(name)
	}

	// Can't update
	if networkMismatch := existing.NetworkTier != wanted.NetworkTier; networkMismatch {
		resource := fmt.Sprintf("Forwarding rule (%v)", name)
		networkTierMismatchErr := utils.NewNetworkTierErr(resource, wanted.NetworkTier, wanted.NetworkTier)
		return nil, utils.ResourceUpdate, networkTierMismatchErr
	}

	// Equal
	if equal, err := EqualIPv4(existing, wanted); err != nil {
		log.Error(err, "EqualIPV4 returned error")
		return nil, utils.ResourceResync, err
	} else if equal {
		return existing, utils.ResourceResync, err
	}

	// Patchable
	if patchable, filtered := PatchableIPv4(existing, wanted); patchable {
		if err := m.Provider.Patch(filtered); err != nil {
			return nil, utils.ResourceUpdate, err
		}
		return m.getAfterUpdate(name)
	}

	// Recreate
	if err := m.recreate(wanted); err != nil {
		return nil, utils.ResourceResync, err
	}
	return m.getAfterUpdate(name)
}

func (m *MixedManagerNetLB) recreate(wanted *composite.ForwardingRule) error {
	if err := m.Provider.Delete(wanted.Name); err != nil {
		return err
	}

	if err := m.Provider.Create(wanted); err != nil {
		return err
	}

	return nil
}

func (m *MixedManagerNetLB) buildWanted(cfg EnsureNetLBConfig, name, protocol, ip string) (*composite.ForwardingRule, error) {
	const version = meta.VersionGA
	const scheme = string(cloud.SchemeExternal)
	protocol = strings.ToUpper(protocol)
	if protocol != "TCP" && protocol != "UDP" {
		return nil, fmt.Errorf("Unknown protocol %s, expected TCP or UDP", protocol)
	}

	svcKey := utils.ServiceKeyFunc(m.Service.Namespace, m.Service.Name)
	desc, err := utils.MakeL4LBServiceDescription(svcKey, ip, version, false, utils.XLB)
	if err != nil {
		return nil, fmt.Errorf("Failed to compute description for forwarding rule %s, err: %w", name, err)
	}

	ports := GetPorts(m.Service.Spec.Ports, api_v1.Protocol(protocol))
	var portRange string
	if len(ports) > maxForwardedPorts {
		portRange = utils.MinMaxPortRange(ports)
		ports = nil
	}

	netTier, _ := utils.GetNetworkTier(m.Service)

	return &composite.ForwardingRule{
		Name:                name,
		Description:         desc,
		IPAddress:           ip,
		IPProtocol:          protocol,
		Ports:               ports,
		PortRange:           portRange,
		LoadBalancingScheme: scheme,
		BackendService:      cfg.BackendServiceLink,
		NetworkTier:         netTier.ToGCEValue(),
	}, nil
}

func (m *MixedManagerNetLB) getAfterUpdate(name string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	found, err := m.Provider.Get(name)
	if err != nil {
		return nil, utils.ResourceUpdate, err
	}
	if found == nil {
		return nil, utils.ResourceUpdate, fmt.Errorf("Forwarding rule %s not found", name)
	}

	return found, utils.ResourceUpdate, nil
}

type ELBManagedRules struct {
	// TCP forwarding rule with '-tcp-' in a name
	TCP *composite.ForwardingRule
	// UDP forwarding rule with '-udp-' in a name
	UDP *composite.ForwardingRule
	// Legacy named forwarding rule (staring with 'a')
	Legacy *composite.ForwardingRule
}

// AllRules returns all forwarding rules for a service specified in the manager
func (m *MixedManagerNetLB) AllRules() (ELBManagedRules, error) {
	var wg sync.WaitGroup
	var tcp, udp, legacy *composite.ForwardingRule
	var tcpErr, udpErr, legacyErr error

	wg.Add(3)
	go func() {
		defer wg.Done()
		tcp, tcpErr = m.Provider.Get(m.name("tcp"))
	}()
	go func() {
		defer wg.Done()
		udp, udpErr = m.Provider.Get(m.name("udp"))
	}()
	go func() {
		defer wg.Done()
		legacy, legacyErr = m.Provider.Get(m.nameLegacy())
	}()
	wg.Wait()

	return ELBManagedRules{
		TCP: tcp, UDP: udp, Legacy: legacy,
	}, errors.Join(tcpErr, udpErr, legacyErr)
}

type AddressResult struct {
	IP      string
	Managed address.IPAddressType
	Release func() error
}

func (m *MixedManagerNetLB) Address(rules ELBManagedRules) (AddressResult, error) {
	res := AddressResult{
		Managed: address.IPAddrUndefined,
		Release: func() error { return nil },
	}
	var err error
	rule := pickForwardingRuleToInferIP(rules)
	// Determine IP which will be used for this LB. If no forwarding rule has been established
	// or specified in the Service spec, then requestedIP = "".
	res.IP, err = address.IPv4ToUse(m.Cloud, m.Recorder, m.Service, rule, "")
	if err != nil {
		m.Logger.Error(err, "ipv4AddrToUse for service returned error")
		return res, err
	}

	if m.Cloud.IsLegacyNetwork() {
		return res, nil
	}

	netTier, isFromAnnotation := utils.GetNetworkTier(m.Service)
	nm := types.NamespacedName{Namespace: m.Service.Namespace, Name: m.Service.Name}.String()
	name := m.nameLegacy()
	addrMgr := address.NewManager(
		m.Cloud, nm, m.Cloud.Region() /*subnetURL = */, "",
		name, res.IP, cloud.SchemeExternal, netTier,
		address.IPv4Version, m.Logger)

	// If network tier annotation in Service Spec is present
	// check if it matches network tiers from forwarding rule and external ip Address.
	// If they do not match, tear down the existing resources with the wrong tier.
	if isFromAnnotation {
		if err := m.tearDownResourcesWithWrongNetworkTier(rules, netTier, addrMgr); err != nil {
			m.Logger.Error(err, "failed to tear down resources with wrong network tier")
			return res, err
		}
	}

	res.IP, res.Managed, err = addrMgr.HoldAddress()
	if err != nil {
		return res, err
	}
	res.Release = func() error {
		return addrMgr.ReleaseAddress()
	}

	return res, nil
}

func pickForwardingRuleToInferIP(rules ELBManagedRules) *composite.ForwardingRule {
	switch {
	case rules.Legacy != nil:
		return rules.Legacy
	case rules.TCP != nil:
		return rules.TCP
	case rules.UDP != nil:
		return rules.UDP
	default:
		return nil
	}
}

func (m *MixedManagerNetLB) DeleteIPv4() error {
	tcpErr := m.delete("tcp")
	udpErr := m.delete("udp")

	return errors.Join(tcpErr, udpErr)
}

func (m *MixedManagerNetLB) delete(protocol string) error {
	name := m.name(protocol)
	return m.Provider.Delete(name)
}

// We need to clean up existing forwarding rule so that there isn't a port collision.
// This means deleting forwarding rule with legacy name - starting with 'a'
// and using protocol specific names identical to those used by ILB.
func (m *MixedManagerNetLB) deleteLegacy() error {
	return m.Provider.Delete(m.nameLegacy())
}

func (m *MixedManagerNetLB) name(protocol string) string {
	return m.Namer.L4ForwardingRule(
		m.Service.Namespace, m.Service.Name, strings.ToLower(protocol),
	)
}

func (m *MixedManagerNetLB) nameLegacy() string {
	return utils.LegacyForwardingRuleName(m.Service)
}

func (m *MixedManagerNetLB) tearDownResourcesWithWrongNetworkTier(rules ELBManagedRules, netTier cloud.NetworkTier, addressMgr *address.Manager) error {
	var tcpErr, udpErr error
	if rules.TCP != nil && rules.TCP.NetworkTier != netTier.ToGCEValue() {
		tcpErr = m.delete("TCP")
	}
	if rules.UDP != nil && rules.UDP.NetworkTier != netTier.ToGCEValue() {
		udpErr = m.delete("UDP")
	}
	addressErr := addressMgr.TearDownAddressIPIfNetworkTierMismatch()

	return errors.Join(tcpErr, udpErr, addressErr)
}

func (m *MixedManagerNetLB) recordf(messageFmt string, args ...any) {
	m.Recorder.Eventf(m.Service, api_v1.EventTypeNormal, events.SyncIngress, messageFmt, args)
}
