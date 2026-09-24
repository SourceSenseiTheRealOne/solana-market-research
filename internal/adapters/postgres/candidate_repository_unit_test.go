package postgres

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestCandidateMarketEvidenceIncludesNormalizedFiveMinuteMomentum(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	evidence := domain.CandidateEvidence{
		MarketObservedAt:         now,
		ReceivedAt:               now.Add(time.Second),
		LiquidityUSD:             domain.USD{Micros: 5_000_000_000},
		FiveMinuteTransactions:   20,
		FiveMinuteBuys:           13,
		FiveMinuteSells:          7,
		FiveMinuteVolumeUSD:      domain.USD{Micros: 750_000_000},
		FiveMinutePriceChangeBPS: 200,
		Token:                    domain.TokenSafety{Program: domain.TokenProgramLegacy, MintAuthorityRevoked: true, FreezeAuthorityRevoked: true},
	}

	got := candidateMarketEvidence(evidence, domain.CandidateEvaluation{Eligible: true})
	want := map[string]any{
		"five_minute_transactions":      int(20),
		"five_minute_buys":              int(13),
		"five_minute_sells":             int(7),
		"five_minute_volume_usd_micros": int64(750_000_000),
		"five_minute_price_change_bps":  int64(200),
	}
	for key, expected := range want {
		if got[key] != expected {
			t.Fatalf("market evidence %q = %#v, want %#v", key, got[key], expected)
		}
	}
}
