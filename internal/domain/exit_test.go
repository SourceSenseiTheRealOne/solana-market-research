package domain_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestExitPolicyClosesAtNetTakeProfit(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	position := domain.PaperPosition{
		ID:                     1,
		State:                  domain.PositionOpen,
		QuoteMint:              "quote-mint",
		MintAddress:            "candidate-mint",
		EntryInputAmount:       10_000_000,
		EntryNetworkFeeMicros:  1_000,
		EntryPriorityFeeMicros: 1_000,
		TokenQuantity:          25_000_000,
		OpenedAt:               now.Add(-10 * time.Minute),
	}
	quote := domain.QuoteEvidence{
		ObservedAt: now,
		InputMint:  position.MintAddress,
		OutputMint: position.QuoteMint,
		InAmount:   position.TokenQuantity,
		OutAmount:  13_004_600,
		RoutePlan:  []domain.RouteLeg{{AMMKey: "route"}},
	}

	mark, err := domain.NewExitMark(position, quote, 1_000, 1_000)
	if err != nil {
		t.Fatalf("NewExitMark() error = %v", err)
	}
	if got, want := mark.NetOutputAmount, uint64(13_002_600); got != want {
		t.Fatalf("net output = %d, want %d", got, want)
	}
	if got, want := mark.ReturnBPS, int64(3_000); got != want {
		t.Fatalf("return BPS = %d, want %d", got, want)
	}

	decision, err := (domain.ExitPolicy{TakeProfitBPS: 3_000, StopLossBPS: 1_500, MaxHoldDuration: time.Hour}).Decide(now, position, mark)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got, want := decision.Reason, domain.PositionCloseTakeProfit; got != want {
		t.Fatalf("close reason = %q, want %q", got, want)
	}
}

func TestExitPolicyClosesAtNetStopLoss(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	position := domain.PaperPosition{ID: 1, State: domain.PositionOpen, QuoteMint: "quote-mint", MintAddress: "candidate-mint", EntryInputAmount: 10_000_000, EntryNetworkFeeMicros: 1_000, EntryPriorityFeeMicros: 1_000, TokenQuantity: 25_000_000, OpenedAt: now.Add(-10 * time.Minute)}
	quote := domain.QuoteEvidence{ObservedAt: now, InputMint: position.MintAddress, OutputMint: position.QuoteMint, InAmount: position.TokenQuantity, OutAmount: 8_503_700, RoutePlan: []domain.RouteLeg{{AMMKey: "route"}}}

	mark, err := domain.NewExitMark(position, quote, 1_000, 1_000)
	if err != nil {
		t.Fatalf("NewExitMark() error = %v", err)
	}
	if got, want := mark.ReturnBPS, int64(-1_500); got != want {
		t.Fatalf("return BPS = %d, want %d", got, want)
	}

	decision, err := (domain.ExitPolicy{TakeProfitBPS: 3_000, StopLossBPS: 1_500, MaxHoldDuration: time.Hour}).Decide(now, position, mark)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got, want := decision.Reason, domain.PositionCloseStopLoss; got != want {
		t.Fatalf("close reason = %q, want %q", got, want)
	}
}

func TestExitPolicyClosesAtMaximumHoldDuration(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	position := domain.PaperPosition{ID: 1, State: domain.PositionOpen, QuoteMint: "quote-mint", MintAddress: "candidate-mint", EntryInputAmount: 10_000_000, EntryNetworkFeeMicros: 1_000, EntryPriorityFeeMicros: 1_000, TokenQuantity: 25_000_000, OpenedAt: now.Add(-time.Hour)}
	quote := domain.QuoteEvidence{ObservedAt: now, InputMint: position.MintAddress, OutputMint: position.QuoteMint, InAmount: position.TokenQuantity, OutAmount: 10_004_000, RoutePlan: []domain.RouteLeg{{AMMKey: "route"}}}

	mark, err := domain.NewExitMark(position, quote, 1_000, 1_000)
	if err != nil {
		t.Fatalf("NewExitMark() error = %v", err)
	}
	if got, want := mark.ReturnBPS, int64(0); got != want {
		t.Fatalf("return BPS = %d, want %d", got, want)
	}

	decision, err := (domain.ExitPolicy{TakeProfitBPS: 3_000, StopLossBPS: 1_500, MaxHoldDuration: time.Hour}).Decide(now, position, mark)
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if got, want := decision.Reason, domain.PositionCloseTimeout; got != want {
		t.Fatalf("close reason = %q, want %q", got, want)
	}
}
