package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestEvaluatorPersistsIneligibleEntryRouteWhenQuoteHasNoExecutableRoute(t *testing.T) {
	now := time.Date(2026, time.August, 20, 19, 0, 0, 0, time.UTC)
	repository := &snapshotRepository{}
	evaluator := application.NewEvaluator(application.EvaluatorOptions{
		Now: now,
		Policy: domain.CandidatePolicy{
			MaxPoolAge:                90 * 24 * time.Hour,
			MinLiquidityUSD:           domain.USD{Micros: 10_000_000_000},
			MinFiveMinuteTransactions: 10,
			MaxEntryPriceImpactBPS:    1_000,
		},
		QuoteMint:        "quote",
		EntryInputAmount: 100_000_000,
		Market:           &marketProvider{snapshot: domain.MarketSnapshot{ObservedAt: now, LiquidityUSD: domain.USD{Micros: 10_000_000_000}, FiveMinuteTransactions: 10}},
		Token:            tokenInspector{},
		Quotes:           noRouteQuoteProvider{},
		Snapshots:        repository,
	})

	result, err := evaluator.Evaluate(context.Background(), domain.DiscoveredPool{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: now})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Eligible || repository.saved != 1 || routePassed(result, domain.RuleEntryRoute) {
		t.Fatalf("result = %#v, saved = %d, want saved ineligible entry-route rejection", result, repository.saved)
	}
}

type noRouteQuoteProvider struct{}

func (noRouteQuoteProvider) Quote(context.Context, string, string, uint64) (domain.ExecutableQuote, error) {
	return domain.ExecutableQuote{}, domain.ErrNoExecutableRoute
}

func routePassed(result domain.CandidateEvaluation, code string) bool {
	for _, rule := range result.Rules {
		if rule.Code == code {
			return rule.Passed
		}
	}
	return true
}
