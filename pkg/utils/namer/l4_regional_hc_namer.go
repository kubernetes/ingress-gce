package namer

import (
	"strings"
)

const (
	regionalHcSuffix = "regional"
)

// L4RegionalHealthCheckNamer is a  namer use only for health check and firewall resources
// it suffixes firewall rule name with '-regional' string
type L4RegionalHealthCheckNamer struct {
	l4namer L4ResourcesNamer
}

// NewL4RegionalHealthCheckNamer returns new instance of L4RegionalHealthCheckNamer
func NewL4RegionalHealthCheckNamer(l4namer L4ResourcesNamer) *L4RegionalHealthCheckNamer {
	l4rhcn := &L4RegionalHealthCheckNamer{
		l4namer: l4namer,
	}
	return l4rhcn
}

// L4HealthCheck returns the name of the L4 LB Healthcheck and the associated firewall rule.
// For shared health checks the firewall rule name is suffixed with '-regional' string
func (l4rhcn *L4RegionalHealthCheckNamer) L4HealthCheck(namespace, name string, shared bool) (string, string) {
	hc, hcfw := l4rhcn.l4namer.L4HealthCheck(namespace, name, shared)
	if !shared {
		return hc, hcfw
	}
	return hc, strings.Join([]string{hcfw, regionalHcSuffix}, "-")
}
