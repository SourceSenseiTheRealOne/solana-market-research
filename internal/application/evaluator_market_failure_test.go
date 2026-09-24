package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestEvaluatorMarksMarketEvidenceFailuresForBoundedScanContinuation(t *testing.T) {
	now := time.Date(2026, time.August, 20, 20, 0, 0, 0, time.UTC)
	evaluator := application.NewEvaluator(application.EvaluatorOptions{
		Now:              now,
		Policy:           domain.CandidatePolicy{MaxPoolAge: 90 * 24 * time.Hour, MinLiquidityUSD: domain.USD{Micros: 10_000_000_000}, MinFiveMinuteTransactions: 10, MaxEntryPriceImpactBPS: 1_000},
		QuoteMint:        "quote",
		EntryInputAmount: 100_000_000,
		Market:           failingMarketProvider{},
		Token:            tokenInspector{},
		Quotes:           &quoteProvider{},
		Snapshots:        &snapshotRepository{},
	})

	_, err := evaluator.Evaluate(context.Background(), domain.DiscoveredPool{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: now})
	if err == nil {
		t.Fatal("Evaluate() accepted unavailable market evidence")
	}
	var unavailable interface{ MarketEvidenceUnavailable() }
	if !errors.As(err, &unavailable) {
		t.Fatalf("Evaluate() error = %v, want typed market-evidence marker", err)
	}
}

type failingMarketProvider struct{}

func (failingMarketProvider) Fetch(context.Context, domain.DiscoveredPool) (domain.MarketSnapshot, error) {
	return domain.MarketSnapshot{}, errors.New("provider unavailable")
}
