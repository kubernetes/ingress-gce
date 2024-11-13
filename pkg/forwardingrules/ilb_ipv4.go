package forwardingrules

import (
	"errors"
	"fmt"
	"strings"

	api_v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils"
)

type Manager struct {
	Namer    Namer
	Provider *ForwardingRules
	Recorder record.EventRecorder
	Logger   logr.Logger

	Service *api_v1.Service
}

type EnsureConfig struct {
	BackendServiceLink string
	SubnetworkURL      string
	NetworkURL         string
	IP                 string
	AllowGlobalAccess  bool
}

type EnsureResult struct {
	UDPFwdRule *composite.ForwardingRule
	TCPFwdRule *composite.ForwardingRule
	SyncStatus utils.ResourceSyncStatus
}

type Namer interface {
	L4ForwardingRule(namespace string, name string, protocol string) string
}

func (m *Manager) EnsureILBIPv4(cfg *EnsureConfig) (*EnsureResult, error) {
	m.info("forwardingrules.EnsureILBIPv4")
	var tcpErr, udpErr error
	var tcpSync, udpSync utils.ResourceSyncStatus

	svcPorts := m.Service.Spec.Ports
	res := &EnsureResult{
		SyncStatus: utils.ResourceResync,
	}

	if NeedsTCP(svcPorts) {
		m.info("forwardingrules.EnsureILBIPv4: Needs TCP")
		res.TCPFwdRule, tcpSync, tcpErr = m.ensure(cfg, "tcp")
	} else {
		m.info("forwardingrules.EnsureILBIPv4: Delete TCP")
		tcpErr = m.delete("tcp")
	}

	if NeedsUDP(svcPorts) {
		m.info("forwardingrules.EnsureILBIPv4: Needs UDP")
		res.UDPFwdRule, udpSync, udpErr = m.ensure(cfg, "udp")
	} else {
		m.info("forwardingrules.EnsureILBIPv4: Delete UDP")
		udpErr = m.delete("udp")
	}

	if udpSync == utils.ResourceUpdate || tcpSync == utils.ResourceUpdate {
		res.SyncStatus = utils.ResourceUpdate
	}

	return res, errors.Join(tcpErr, udpErr)
}

func (m *Manager) DeleteILBIPv4() error {
	tcpErr := m.delete("tcp")
	udpErr := m.delete("udp")

	return errors.Join(tcpErr, udpErr)
}

func (m *Manager) ensure(cfg *EnsureConfig, protocol string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	name := m.name(protocol)
	const resync = utils.ResourceResync
	const update = utils.ResourceUpdate

	existing, err := m.Provider.Get(name)
	if err != nil {
		return nil, resync, err
	}

	wanted, err := m.buildWanted(cfg, name, protocol)
	if err != nil {
		return nil, resync, err
	}

	if existing == nil {
		if err := m.Provider.Create(wanted); err != nil {
			return nil, update, err
		}
		m.recordf("ForwardingRule %s created", name)
		return m.find(name)
	}

	if equal, err := Equal(existing, wanted); err != nil {
		return nil, resync, err
	} else if equal {
		// Nothing to do
		m.info("forwardingrules.ensure: Skipping update of unchanged forwarding rule")
		return existing, resync, err
	}

	m.info(
		"forwardingrules.ensure: Forwarding rule changed.",
		"existing", fmt.Sprintf("%+v", existing),
		"wanted", fmt.Sprintf("%+v", wanted),
		"diff", cmp.Diff(existing, wanted),
	)

	if patchable, filtered := Patchable(existing, wanted); patchable {
		if err := m.Provider.Patch(filtered); err != nil {
			return nil, update, err
		}
		m.recordf("ForwardingRule %s patched", name)
		return m.find(name)
	}

	// Needs to be recreated
	if err := m.Provider.Delete(name); err != nil {
		return nil, update, err
	}
	m.recordf("ForwardingRule %s deleted", name)

	if err := m.Provider.Create(wanted); err != nil {
		return nil, update, err
	}
	m.recordf("ForwardingRule %s recreated", name)

	return m.find(name)
}

func (m *Manager) find(name string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	found, err := m.Provider.Get(name)
	if err != nil {
		return nil, utils.ResourceUpdate, err
	}

	if found == nil {
		return nil, utils.ResourceUpdate, fmt.Errorf("Forwarding Rule %s not found", name)
	}

	return found, utils.ResourceUpdate, err
}

func (m *Manager) buildWanted(cfg *EnsureConfig, name, protocol string) (*composite.ForwardingRule, error) {
	const version = meta.VersionGA
	const scheme = string(cloud.SchemeInternal)

	svcKey := utils.ServiceKeyFunc(m.Service.Namespace, m.Service.Name)
	desc, err := utils.MakeL4LBServiceDescription(svcKey, cfg.IP, version, false, utils.ILB)
	if err != nil {
		return nil, err
	}

	return &composite.ForwardingRule{
		Name:                name,
		IPAddress:           cfg.IP,
		Ports:               GetPorts(m.Service.Spec.Ports, api_v1.Protocol(strings.ToUpper(protocol))),
		IPProtocol:          protocol,
		LoadBalancingScheme: scheme,
		Subnetwork:          cfg.SubnetworkURL,
		Network:             cfg.NetworkURL,
		NetworkTier:         cloud.NetworkTierDefault.ToGCEValue(),
		Version:             version,
		BackendService:      cfg.BackendServiceLink,
		AllowGlobalAccess:   cfg.AllowGlobalAccess,
		Description:         desc,
	}, nil
}

func (m *Manager) delete(protocol string) error {
	name := m.name(protocol)

	return utils.IgnoreHTTPNotFound(m.Provider.Delete(name))
}

func (m *Manager) recordf(messageFmt string, args ...any) {
	m.Recorder.Eventf(m.Service, api_v1.EventTypeNormal, events.SyncIngress, messageFmt, args)
}

func (m *Manager) info(message string, keysAndValues ...any) {
	m.Logger.V(2).Info(message, keysAndValues...)
}

func (m *Manager) name(protocol string) string {
	return m.Namer.L4ForwardingRule(
		m.Service.Namespace, m.Service.Name, strings.ToLower(protocol),
	)
}
