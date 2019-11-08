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
	rconfigPtr := reflect.ValueOf(c)
	rconfig := reflect.Indirect(rconfigPtr)

	for k, v := range m {
		_, ok := rconfig.Type().FieldByName(k)
		if ok {
			field := rconfig.FieldByName(k)
			fieldType := field.Kind()
			if fieldType == reflect.Bool {
				if v == sTrue {
					field.SetBool(true)
				} else if v == sFalse {
					field.SetBool(false)
				} else {
					msg := fmt.Sprintf("The map provided a unvalid value for field: %s, value: %s, valid values are: %s/%s", k, v, sTrue, sFalse)
					klog.Errorf(msg)
					recordWarning(msg)
				}
			} else if fieldType == reflect.String {
				field.SetString(v)
			} else if fieldType == reflect.Slice {
				if field.Type().Elem().Kind() == reflect.String {
					values := strings.Split(v, ",")
					field.Set(reflect.ValueOf(values))
				} else {
					msg := fmt.Sprintf("config struct using a unsupported slice type: %s, only support []string", field.Elem().Kind().String())
					klog.Errorf(msg)
					recordWarning(msg)
				}
			} else {
				msg := fmt.Sprintf("config struct using a unsupported type: %s, only support bool/string.", fieldType)
				klog.Errorf(msg)
				recordWarning(msg)
			}
		} else {
			msg := fmt.Sprintf("The map contains a unknown key-value pair: %s:%s", k, v)
			klog.Errorf(msg)
			recordWarning(msg)
		}
	}
}
