package domain_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestFreshPoolPolicyAcceptsTwoMonthPoolAndMarketDataWithoutAgeLimit(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	policy := domain.CandidatePolicy{
		MaxPoolAge:                90 * 24 * time.Hour,
		MinLiquidityUSD:           domain.USD{Micros: 10_000_000_000},
		MinFiveMinuteTransactions: 10,
		MaxEntryPriceImpactBPS:    1_000,
	}
	evidence := validCandidateEvidence(now)
	evidence.PoolCreatedAt = now.Add(-89 * 24 * time.Hour)
	evidence.MarketObservedAt = now.Add(-365 * 24 * time.Hour)
	evidence.LiquidityUSD = domain.USD{Micros: 10_000_000_000}
	evidence.FiveMinuteTransactions = 10
	evidence.EntryQuote.PriceImpactBPS = 1_000
	evidence.ExitQuote.PriceImpactBPS = 1_000

	result := policy.Evaluate(now, evidence)
	if !result.Eligible {
		t.Fatalf("eligible = false, failed rules = %v", failedRules(result))
	}
}

func TestFreshPoolPolicyRejectsPoolsOlderThanNinetyDays(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	policy := domain.CandidatePolicy{MaxPoolAge: 90 * 24 * time.Hour, MinLiquidityUSD: domain.USD{Micros: 10_000_000_000}, MinFiveMinuteTransactions: 10, MaxEntryPriceImpactBPS: 1_000}
	evidence := validCandidateEvidence(now)
	evidence.PoolCreatedAt = now.Add(-90*24*time.Hour - time.Nanosecond)
	evidence.LiquidityUSD = domain.USD{Micros: 10_000_000_000}
	evidence.FiveMinuteTransactions = 10
	evidence.EntryQuote.PriceImpactBPS = 1_000
	evidence.ExitQuote.PriceImpactBPS = 1_000

	result := policy.Evaluate(now, evidence)
	if result.Eligible || rulePassed(result, domain.RulePoolAge) {
		t.Fatalf("over-ninety-day pool was accepted: %#v", result)
	}
}
