package cmconfig

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadValue(t *testing.T) {
	testcases := []struct {
		desc       string
		inputMap   map[string]string
		wantConfig Config
		wantLog    string
	}{
		{
			desc:       "empty map should give default config",
			inputMap:   map[string]string{},
			wantConfig: NewConfig(),
			wantLog:    "",
		},
		{
			desc:       "LoadValue should load values from a valid map",
			inputMap:   map[string]string{"enable-asm": "true", "asm-skip-namespaces": "name-space1,namespace2"},
			wantConfig: Config{EnableASM: true, ASMServiceNEGSkipNamespaces: []string{"name-space1", "namespace2"}},
			wantLog:    "",
		},
		{
			desc:       "LoadValue should return the default value if EnableASM has a unvalid value.",
			inputMap:   map[string]string{"enable-asm": "f"},
			wantConfig: Config{EnableASM: false, ASMServiceNEGSkipNamespaces: []string{"kube-system"}},
			wantLog:    "The map provided a unvalid value for field: enable-asm, value: f, valid values are: true/false",
		},
		{
			desc:       "LoadValue should be tolerant for unknow field.",
			inputMap:   map[string]string{"A": "B"},
			wantConfig: NewConfig(),
			wantLog:    "The map contains a unknown key-value pair: A:B",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			config := NewConfig()
			err := config.LoadValue(tc.inputMap)
			if !config.Equals(&tc.wantConfig) {
				t.Errorf("LoadValue loads wrong value, got: %v, want: %v", config, tc.wantConfig)
			}
			if tc.wantLog != "" {
				if !strings.Contains(err.Error(), tc.wantLog) {
					t.Errorf("LoadValue logs don't contain wanted log, got: %s, want: %s", err.Error(), tc.wantLog)
				}
			}
		})
	}
}

func TestConfigTag(t *testing.T) {
	configType := reflect.TypeOf(Config{})
	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		fieldType := field.Type.Kind()
		if fieldType != reflect.Bool && fieldType != reflect.String && fieldType != reflect.Slice {
			t.Errorf("Struct config contains filed with unknown type: %s, only supports: %s/%s/[]string types", fieldType, reflect.Bool.String(), reflect.String.String())
		}
		if fieldType == reflect.Slice {
			if field.Type.Elem().Kind() != reflect.String {
				t.Errorf("Struct config contains slice filed with unknown type: %s, only supports []string slice", field.Type.Elem().Kind())
			}
		}
	}
}
