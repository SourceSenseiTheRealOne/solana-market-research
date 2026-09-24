package domain_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestEntryFillUsesQuoteNativeAmountsAndCanonicalQuoteHash(t *testing.T) {
	observedAt := time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC)
	quote := domain.QuoteEvidence{
		ObservedAt:     observedAt,
		InputMint:      "quote-mint",
		OutputMint:     "candidate-mint",
		InAmount:       10_000_000,
		OutAmount:      25_000_000,
		PriceImpactBPS: 125,
		PlatformFee:    domain.QuoteFee{Amount: 42, Mint: "quote-mint"},
		RoutePlan: []domain.RouteLeg{{
			AMMKey: "route-one", Label: "Raydium", Fee: domain.QuoteFee{Amount: 31, Mint: "candidate-mint"},
		}},
	}

	fill, err := domain.NewEntryFill(quote, 1_000, 2_000)
	if err != nil {
		t.Fatalf("NewEntryFill() error = %v", err)
	}
	if got, want := fill.TokenQuantity, uint64(25_000_000); got != want {
		t.Fatalf("token quantity = %d, want %d", got, want)
	}
	if got, want := fill.EntryPrice, "10000000/25000000"; got != want {
		t.Fatalf("entry price = %q, want %q", got, want)
	}
	if got, want := fill.NetworkFeeMicros, int64(1_000); got != want {
		t.Fatalf("network fee = %d, want %d", got, want)
	}
	if got, want := fill.PriorityFeeMicros, int64(2_000); got != want {
		t.Fatalf("priority fee = %d, want %d", got, want)
	}
	if len(fill.QuoteHash) != 64 {
		t.Fatalf("quote hash = %q, want a SHA-256 hex digest", fill.QuoteHash)
	}

	sameFill, err := domain.NewEntryFill(quote, 1_000, 2_000)
	if err != nil {
		t.Fatalf("second NewEntryFill() error = %v", err)
	}
	if sameFill.QuoteHash != fill.QuoteHash {
		t.Fatalf("same quote hash = %q, want %q", sameFill.QuoteHash, fill.QuoteHash)
	}
}

func TestEntryFillRejectsMissingObservedQuoteAndNegativeFeeEstimates(t *testing.T) {
	valid := domain.QuoteEvidence{
		ObservedAt: time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC),
		InputMint:  "quote-mint", OutputMint: "candidate-mint", InAmount: 10_000_000, OutAmount: 25_000_000,
		RoutePlan: []domain.RouteLeg{{AMMKey: "route-one"}},
	}

	missingObserved := valid
	missingObserved.ObservedAt = time.Time{}
	if _, err := domain.NewEntryFill(missingObserved, 0, 0); err == nil {
		t.Fatal("NewEntryFill() accepted a quote without an observation time")
	}
	if _, err := domain.NewEntryFill(valid, -1, 0); err == nil {
		t.Fatal("NewEntryFill() accepted a negative network-fee estimate")
	}
	if _, err := domain.NewEntryFill(valid, 0, -1); err == nil {
		t.Fatal("NewEntryFill() accepted a negative priority-fee estimate")
	}
}
