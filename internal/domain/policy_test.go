package domain_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestCandidatePolicyEvaluatesEveryRequiredGate(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	policy := validCandidatePolicy()

	tests := []struct {
		name       string
		mutate     func(*domain.CandidateEvidence)
		failedRule string
	}{
		{"accepts exact policy boundaries", func(*domain.CandidateEvidence) {}, ""},
		{"accepts pools created at scan time", func(e *domain.CandidateEvidence) { e.PoolCreatedAt = now }, ""},
		{"rejects pools older than ninety minutes", func(e *domain.CandidateEvidence) { e.PoolCreatedAt = now.Add(-90*time.Minute - time.Nanosecond) }, domain.RulePoolAge},
		{"rejects insufficient liquidity", func(e *domain.CandidateEvidence) { e.LiquidityUSD = domain.USD{Micros: 4_999_999_999} }, domain.RuleLiquidity},
		{"rejects insufficient activity", func(e *domain.CandidateEvidence) { e.FiveMinuteTransactions = 19 }, domain.RuleFiveMinuteActivity},
		{"rejects inconsistent activity", func(e *domain.CandidateEvidence) { e.FiveMinuteTransactions = 21 }, domain.RuleFiveMinuteActivity},
		{"rejects zero activity without dividing", func(e *domain.CandidateEvidence) {
			e.FiveMinuteTransactions = 0
			e.FiveMinuteBuys = 0
			e.FiveMinuteSells = 0
		}, domain.RuleFiveMinuteBuyShare},
		{"rejects buy share below sixty percent", func(e *domain.CandidateEvidence) { e.FiveMinuteBuys = 11; e.FiveMinuteSells = 9 }, domain.RuleFiveMinuteBuyShare},
		{"rejects buy share at five thousand nine hundred ninety-nine bps", func(e *domain.CandidateEvidence) {
			e.FiveMinuteTransactions = 10_000
			e.FiveMinuteBuys = 5_999
			e.FiveMinuteSells = 4_001
		}, domain.RuleFiveMinuteBuyShare},
		{"rejects zero liquidity without dividing", func(e *domain.CandidateEvidence) { e.LiquidityUSD = domain.USD{} }, domain.RuleFiveMinuteTurnover},
		{"rejects turnover below fifteen percent", func(e *domain.CandidateEvidence) { e.FiveMinuteVolumeUSD = domain.USD{Micros: 749_999_999} }, domain.RuleFiveMinuteTurnover},
		{"rejects momentum below two percent", func(e *domain.CandidateEvidence) { e.FiveMinutePriceChangeBPS = 199 }, domain.RuleFiveMinutePriceChange},
		{"rejects momentum above sixty percent", func(e *domain.CandidateEvidence) { e.FiveMinutePriceChangeBPS = 6_001 }, domain.RuleFiveMinutePriceChange},
		{"rejects excessive entry impact", func(e *domain.CandidateEvidence) { e.EntryQuote.PriceImpactBPS = 1_001 }, domain.RuleEntryPriceImpact},
		{"rejects a missing entry route", func(e *domain.CandidateEvidence) { e.EntryQuote.RoutePlan = nil }, domain.RuleEntryRoute},
		{"rejects a missing exit route", func(e *domain.CandidateEvidence) { e.ExitQuote.RoutePlan = nil }, domain.RuleExitRoute},
		{"rejects active mint authority", func(e *domain.CandidateEvidence) { e.Token.MintAuthorityRevoked = false }, domain.RuleMintAuthority},
		{"rejects active freeze authority", func(e *domain.CandidateEvidence) { e.Token.FreezeAuthorityRevoked = false }, domain.RuleFreezeAuthority},
		{"rejects unallowlisted Token-2022 extensions", func(e *domain.CandidateEvidence) { e.Token.Extensions = []domain.TokenExtension{"TransferFeeConfig"} }, domain.RuleTokenProgram},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validCandidateEvidence(now)
			tt.mutate(&evidence)

			result := policy.Evaluate(now, evidence)
			if got, want := len(result.Rules), 12; got != want {
				t.Fatalf("rule count = %d, want %d", got, want)
			}
			if tt.failedRule == "" {
				if !result.Eligible {
					t.Fatalf("eligible = false, failed rules = %v", failedRules(result))
				}
				return
			}
			if result.Eligible {
				t.Fatal("eligible = true, want false")
			}
			if rulePassed(result, tt.failedRule) {
				t.Fatalf("rule %q passed, want failure", tt.failedRule)
			}
		})
	}
}

func TestCandidatePolicyAllowsOnlyExplicitToken2022Extensions(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	evidence := validCandidateEvidence(now)
	evidence.Token.Program = domain.TokenProgram2022
	evidence.Token.Extensions = []domain.TokenExtension{"ImmutableOwner"}

	result := validCandidatePolicy().Evaluate(now, evidence)
	if result.Eligible || rulePassed(result, domain.RuleTokenProgram) {
		t.Fatalf("unallowlisted extension was accepted: %#v", result)
	}

	policy := validCandidatePolicy()
	policy.AllowedToken2022Extensions = []domain.TokenExtension{"ImmutableOwner"}
	result = policy.Evaluate(now, evidence)
	if !result.Eligible {
		t.Fatalf("explicitly allowlisted extension was rejected: %#v", result)
	}
}

func TestCandidatePolicyReportsEveryFailureInsteadOfFailingFast(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	evidence := validCandidateEvidence(now)
	evidence.LiquidityUSD = domain.USD{Micros: 1}
	evidence.EntryQuote.RoutePlan = nil
	evidence.Token.MintAuthorityRevoked = false

	result := validCandidatePolicy().Evaluate(now, evidence)
	for _, rule := range []string{domain.RuleLiquidity, domain.RuleEntryRoute, domain.RuleMintAuthority} {
		if rulePassed(result, rule) {
			t.Fatalf("rule %q passed, want failure", rule)
		}
	}
}

func TestCandidatePolicyRecordsObservedAndLimitForEveryRule(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	evidence := validCandidateEvidence(now)
	evidence.LiquidityUSD = domain.USD{Micros: 1}

	result := validCandidatePolicy().Evaluate(now, evidence)
	for _, rule := range result.Rules {
		if rule.Observed == "" {
			t.Fatalf("rule %q omitted observed value", rule.Code)
		}
		if rule.Limit == "" {
			t.Fatalf("rule %q omitted policy limit", rule.Code)
		}
	}
}

func validCandidatePolicy() domain.CandidatePolicy {
	return domain.CandidatePolicy{
		MaxPoolAge:                  90 * time.Minute,
		MinLiquidityUSD:             domain.USD{Micros: 5_000_000_000},
		MinFiveMinuteTransactions:   20,
		MinFiveMinuteBuyShareBPS:    6_000,
		MinFiveMinuteTurnoverBPS:    1_500,
		MinFiveMinutePriceChangeBPS: 200,
		MaxFiveMinutePriceChangeBPS: 6_000,
		MaxEntryPriceImpactBPS:      1_000,
		AllowedToken2022Extensions:  nil,
	}
}

func validCandidateEvidence(now time.Time) domain.CandidateEvidence {
	return domain.CandidateEvidence{
		MintAddress:              "candidate-mint",
		QuoteMint:                "quote-mint",
		PoolCreatedAt:            now.Add(-3 * time.Minute),
		MarketObservedAt:         now.Add(-90 * time.Second),
		LiquidityUSD:             domain.USD{Micros: 5_000_000_000},
		FiveMinuteTransactions:   20,
		FiveMinuteBuys:           12,
		FiveMinuteSells:          8,
		FiveMinuteVolumeUSD:      domain.USD{Micros: 750_000_000},
		FiveMinutePriceChangeBPS: 200,
		EntryInputAmount:         10_000_000,
		Token:                    domain.TokenSafety{Program: domain.TokenProgramLegacy, MintAuthorityRevoked: true, FreezeAuthorityRevoked: true},
		EntryQuote:               domain.QuoteEvidence{InputMint: "quote-mint", OutputMint: "candidate-mint", InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 1_000, RoutePlan: []domain.RouteLeg{{AMMKey: "pool-one", Label: "Raydium"}}},
		ExitQuote:                domain.QuoteEvidence{InputMint: "candidate-mint", OutputMint: "quote-mint", InAmount: 25_000_000, OutAmount: 9_000_000, PriceImpactBPS: 1_000, RoutePlan: []domain.RouteLeg{{AMMKey: "pool-one", Label: "Raydium"}}},
	}
}

func rulePassed(result domain.CandidateEvaluation, rule string) bool {
	for _, candidateRule := range result.Rules {
		if candidateRule.Code == rule {
			return candidateRule.Passed
		}
	}
	return false
}

func failedRules(result domain.CandidateEvaluation) []string {
	failed := make([]string, 0)
	for _, rule := range result.Rules {
		if !rule.Passed {
			failed = append(failed, rule.Code)
		}
	}
	return failed
}
