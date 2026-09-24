package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestProductionScanJobWatchesFuturePoolWithoutEvaluatingIt(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	pool := productionPool(now.Add(time.Minute))
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{pool}}}
	evaluator := &productionEvaluator{evaluation: eligibleProductionEvaluation()}
	watches := &productionFutureWatches{}
	options := validProductionScanOptions(now, discovery, evaluator)
	options.FutureWatches = watches

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if evaluator.calls != 0 || watches.watched.Identity() != pool.Identity() || !watches.expiresAt.Equal(pool.CreatedAt.Add(application.FuturePoolWatchTTL)) {
		t.Fatalf("future pool evaluation/watch = %d/%#v/%s, want no evaluation and one bounded watch", evaluator.calls, watches.watched, watches.expiresAt)
	}
}

type productionFutureWatches struct {
	watched   domain.DiscoveredPool
	expiresAt time.Time
}

func (watches *productionFutureWatches) Watch(_ context.Context, pool domain.DiscoveredPool, expiresAt time.Time) error {
	watches.watched = pool
	watches.expiresAt = expiresAt
	return nil
}

func (watches *productionFutureWatches) TakeDue(context.Context, time.Time, int) ([]domain.DiscoveredPool, error) {
	return nil, nil
}
