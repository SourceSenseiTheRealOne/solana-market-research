package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	MarketRetryCap      = 5
	MarketRetryInterval = 30 * time.Second
	MarketRetryTTL      = 10 * time.Minute
)

type MarketRetryStore interface {
	Schedule(context.Context, domain.DiscoveredPool, time.Time, time.Time) error
	ReserveDue(context.Context, time.Time, time.Time, int) ([]domain.DiscoveredPool, error)
	Complete(context.Context, domain.DiscoveredPool) error
}

type MarketRetryDiscoveryOptions struct {
	Store      MarketRetryStore
	Now        time.Time
	LeaseUntil time.Time
	Limit      int
}

type MarketRetryDiscovery struct {
	options MarketRetryDiscoveryOptions
}

func NewMarketRetryDiscovery(options MarketRetryDiscoveryOptions) *MarketRetryDiscovery {
	return &MarketRetryDiscovery{options: options}
}

func (discovery *MarketRetryDiscovery) Run(ctx context.Context) (DiscoveryResult, error) {
	if discovery == nil || discovery.options.Store == nil || discovery.options.Now.IsZero() || discovery.options.LeaseUntil.IsZero() ||
		!discovery.options.LeaseUntil.After(discovery.options.Now) || discovery.options.Limit < 1 || discovery.options.Limit > MarketRetryCap {
		return DiscoveryResult{}, errors.New("market retry discovery is not completely configured")
	}
	if ctx == nil {
		return DiscoveryResult{}, errors.New("market retry discovery requires a context")
	}
	pools, err := discovery.options.Store.ReserveDue(
		ctx,
		discovery.options.Now.UTC(),
		discovery.options.LeaseUntil.UTC(),
		discovery.options.Limit,
	)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("reserve due market retries: %w", err)
	}
	if len(pools) > discovery.options.Limit {
		return DiscoveryResult{}, errors.New("market retry store exceeded the requested limit")
	}
	for _, pool := range pools {
		if err := pool.Validate(); err != nil {
			return DiscoveryResult{}, fmt.Errorf("validate reserved market retry: %w", err)
		}
	}
	return DiscoveryResult{Pools: pools}, nil
}

var _ ProductionDiscovery = (*MarketRetryDiscovery)(nil)
