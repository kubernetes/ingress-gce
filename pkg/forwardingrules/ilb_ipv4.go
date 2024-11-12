package forwardingrules

import (
	"errors"
	"fmt"

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

type EnsureConfig struct {
	Namer    Namer
	Provider *ForwardingRules
	Recorder record.EventRecorder
	Logger   logr.Logger

	BackendServiceLink string
	SubnetworkURL      string
	NetworkURL         string
	IP                 string
	AllowGlobalAccess  bool

	Service *api_v1.Service
}

type EnsureResult struct {
	UDPFwdRule *composite.ForwardingRule
	TCPFwdRule *composite.ForwardingRule
	SyncStatus utils.ResourceSyncStatus
}

type Namer interface {
	L4ForwardingRule(namespace string, name string, protocol string) string
}

func EnsureILBIPv4(cfg *EnsureConfig) (*EnsureResult, error) {
	cfg.Logger.V(2).Info("forwardingrules.EnsureILBIPv4")
	var tcpErr, udpErr error
	var tcpSync, udpSync utils.ResourceSyncStatus

	svcPorts := cfg.Service.Spec.Ports
	res := &EnsureResult{
		SyncStatus: utils.ResourceResync,
	}

	if NeedsTCP(svcPorts) {
		cfg.Logger.V(2).Info("forwardingrules.EnsureILBIPv4: Needs TCP")
		res.TCPFwdRule, tcpSync, tcpErr = ensure(cfg, "tcp")
	} else {
		cfg.Logger.V(2).Info("forwardingrules.EnsureILBIPv4: Delete TCP")
		tcpErr = delete(cfg, "TCP")
	}

	if NeedsUDP(svcPorts) {
		cfg.Logger.V(2).Info("forwardingrules.EnsureILBIPv4: Needs UDP")
		res.UDPFwdRule, udpSync, udpErr = ensure(cfg, "udp")
	} else {
		cfg.Logger.V(2).Info("forwardingrules.EnsureILBIPv4: Delete UDP")
		udpErr = delete(cfg, "UDP")
	}

	if udpSync == utils.ResourceUpdate || tcpSync == utils.ResourceUpdate {
		res.SyncStatus = utils.ResourceUpdate
	}

	return res, errors.Join(tcpErr, udpErr)
}

func ensure(cfg *EnsureConfig, protocol string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	name := cfg.Namer.L4ForwardingRule(cfg.Service.Namespace, cfg.Service.Name, protocol)
	const resync = utils.ResourceResync
	const update = utils.ResourceUpdate

	existing, err := cfg.Provider.Get(name)
	if err != nil {
		return nil, resync, err
	}

	wanted, err := buildWanted(cfg, name, protocol)
	if err != nil {
		return nil, resync, err
	}

	if existing == nil {
		if err := cfg.Provider.Create(wanted); err != nil {
			return nil, update, err
		}
		cfg.recordf("ForwardingRule %s created", name)
		return cfg.find(name)
	}

	if equal, err := Equal(existing, wanted); err != nil {
		return nil, resync, err
	} else if equal {
		// Nothing to do
		cfg.Logger.V(2).Info("forwardingrules.ensure: Skipping update of unchanged forwarding rule")
		return existing, resync, err
	}

	cfg.Logger.V(2).Info(
		"forwardingrules.ensure: Forwarding rule changed.",
		"existing", fmt.Sprintf("%+v", existing),
		"wanted", fmt.Sprintf("%+v", wanted),
		"diff", cmp.Diff(existing, wanted),
	)

	if patchable, filtered := Patchable(existing, wanted); patchable {
		if err := cfg.Provider.Patch(filtered); err != nil {
			return nil, update, err
		}
		cfg.recordf("ForwardingRule %s patched", name)
		return cfg.find(name)
	}

	// Needs to be recreated
	if err := cfg.Provider.Delete(name); err != nil {
		return nil, update, err
	}
	cfg.recordf("ForwardingRule %s deleted", name)

	if err := cfg.Provider.Create(wanted); err != nil {
		return nil, update, err
	}
	cfg.recordf("ForwardingRule %s recreated", name)

	return cfg.find(name)
}

func (ec *EnsureConfig) find(name string) (*composite.ForwardingRule, utils.ResourceSyncStatus, error) {
	found, err := ec.Provider.Get(name)
	if err != nil {
		return nil, utils.ResourceUpdate, err
	}

	if found == nil {
		return nil, utils.ResourceUpdate, fmt.Errorf("Forwarding Rule %s not found", name)
	}

	return found, utils.ResourceUpdate, err
}

func buildWanted(cfg *EnsureConfig, name, protocol string) (*composite.ForwardingRule, error) {
	const version = meta.VersionGA
	const scheme = string(cloud.SchemeInternal)

	svcKey := utils.ServiceKeyFunc(cfg.Service.Namespace, cfg.Service.Name)
	desc, err := utils.MakeL4LBServiceDescription(svcKey, cfg.IP, version, false, utils.ILB)
	if err != nil {
		return nil, err
	}

	return &composite.ForwardingRule{
		Name:                name,
		IPAddress:           cfg.IP,
		Ports:               utils.GetPorts(cfg.Service.Spec.Ports),
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

func delete(cfg *EnsureConfig, protocol string) error {
	name := cfg.Namer.L4ForwardingRule(cfg.Service.Namespace, cfg.Service.Name, protocol)

	return utils.IgnoreHTTPNotFound(cfg.Provider.Delete(name))
}

func (ec *EnsureConfig) recordf(messageFmt string, args ...interface{}) {
	ec.Recorder.Eventf(ec.Service, api_v1.EventTypeNormal, events.SyncIngress, messageFmt, args)
}
