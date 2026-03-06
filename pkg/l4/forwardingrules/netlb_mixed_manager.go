package forwardingrules

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/l4/address"
	"k8s.io/ingress-gce/pkg/l4/annotations"
	"k8s.io/ingress-gce/pkg/utils"

	"k8s.io/cloud-provider-gcp/providers/gce"

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

	addressHandle, err := address.HoldExternalIPv4(address.HoldConfig{
		Cloud:                 m.Cloud,
		Recorder:              m.Recorder,
		Logger:                m.Logger,
		Service:               m.Service,
		ExistingRules:         []*composite.ForwardingRule{existing.Legacy, existing.TCP, existing.UDP},
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
		res.TCPFwdRule, tcpSync, tcpErr = m.ensure(existing.TCP, backendServiceLink, "TCP", addressHandle.IP)
	}()
	go func() {
		defer wg.Done()
		res.UDPFwdRule, udpSync, udpErr = m.ensure(existing.UDP, backendServiceLink, "UDP", addressHandle.IP)
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
func (m *MixedManagerNetLB) ensure(existing *composite.ForwardingRule, backendServiceLink, protocol, ip string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	name := m.name(protocol)
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

// NetLBManagedRules contains rules managed by NetLB loadbalancer.
// Under normal conditions one of three results is possible:
// a) TCP and UDP both present - rules have been created for mixed protocol
// b) Legacy present - rule has been created for single protocol
// c) empty - nothing has been yet created
type NetLBManagedRules struct {
	// TCP forwarding rule with '-tcp-' in a name
	TCP *composite.ForwardingRule
	// UDP forwarding rule with '-udp-' in a name
	UDP *composite.ForwardingRule
	// Legacy named forwarding rule (staring with 'a')
	Legacy *composite.ForwardingRule
}

// AllRules returns all forwarding rules for a service specified in the manager
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

	return NetLBManagedRules{
		TCP: tcp, UDP: udp, Legacy: legacy,
	}, errors.Join(tcpErr, udpErr, legacyErr)
}

// DeleteIPv4 will try to delete forwarding rules for mixed protocol NetLB service.
func (m *MixedManagerNetLB) DeleteIPv4() error {
	var wg sync.WaitGroup
	var tcpErr, udpErr error
	wg.Add(2)

	go func() {
		defer wg.Done()
		tcpErr = m.delete("tcp")
	}()
	go func() {
		defer wg.Done()
		udpErr = m.delete("udp")
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
