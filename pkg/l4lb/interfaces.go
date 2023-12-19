package l4lb

import "k8s.io/ingress-gce/pkg/composite"

// ForwardingRulesGetter is an interface which allows getting Google Cloud Forwarding Rules
type ForwardingRulesGetter interface {
	Get(name string) (*composite.ForwardingRule, error)
}
