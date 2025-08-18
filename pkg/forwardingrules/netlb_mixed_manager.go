package forwardingrules

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/cloud-provider-gcp/providers/gce"
	"k8s.io/ingress-gce/pkg/address"
	"k8s.io/ingress-gce/pkg/annotations"
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

// EnsureNetLBResult contains relevant results for Ensure method
type EnsureNetLBResult struct {
	UDPFwdRule *composite.ForwardingRule
	TCPFwdRule *composite.ForwardingRule
	IPManaged  address.IPAddressType
	SyncStatus utils.ResourceSyncStatus
}

// EnsureIPv4 will try to create or update forwarding rules for mixed protocol service.
func (m *MixedManagerNetLB) EnsureIPv4(backendServiceLink string) (EnsureNetLBResult, error) {
	m.Logger = m.Logger.WithName("MixedManagerNetLB")
	res := EnsureNetLBResult{
		SyncStatus: utils.ResourceResync,
	}

	existing, err := m.AllRules()
	if err != nil {
		return res, err
	}

	addressHandle, err := address.HoldExternalIPv4(address.HoldConfig{
		Cloud:                 m.Cloud,
		Recorder:              m.Recorder,
		Logger:                m.Logger,
		Service:               m.Service,
		ExistingRules:         []*composite.ForwardingRule{existing.TCP, existing.UDP},
		ForwardingRuleDeleter: m.Provider,
	})
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

	var tcpErr, udpErr error
	var tcpSync, udpSync utils.ResourceSyncStatus
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		res.TCPFwdRule, tcpSync, tcpErr = m.ensure(existing.TCP, backendServiceLink, "TCP", addressHandle.IP)
	}()
	go func() {
		defer wg.Done()
		res.UDPFwdRule, udpSync, udpErr = m.ensure(existing.UDP, backendServiceLink, "UDP", addressHandle.IP)
	}()

	wg.Wait()

	res.SyncStatus = syncStatus(tcpSync, udpSync)
	err = errors.Join(tcpErr, udpErr)

	return res, err
}

func syncStatus(statuses ...utils.ResourceSyncStatus) utils.ResourceSyncStatus {
	for _, s := range statuses {
		if s == utils.ResourceUpdate {
			return utils.ResourceUpdate
		}
	}
	return utils.ResourceResync
}

// ensure has similar implementation to the L4NetLB.ensureIPv4ForwardingRule,
// but can use multiple names for fwd rule.
// This will:
// * delete forwarding rule if it's not needed, otherwise:
// * compare existing rule to wanted
// * if doesnt exist 	-> create
// * if equal 			-> do nothing
// * if can be patched 	-> patch
// * else 				-> delete and recreate
func (m *MixedManagerNetLB) ensure(existing *composite.ForwardingRule, backendServiceLink, protocol, ip string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	name := m.name(protocol)
	if existing != nil {
		name = existing.Name
	}
	start := time.Now()
	log := m.Logger.
		WithName("ensure").
		WithValues("forwardingRuleName", name).
		WithValues("protocol", protocol)
	log.Info("Ensuring external forwarding rule for L4 NetLB Service", "backendServiceLink", backendServiceLink)
	defer func() {
		log.Info("Finished ensuring external forwarding rule for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	wanted, err := m.buildWanted(backendServiceLink, name, protocol, ip)
	if err != nil {
		log.Error(err, "buildWanted returned error")
		return nil, utils.ResourceResync, err
	}

	// Forwarding Rule not needed
	if wanted == nil {
		if existing != nil {
			err := m.Provider.Delete(name)
			return nil, utils.ResourceUpdate, err
		}
		return nil, utils.ResourceResync, nil
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
		m.recordEventf("ForwardingRule %s patched", name)
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
	m.recordEventf("ForwardingRule %s deleted", wanted.Name)

	if err := m.Provider.Create(wanted); err != nil {
		return err
	}
	m.recordEventf("ForwardingRule %s re-created", wanted.Name)

	return nil
}

func (m *MixedManagerNetLB) buildWanted(backendServiceLink, name, protocol, ip string) (*composite.ForwardingRule, error) {
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
	// We don't need a Forwarding Rule for this protocol
	if len(ports) == 0 {
		return nil, nil
	}

	var portRange string
	if len(ports) > maxForwardedPorts {
		portRange = utils.MinMaxPortRange(ports)
		ports = nil
	}

	netTier, _ := annotations.NetworkTier(m.Service)

	return &composite.ForwardingRule{
		Name:                name,
		Description:         desc,
		IPAddress:           ip,
		IPProtocol:          protocol,
		Ports:               ports,
		PortRange:           portRange,
		LoadBalancingScheme: scheme,
		BackendService:      backendServiceLink,
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

// NetLBManagedRules contains rules managed by NetLB loadbalancer. At most one will have legacy name.
type NetLBManagedRules struct {
	// TCP forwarding rule with '-tcp-' in a name or legacy named (with 'a')
	TCP *composite.ForwardingRule
	// UDP forwarding rule with '-udp-' in a name or legacy named (with 'a')
	UDP *composite.ForwardingRule
}

// AllRules returns all forwarding rules for a service specified in the manager.
// At most there will be two Forwarding rules, one for TCP and one for UDP.
// For new LBs we prefer to use v2 names, not legacy (the one that starts with "a").
// However if a Forwarding Rule already exists with legacy name, we should use it to avoid recreation,
// which will cause traffic drop.
func (m *MixedManagerNetLB) AllRules() (NetLBManagedRules, error) {
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

	if err := errors.Join(tcpErr, udpErr, legacyErr); err != nil {
		return NetLBManagedRules{}, fmt.Errorf("failed to get forwarding rules: %w", err)
	}

	return managedRules(tcp, udp, legacy)
}

// Legacy will return a legacy named Forwarding Rule if such exists for the LB.
// Otherwise returns nil.
func Legacy(rules NetLBManagedRules) *composite.ForwardingRule {
	if rules.TCP != nil && isLegacy(rules.TCP) {
		return rules.TCP
	}
	if rules.UDP != nil && isLegacy(rules.UDP) {
		return rules.UDP
	}
	return nil
}

func isLegacy(fr *composite.ForwardingRule) bool {
	return strings.HasPrefix(fr.Name, "a")
}

func managedRules(frs ...*composite.ForwardingRule) (NetLBManagedRules, error) {
	// The key in the map will be the protocol. API returns them in UPPERCASE, either `TCP` or `UDP`
	const tcpKey, udpKey = "TCP", "UDP"
	m := make(map[string]*composite.ForwardingRule)

	for _, fr := range frs {
		if fr == nil {
			continue
		}
		if found, ok := m[fr.IPProtocol]; ok {
			return NetLBManagedRules{}, fmt.Errorf("duplicate forwarding rule for protocol %q: %v and %v", fr.IPProtocol, found, fr)
		}

		m[fr.IPProtocol] = fr
	}

	return NetLBManagedRules{
		TCP: m[tcpKey],
		UDP: m[udpKey],
	}, nil
}

// DeleteIPv4 will try to delete ALL forwarding rules for mixed protocol NetLB service.
// This includes "-tcp-", "-udp-" and legacy named ones.
func (m *MixedManagerNetLB) DeleteIPv4() error {
	var wg sync.WaitGroup
	var tcpErr, udpErr, legacyErr error

	wg.Add(3)
	go func() {
		defer wg.Done()
		tcpErr = m.delete("tcp")
	}()
	go func() {
		defer wg.Done()
		udpErr = m.delete("udp")
	}()
	go func() {
		defer wg.Done()
		legacyErr = m.deleteLegacy()
	}()

	wg.Wait()
	return errors.Join(tcpErr, udpErr, legacyErr)
}

// DeleteExclusivelyManaged will delete resources that can only be managed by MixedManager.
//
// This is meant to be called only in situations when MixedProtocol has been disabled, but has some leftover managed resources.
// We check for existing ones, before deletion to prevent creating empty audit logs.
func (m *MixedManagerNetLB) DeleteExclusivelyManaged(existing NetLBManagedRules) error {
	var wg sync.WaitGroup
	var tcpErr, udpErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		if existing.TCP != nil {
			tcpErr = m.Provider.Delete(existing.TCP.Name)
		}
	}()
	go func() {
		defer wg.Done()
		if existing.UDP != nil {
			udpErr = m.Provider.Delete(existing.UDP.Name)
		}
	}()

	wg.Wait()
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

func (m *MixedManagerNetLB) recordEventf(messageFmt string, args ...any) {
	m.Recorder.Eventf(m.Service, api_v1.EventTypeNormal, events.SyncIngress, messageFmt, args)
}
