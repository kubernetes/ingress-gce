package experiments_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"k8s.io/ingress-gce/pkg/experiments"
	"k8s.io/klog/v2"
)

func TestNewFactoryClientValidation(t *testing.T) {
	log := klog.Background()
	mockSecondary := experiments.NewMockClient(log)

	t.Run("nil createPrimary returns error", func(t *testing.T) {
		_, err := experiments.NewFactoryClient(nil, mockSecondary, time.Second, log)
		if err == nil {
			t.Error("expected error for nil createPrimary, got nil")
		}
	})

	t.Run("nil secondaryClient returns error", func(t *testing.T) {
		createPrimary := func() (experiments.Client, error) {
			return mockSecondary, nil
		}
		_, err := experiments.NewFactoryClient(createPrimary, nil, time.Second, log)
		if err == nil {
			t.Error("expected error for nil secondaryClient, got nil")
		}
	})

	t.Run("valid parameters returns FactoryClient", func(t *testing.T) {
		createPrimary := func() (experiments.Client, error) {
			return mockSecondary, nil
		}
		fc, err := experiments.NewFactoryClient(createPrimary, mockSecondary, time.Second, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fc == nil {
			t.Fatal("expected non-nil FactoryClient")
		}
	})
}

// TestFactoryClientPrimarySuccess tests primary client is used when available.

func TestFactoryClientPrimarySuccess(t *testing.T) {
	log := klog.Background()
	primary := experiments.NewMockClient(log)
	secondary := experiments.NewMockClient(log)

	const expName = "ClientName"
	primary.Set(expName, "Primary")
	secondary.Set(expName, "Secondary")

	createPrimary := func() (experiments.Client, error) {
		return primary, nil
	}

	fc, err := experiments.NewFactoryClient(createPrimary, secondary, 1*time.Minute, log)
	if err != nil {
		t.Fatalf("failed to create FactoryClient: %v", err)
	}

	ctx := t.Context()
	for range 1000 {
		got := fc.StringFlagValueOrFailsafe(ctx, expName, "")
		if got != "Primary" {
			t.Errorf("expected 'Primary' from primary client, got %v", got)
		}
	}
}

func TestFactoryClientPrimaryFail(t *testing.T) {
	log := klog.Background()
	secondary := experiments.NewMockClient(log)

	const expName = "ClientName"
	secondary.Set(expName, "Secondary")

	testCases := []struct {
		desc          string
		createPrimary func() (experiments.Client, error)
	}{
		{desc: "nil primary", createPrimary: func() (experiments.Client, error) {
			return nil, errors.New("primary creation failed")
		}},
		{desc: "non nil primary", createPrimary: func() (experiments.Client, error) {
			return experiments.NewMockClient(log), errors.New("primary creation failed")
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			fc, err := experiments.NewFactoryClient(tc.createPrimary, secondary, 1*time.Minute, log)
			if err != nil {
				t.Fatalf("failed to create FactoryClient: %v", err)
			}
			ctx := t.Context()
			for range 1000 {
				got := fc.StringFlagValueOrFailsafe(ctx, expName, "")
				if got != "Secondary" {
					t.Errorf("expected 'Secondary' from secondary client, got %v", got)
				}
			}
		})
	}
}

// TestFactoryClientFallbackAndRetry tests that FactoryClient falls back to the
// secondary client when the primary client creation fails and retries creating
// the primary client after the retry period.
func TestFactoryClientFallbackAndRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log := klog.Background()
		secondary := experiments.NewMockClient(log)
		const expName = "TestFeature::Fallback"
		secondary.Set(expName, "Fallback")

		primary := experiments.NewMockClient(log)
		primary.Set(expName, "Primary")

		shouldFail := true
		createCount := 0
		createPrimary := func() (experiments.Client, error) {
			createCount++
			if shouldFail {
				return nil, errors.New("primary creation failed")
			}
			return primary, nil
		}

		retryPeriod := 1 * time.Hour
		fc, err := experiments.NewFactoryClient(createPrimary, secondary, retryPeriod, log)
		if err != nil {
			t.Fatalf("failed to create FactoryClient: %v", err)
		}

		ctx := t.Context()

		// First attempt: primary creation fails, falls back to secondary (value = false)
		got := fc.StringFlagValueOrFailsafe(ctx, expName, "")
		if got != "Fallback" {
			t.Errorf("expected 'Fallback' from secondary client, got %v", got)
		}
		if createCount != 1 {
			t.Errorf("expected createCount to be 1, got %d", createCount)
		}

		// Immediate second attempt (before retryPeriod): should not try createPrimary again even if ready
		shouldFail = false
		got = fc.StringFlagValueOrFailsafe(ctx, expName, "")
		if got != "Fallback" {
			t.Errorf("expected 'Fallback' from secondary client before retry period, got %v", got)
		}
		if createCount != 1 {
			t.Errorf("expected createCount to stay 1 before retry period, got %d", createCount)
		}

		// Advance virtual time beyond retryPeriod
		time.Sleep(2 * time.Hour)

		// Next attempt: retry period has elapsed, primary creation succeeds, uses primary (value = true)
		got = fc.StringFlagValueOrFailsafe(ctx, expName, "")
		if got != "Primary" {
			t.Errorf("expected 'Primary' from primary client after retry period, got %v", got)
		}
		if createCount != 2 {
			t.Errorf("expected createCount to be 2, got %d", createCount)
		}

		// Subsequent call: primary is cached, createPrimary is not called again
		got = fc.StringFlagValueOrFailsafe(ctx, expName, "")
		if got != "Primary" {
			t.Errorf("expected 'Primary' from primary client on cached call, got %v", got)
		}
		if createCount != 2 {
			t.Errorf("expected createCount to remain 2, got %d", createCount)
		}
	})
}

// TestFactoryClientAllTypes tests that FactoryClient works with all supported types.
func TestFactoryClientAllTypes(t *testing.T) {
	ctx := context.Background()
	log := klog.Background()
	primary := experiments.NewMockClient(log)

	primary.Set("string_exp", "string_value")
	primary.Set("int64_exp", int64(12345))
	primary.Set("float64_exp", 123.45)
	primary.Set("bool_exp", true)

	createPrimary := func() (experiments.Client, error) { return primary, nil }

	secondary := experiments.NewMockClient(log)

	fc, err := experiments.NewFactoryClient(createPrimary, secondary, 1*time.Minute, log)
	if err != nil {
		t.Fatalf("failed to create FactoryClient: %v", err)
	}

	if val := fc.BoolFlagValueOrFailsafe(ctx, "bool_exp", false); val != true {
		t.Errorf("want true from bool_exp, got %v", val)
	}

	if val := fc.Int64FlagValueOrFailsafe(ctx, "int64_exp", 0); val != int64(12345) {
		t.Errorf("want int64(12345) from int64_exp, got %v", val)
	}

	if val := fc.DoubleFlagValueOrFailsafe(ctx, "float64_exp", 0.0); val != 123.45 {
		t.Errorf("want 123.45 from float64_exp, got %v", val)
	}

	if val := fc.StringFlagValueOrFailsafe(ctx, "string_exp", ""); val != "string_value" {
		t.Errorf("want 'string_value' from string_exp, got %v", val)
	}
}
