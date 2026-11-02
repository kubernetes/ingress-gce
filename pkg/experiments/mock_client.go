package experiments

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
)

type MockClient struct {
	values map[string]any
	log    klog.Logger
}

func NewMockClient(logger klog.Logger) *MockClient {
	return &MockClient{
		values: make(map[string]any),
		log:    logger.WithName("MockClient").V(3),
	}
}

func (m *MockClient) Set(flagName string, value any) {
	m.values[flagName] = value
	m.log.Info("Set.", "experiment", flagName, "value", value)
}

func (m *MockClient) Clear(flagNames ...string) {
	for _, f := range flagNames {
		delete(m.values, f)
		m.log.Info("Cleared.", "experiment", f)
	}
}

func (m *MockClient) flagValueOrFailsafe[T any](_ context.Context, flagName string, failSafeValue T) T {
	if v, ok := m.values[flagName]; ok {
		if val, ok := v.(T); ok {
			m.log.Info("Found value.", "experiment", flagName, "value", v)
			return val
		}
		err := fmt.Errorf("expected %T, got %T", failSafeValue, v)
		m.log.Error(err, "Error reading flag %q.", flagName)
	}
	m.log.Info("Falling back to fail safe.", "experiment", flagName, "value", failSafeValue)
	return failSafeValue
}

func (m *MockClient) BoolFlagValueOrFailsafe(ctx context.Context, flagName string, failSafeValue bool) bool {
	return m.flagValueOrFailsafe(ctx, flagName, failSafeValue)
}

func (m *MockClient) StringFlagValueOrFailsafe(ctx context.Context, flagName string, failSafeValue string) string {
	return m.flagValueOrFailsafe(ctx, flagName, failSafeValue)
}

func (m *MockClient) Int64FlagValueOrFailsafe(ctx context.Context, flagName string, failSafeValue int64) int64 {
	return m.flagValueOrFailsafe(ctx, flagName, failSafeValue)
}

func (m *MockClient) DoubleFlagValueOrFailsafe(ctx context.Context, flagName string, failSafeValue float64) float64 {
	return m.flagValueOrFailsafe(ctx, flagName, failSafeValue)
}
