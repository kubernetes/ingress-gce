package experiments

import (
	"context"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

// Client is the client interface for feature experiments.
type Client interface {
	BoolFlagValueOrFailsafe(ctx context.Context, name string, defaultValue bool) bool
	StringFlagValueOrFailsafe(ctx context.Context, name string, defaultValue string) string
	DoubleFlagValueOrFailsafe(ctx context.Context, name string, defaultValue float64) float64
	Int64FlagValueOrFailsafe(ctx context.Context, name string, defaultValue int64) int64
}

// FactoryClient will retry provisioning the primary client on failure, and will use secondary client if first cannot be created.
type FactoryClient struct {
	primaryClient   Client
	secondaryClient Client

	createPrimary func() (Client, error)
	retryPeriod   time.Duration
	lastCreateTry time.Time

	mu sync.Mutex

	log klog.Logger
}

func NewFactoryClient(createPrimary func() (Client, error), secondaryClient Client, retryPeriod time.Duration, logger klog.Logger) (*FactoryClient, error) {
	if createPrimary == nil {
		return nil, NewNilArgError("createPrimary")
	}

	if secondaryClient == nil {
		return nil, NewNilArgError("secondaryClient")
	}

	logger = logger.WithName("experiments.FactoryClient")

	return &FactoryClient{
		createPrimary:   createPrimary,
		secondaryClient: secondaryClient,
		retryPeriod:     retryPeriod,
		log:             logger,
	}, nil
}

func (f *FactoryClient) client() Client {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.primaryClient != nil {
		f.log.V(5).Info("Using primary client.")
		return f.primaryClient
	}

	now := time.Now()

	if now.Sub(f.lastCreateTry) < f.retryPeriod {
		f.log.V(5).Info("Using secondary client.", "nextRetryAfter", now.Sub(f.lastCreateTry))
		return f.secondaryClient
	}

	f.lastCreateTry = now
	client, err := f.createPrimary()
	if err != nil {
		nextRetry := now.Add(f.retryPeriod)
		f.log.Error(err, "Failed to create primaryClient, using backup.", "nextRetryAfter", nextRetry)
		return f.secondaryClient
	}
	f.log.Info("Primary client created.")
	f.primaryClient = client
	return client
}

func (f *FactoryClient) BoolFlagValueOrFailsafe(ctx context.Context, name string, defaultValue bool) bool {
	return f.client().BoolFlagValueOrFailsafe(ctx, name, defaultValue)
}

func (f *FactoryClient) StringFlagValueOrFailsafe(ctx context.Context, name string, defaultValue string) string {
	return f.client().StringFlagValueOrFailsafe(ctx, name, defaultValue)
}

func (f *FactoryClient) DoubleFlagValueOrFailsafe(ctx context.Context, name string, defaultValue float64) float64 {
	return f.client().DoubleFlagValueOrFailsafe(ctx, name, defaultValue)
}

func (f *FactoryClient) Int64FlagValueOrFailsafe(ctx context.Context, name string, defaultValue int64) int64 {
	return f.client().Int64FlagValueOrFailsafe(ctx, name, defaultValue)
}
