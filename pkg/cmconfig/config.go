package cmconfig

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/klog"
)

// Config holds configmap based configurations.
type Config struct {
	EnableASM                   bool
	ASMServiceNEGSkipNamespaces []string
}

const (
	sTrue  = "true"
	sFalse = "false"

	sEnableASM                   = "enable-asm"
	sASMServiceNEGSkipNamespaces = "asm-skip-namespaces"
)

// NewConfig returns a Conifg instances with default values.
func NewConfig() Config {
	return Config{ASMServiceNEGSkipNamespaces: []string{"kube-system"}}
}

// Equals returns true if c equals to other.
func (c *Config) Equals(other *Config) bool {
	return reflect.DeepEqual(c, other)
}

// LoadValue loads configs from a map, it will ignore any unknow/unvalid field.
func (c *Config) LoadValue(m map[string]string, recordWarning func(msg string)) {
	for k, v := range m {
		if k == sEnableASM {
			if v == sTrue {
				c.EnableASM = true
			} else if v == sFalse {
				c.EnableASM = false
			} else {
				msg := fmt.Sprintf("The map provided a unvalid value for field: %s, value: %s, valid values are: %s/%s", k, v, sTrue, sFalse)
				klog.Errorf(msg)
				recordWarning(msg)
			}
		} else if k == sASMServiceNEGSkipNamespaces {
			c.ASMServiceNEGSkipNamespaces = strings.Split(v, ",")
		} else {
			msg := fmt.Sprintf("The map contains a unknown key-value pair: %s:%s", k, v)
			klog.Errorf(msg)
			recordWarning(msg)
		}
	}
}
