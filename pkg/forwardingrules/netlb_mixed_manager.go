package forwardingrules

import (
	"errors"
	"fmt"
	"strings"
	"time"

	api_v1 "k8s.io/api/core/v1"
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

type Namer interface {
	L4ForwardingRule(namespace, name, protocol string) string
}

type MixedManagerELB struct {
	Namer    Namer
	Provider *ForwardingRules
	Recorder record.EventRecorder
	Logger   logr.Logger

	Service *api_v1.Service
}

type EnsureELBConfig struct {
	// BackendServiceLink to the L3 (UNDEFINED) Protocol Backend Service.
	BackendServiceLink string
	IP                 string
}

type EnsureELBResult struct {
	UDPFwdRule *composite.ForwardingRule
	TCPFwdRule *composite.ForwardingRule
	IPManaged  bool
	SyncStatus utils.ResourceSyncStatus
}

// EnsureIPv4 will try to create or update forwarding rules for mixed protocol service.
func (m *MixedManagerELB) EnsureIPv4(cfg *EnsureELBConfig) (*EnsureELBResult, error) {
	svcPorts := m.Service.Spec.Ports
	needsMixed := NeedsTCP(svcPorts) && NeedsUDP(svcPorts)
	if !needsMixed {
		return nil, fmt.Errorf("MixedManagerELB shouldn't be used to ensure single protocol forwarding rules to be backwards compatible")
	}

	var tcpErr, udpErr error
	var tcpSync, udpSync utils.ResourceSyncStatus
	res := &EnsureELBResult{
		SyncStatus: utils.ResourceResync,
	}

	res.TCPFwdRule, tcpSync, tcpErr = m.ensure(cfg, "TCP")
	res.UDPFwdRule, udpSync, udpErr = m.ensure(cfg, "UDP")

	res.SyncStatus = tcpSync || udpSync
	err := errors.Join(tcpErr, udpErr)

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
func (m *MixedManagerELB) ensure(cfg *EnsureELBConfig, protocol string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	name := m.name(protocol)
	start := time.Now()
	log := m.Logger.
		WithValues("forwardingRuleName", name).
		WithValues("protocol", protocol).V(2)
	log.Info("Ensuring external forwarding rule for L4 NetLB Service", "backendServiceLink", cfg.BackendServiceLink)
	defer func() {
		log.Info("Finished ensuring external forwarding rule for L4 NetLB Service", "timeTaken", time.Since(start))
	}()

	existing, err := m.Provider.Get(name)
	if err != nil {
		log.Error(err, "Provider.Get returned error")
		return nil, utils.ResourceResync, err
	}

	wanted, err := m.buildWanted(cfg, name, protocol)
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

func (m *MixedManagerELB) recreate(wanted *composite.ForwardingRule) error {
	if err := m.Provider.Delete(wanted.Name); err != nil {
		return err
	}

	if err := m.Provider.Create(wanted); err != nil {
		return err
	}

	return nil
}

func (m *MixedManagerELB) buildWanted(cfg *EnsureELBConfig, name, protocol string) (*composite.ForwardingRule, error) {
	const version = meta.VersionGA
	const scheme = string(cloud.SchemeExternal)
	protocol = strings.ToUpper(protocol)
	if protocol != "TCP" && protocol != "UDP" {
		return nil, fmt.Errorf("Unknown protocol %s, expected TCP or UDP", protocol)
	}

	svcKey := utils.ServiceKeyFunc(m.Service.Namespace, m.Service.Name)
	desc, err := utils.MakeL4LBServiceDescription(svcKey, cfg.IP, version, false, utils.XLB)
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
		IPAddress:           cfg.IP,
		IPProtocol:          protocol,
		Ports:               ports,
		PortRange:           portRange,
		LoadBalancingScheme: scheme,
		BackendService:      cfg.BackendServiceLink,
		NetworkTier:         netTier.ToGCEValue(),
	}, nil
}

func (m *MixedManagerELB) getAfterUpdate(name string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	found, err := m.Provider.Get(name)
	if err != nil {
		return nil, utils.ResourceUpdate, err
	}
	if found == nil {
		return nil, utils.ResourceUpdate, fmt.Errorf("Forwarding rule %s not found", name)
	}

	return found, utils.ResourceUpdate, nil
}

func (m *MixedManagerELB) DeleteIPv4() error {
	tcpErr := m.delete("tcp")
	udpErr := m.delete("udp")

	return errors.Join(tcpErr, udpErr)
}

func (m *MixedManagerELB) delete(protocol string) error {
	name := m.name(protocol)
	return m.Provider.Delete(name)
}

func (m *MixedManagerELB) name(protocol string) string {
	return m.Namer.L4ForwardingRule(
		m.Service.Namespace, m.Service.Name, strings.ToLower(protocol),
	)
}

func (m *MixedManagerELB) recordf(messageFmt string, args ...any) {
	m.Recorder.Eventf(m.Service, api_v1.EventTypeNormal, events.SyncIngress, messageFmt, args)
}
