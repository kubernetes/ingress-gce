package experiments_test

import (
	"context"
	"testing"

	"k8s.io/ingress-gce/pkg/experiments"
	"k8s.io/klog/v2"
)

func TestMockClientSetAndGet(t *testing.T) {
	ctx := context.Background()
	logger := klog.Background()
	client := experiments.NewMockClient(logger)

	client.Set("flagTrue", true)
	client.Set("flagFalse", false)
	client.Set("flagGreatVCS", "Jujutsu")
	client.Set("flag12345", int64(12345))
	client.Set("flag123.45", 123.45)

	if got := client.BoolFlagValueOrFailsafe(ctx, "flagTrue", false); got != true {
		t.Errorf("expected flagTrue to be true, got %v", got)
	}

	if got := client.BoolFlagValueOrFailsafe(ctx, "flagFalse", true); got != false {
		t.Errorf("expected flagFalse to be false, got %v", got)
	}

	if got := client.StringFlagValueOrFailsafe(ctx, "flagGreatVCS", "git"); got != "Jujutsu" {
		t.Errorf("expected flagGreatVCS to be 'Jujutsu', got %q", got)
	}

	if got := client.Int64FlagValueOrFailsafe(ctx, "flag12345", int64(67890)); got != int64(12345) {
		t.Errorf("expected flag12345 to be 12345, got %d", got)
	}

	if got := client.DoubleFlagValueOrFailsafe(ctx, "flag123.45", 678.90); got != 123.45 {
		t.Errorf("expected flag123.45 to be 123.45, got %f", got)
	}
}

func TestMockClientFailsafe(t *testing.T) {
	ctx := context.Background()
	logger := klog.Background()
	client := experiments.NewMockClient(logger)

	// No sets

	if got := client.BoolFlagValueOrFailsafe(ctx, "nonexistent_bool_flag", true); got != true {
		t.Errorf("expected failsafe value true for nonexistent_bool_flag, got %v", got)
	}

	if got := client.StringFlagValueOrFailsafe(ctx, "nonexistent_str_flag", "default_string"); got != "default_string" {
		t.Errorf("expected failsafe value 'default_string' for nonexistent_str_flag, got %q", got)
	}

	if got := client.Int64FlagValueOrFailsafe(ctx, "nonexistent_int64_flag", int64(12345)); got != int64(12345) {
		t.Errorf("expected failsafe value 12345 for nonexistent_int64_flag, got %d", got)
	}

	if got := client.DoubleFlagValueOrFailsafe(ctx, "nonexistent_double_flag", 123.45); got != 123.45 {
		t.Errorf("expected failsafe value 123.45 for nonexistent_double_flag, got %f", got)
	}
}

func TestMockClientTypeMismatch(t *testing.T) {
	ctx := context.Background()
	logger := klog.Background()
	client := experiments.NewMockClient(logger)

	// ctx is neither string, int, double, bool
	client.Set("flag", ctx)

	// We call with wrong type to force fallback value
	if got := client.BoolFlagValueOrFailsafe(ctx, "flag", true); got != true {
		t.Errorf("expected failsafe value true for flag, got %v", got)
	}

	if got := client.Int64FlagValueOrFailsafe(ctx, "flag", 1); got != 1 {
		t.Errorf("expected failsafe value 1 for flag, got %d", got)
	}

	if got := client.DoubleFlagValueOrFailsafe(ctx, "flag", 1.0); got != 1.0 {
		t.Errorf("expected failsafe value 1.0 for flag, got %f", got)
	}

	if got := client.StringFlagValueOrFailsafe(ctx, "flag", "string"); got != "string" {
		t.Errorf("expected string for flag, got %q", got)
	}
}

func TestMockClientClear(t *testing.T) {
	ctx := context.Background()
	logger := klog.Background()
	client := experiments.NewMockClient(logger)

	client.Set("flagStay1", true)
	client.Set("flagClear1", true)
	client.Set("flagClear2", true)
	client.Set("flagStay2", true)

	client.Clear("flagClear1", "flagClear2")

	// flagStays should not be removed
	if got := client.BoolFlagValueOrFailsafe(ctx, "flagStay1", false); !got {
		t.Errorf("expected flagStay1 to be true, got %v", got)
	}
	if got := client.BoolFlagValueOrFailsafe(ctx, "flagStay2", false); !got {
		t.Errorf("expected flagStay2 to be true, got %v", got)
	}

	// Deletes should return failsafe value.
	if got := client.BoolFlagValueOrFailsafe(ctx, "flagClear1", false); got {
		t.Errorf("expected flagClear1 to be false, got %v", got)
	}
	if got := client.BoolFlagValueOrFailsafe(ctx, "flagClear2", false); got {
		t.Errorf("expected flagClear2 to be false, got %v", got)
	}
}

func TestMockClientOverwrite(t *testing.T) {
	ctx := context.Background()
	logger := klog.Background()
	client := experiments.NewMockClient(logger)

	client.Set("flag", "flip")
	if got := client.StringFlagValueOrFailsafe(ctx, "flag", ""); got != "flip" {
		t.Errorf("expected flag to be flip, got %q", got)
	}

	client.Set("flag", "flop")
	if got := client.StringFlagValueOrFailsafe(ctx, "flag", ""); got != "flop" {
		t.Errorf("expected flag to be updated to flop, got %q", got)
	}
}
