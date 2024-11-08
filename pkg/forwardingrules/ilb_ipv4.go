package forwardingrules

import (
	"errors"

	api_v1 "k8s.io/api/core/v1"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"k8s.io/ingress-gce/pkg/composite"
	"k8s.io/ingress-gce/pkg/utils"
)

type EnsureConfig struct {
	Namer    Namer
	Provider ForwardingRules

	BackendServiceLink string
	SubnetworkURL      string
	NetworkURL         string
	IP                 string
	AllowGlobalAccess  bool

	Service api_v1.Service
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
	var tcpErr, udpErr error
	svcPorts := cfg.Service.Spec.Ports

	if NeedsTCP(svcPorts) {
		_, tcpErr = ensure(cfg, "TCP")
	} else {
		tcpErr = delete(cfg, "TCP")
	}

	if NeedsUDP(svcPorts) {
		_, udpErr = ensure(cfg, "UDP")
	} else {
		udpErr = delete(cfg, "UDP")
	}

	return nil, errors.Join(tcpErr, udpErr)
}

func ensure(cfg *EnsureConfig, protocol string) (*composite.ForwardingRule, error) {
	name := cfg.Namer.L4ForwardingRule(cfg.Service.Namespace, cfg.Service.Name, protocol)

	existing, err := cfg.Provider.Get(name)
	if err != nil {
		return existing, err
	}

	wanted, err := buildWanted(cfg, name, protocol)
	if err != nil {
		return existing, err
	}

	switch {
	case existing == nil:
		// Create wanted
	case Equal():
		continue
	case Patchable():
		// patch
	default: // Needs to be recreated
		// delete existing
		// create wanted
	}

	return nil, nil
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
	frName := cfg.Namer.L4ForwardingRule(cfg.Service.Namespace, cfg.Service.Name, protocol)

	return cfg.Provider.Delete(frName)
}
