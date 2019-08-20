package cmconfig

import (
	"fmt"
	"reflect"
	"strings"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// Config holds configmap based configurations.
type Config struct {
	EnableASM                   bool
	ASMServiceNEGSkipNamespaces []string
}

const (
	trueValue  = "true"
	falseValue = "false"

	enableASM         = "enable-asm"
	asmSkipNamespaces = "asm-skip-namespaces"
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
func (c *Config) LoadValue(m map[string]string) error {
	var errList []error
	for k, v := range m {
		if k == enableASM {
			if v == trueValue {
				c.EnableASM = true
			} else if v == falseValue {
				c.EnableASM = false
			} else {
				errList = append(errList, fmt.Errorf("The map provided a unvalid value for field: %s, value: %s, valid values are: %s/%s", k, v, trueValue, falseValue))
			}
		} else if k == asmSkipNamespaces {
			c.ASMServiceNEGSkipNamespaces = strings.Split(v, ",")
		} else {
			errList = append(errList, fmt.Errorf("The map contains a unknown key-value pair: %s:%s", k, v))
		}
	}
	return utilerrors.NewAggregate(errList)
}
