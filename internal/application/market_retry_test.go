package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestMarketRetryDiscoveryReservesOneDueCandidateWithThirtySecondLease(t *testing.T) {
	now := time.Date(2026, time.August, 31, 10, 30, 0, 0, time.UTC)
	pool := domain.DiscoveredPool{
		Source:      domain.SourceDexScreener,
		Network:     domain.NetworkSolana,
		MintAddress: "retry-mint",
		PoolAddress: "retry-pool",
		CreatedAt:   now.Add(-3 * time.Minute),
	}
	store := &marketRetryStoreFake{reserved: []domain.DiscoveredPool{pool}}
	discovery := application.NewMarketRetryDiscovery(application.MarketRetryDiscoveryOptions{
		Store:      store,
		Now:        now,
		LeaseUntil: now.Add(application.MarketRetryInterval),
		Limit:      1,
	})

	result, err := discovery.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if store.reserveCalls != 1 || !store.now.Equal(now) || !store.leaseUntil.Equal(now.Add(30*time.Second)) || store.limit != 1 {
		t.Fatalf("reservation = calls=%d now=%s lease=%s limit=%d", store.reserveCalls, store.now, store.leaseUntil, store.limit)
	}
	if len(result.Pools) != 1 || result.Pools[0].Identity() != pool.Identity() {
		t.Fatalf("retry pools = %#v, want %s", result.Pools, pool.Identity())
	}
}

func TestMarketRetryDiscoveryRejectsInvalidBoundsBeforeStoreCall(t *testing.T) {
	now := time.Date(2026, time.August, 31, 10, 30, 0, 0, time.UTC)
	tests := []struct {
		name    string
		options application.MarketRetryDiscoveryOptions
	}{
		{name: "nil store", options: application.MarketRetryDiscoveryOptions{Now: now, LeaseUntil: now.Add(30 * time.Second), Limit: 1}},
		{name: "zero now", options: application.MarketRetryDiscoveryOptions{Store: &marketRetryStoreFake{}, LeaseUntil: now.Add(30 * time.Second), Limit: 1}},
		{name: "zero lease", options: application.MarketRetryDiscoveryOptions{Store: &marketRetryStoreFake{}, Now: now, Limit: 1}},
		{name: "lease not after now", options: application.MarketRetryDiscoveryOptions{Store: &marketRetryStoreFake{}, Now: now, LeaseUntil: now, Limit: 1}},
		{name: "zero limit", options: application.MarketRetryDiscoveryOptions{Store: &marketRetryStoreFake{}, Now: now, LeaseUntil: now.Add(30 * time.Second)}},
		{name: "limit above cap", options: application.MarketRetryDiscoveryOptions{Store: &marketRetryStoreFake{}, Now: now, LeaseUntil: now.Add(30 * time.Second), Limit: application.MarketRetryCap + 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := application.NewMarketRetryDiscovery(test.options).Run(context.Background())
			if err == nil {
				t.Fatal("Run() accepted invalid retry discovery options")
			}
			if test.options.Store != nil {
				store := test.options.Store.(*marketRetryStoreFake)
				if store.reserveCalls != 0 {
					t.Fatalf("ReserveDue() calls = %d, want zero", store.reserveCalls)
				}
			}
		})
	}
}

type marketRetryStoreFake struct {
	reserveCalls  int
	now           time.Time
	leaseUntil    time.Time
	limit         int
	reserved      []domain.DiscoveredPool
	scheduleCalls int
	scheduled     domain.DiscoveredPool
	nextAttemptAt time.Time
	expiresAt     time.Time
	scheduleErr   error
	completeCalls int
	completed     domain.DiscoveredPool
	completeErr   error
}

func (store *marketRetryStoreFake) Schedule(_ context.Context, pool domain.DiscoveredPool, nextAttemptAt, expiresAt time.Time) error {
	store.scheduleCalls++
	store.scheduled = pool
	store.nextAttemptAt = nextAttemptAt
	store.expiresAt = expiresAt
	return store.scheduleErr
}

func (store *marketRetryStoreFake) ReserveDue(_ context.Context, now, leaseUntil time.Time, limit int) ([]domain.DiscoveredPool, error) {
	store.reserveCalls++
	store.now = now
	store.leaseUntil = leaseUntil
	store.limit = limit
	return store.reserved, nil
}

func (store *marketRetryStoreFake) Complete(_ context.Context, pool domain.DiscoveredPool) error {
	store.completeCalls++
	store.completed = pool
	return store.completeErr
}
