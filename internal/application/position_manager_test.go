package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestPositionManagerMarksAndClosesAtTakeProfit(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	position := managerOpenPosition(now)
	quotes := &managerQuotes{quote: domain.QuoteEvidence{ObservedAt: now, InputMint: position.MintAddress, OutputMint: position.QuoteMint, InAmount: position.TokenQuantity, OutAmount: 15_005_000, RoutePlan: []domain.RouteLeg{{AMMKey: "route"}}}}
	positions := &managerPositions{open: []domain.PaperPosition{position}}
	manager := application.NewPositionManager(application.PositionManagerOptions{
		Now:               now,
		Quotes:            quotes,
		Positions:         positions,
		Policy:            domain.ExitPolicy{TakeProfitBPS: 5_000, StopLossBPS: 2_000, MaxHoldDuration: 45 * time.Minute},
		NetworkFeeMicros:  1_000,
		PriorityFeeMicros: 1_000,
	})

	if err := manager.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if got, want := quotes.calls, []managerQuoteCall{{inMint: position.MintAddress, outMint: position.QuoteMint, amount: position.TokenQuantity}}; !sameManagerQuoteCalls(got, want) {
		t.Fatalf("quote calls = %#v, want %#v", got, want)
	}
	if len(positions.inputs) != 1 {
		t.Fatalf("mark inputs = %d, want 1", len(positions.inputs))
	}
	input := positions.inputs[0]
	if input.PositionID != position.ID || input.Mark.ReturnBPS != 5_000 || input.CloseReason != domain.PositionCloseTakeProfit {
		t.Fatalf("mark input = %#v, want take-profit net liquidation mark", input)
	}
}

func TestPositionManagerCountsOnlyConfirmedNoRouteResponses(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	position := managerOpenPosition(now)
	tests := []struct {
		name        string
		quoteErr    error
		healthErr   error
		wantErr     bool
		wantNoRoute int
	}{
		{name: "confirmed mint-specific no route", quoteErr: domain.ErrNoExecutableRoute, wantNoRoute: 1},
		{name: "temporary provider outage", quoteErr: errors.New("upstream timeout"), healthErr: errors.New("provider unavailable"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quotes := &managerQuotes{err: tt.quoteErr, healthErr: tt.healthErr}
			positions := &managerPositions{open: []domain.PaperPosition{position}}
			manager := application.NewPositionManager(application.PositionManagerOptions{Now: now, Quotes: quotes, Positions: positions, Policy: domain.ExitPolicy{TakeProfitBPS: 5_000, StopLossBPS: 2_000, MaxHoldDuration: 45 * time.Minute}, NetworkFeeMicros: 1_000, PriorityFeeMicros: 1_000})

			err := manager.RunOnce(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("RunOnce() error = %v, want error=%t", err, tt.wantErr)
			}
			if got := len(positions.noRouteInputs); got != tt.wantNoRoute {
				t.Fatalf("no-route inputs = %d, want %d", got, tt.wantNoRoute)
			}
			if got, want := quotes.healthCalls, 1; got != want {
				t.Fatalf("health calls = %d, want %d", got, want)
			}
		})
	}
}

type managerQuoteCall struct {
	inMint, outMint string
	amount          uint64
}

type managerQuotes struct {
	quote       domain.QuoteEvidence
	err         error
	healthErr   error
	calls       []managerQuoteCall
	healthCalls int
}

func (quotes *managerQuotes) Quote(_ context.Context, inMint, outMint string, amount uint64) (domain.QuoteEvidence, error) {
	quotes.calls = append(quotes.calls, managerQuoteCall{inMint: inMint, outMint: outMint, amount: amount})
	if quotes.err != nil {
		return domain.QuoteEvidence{}, quotes.err
	}
	return quotes.quote, nil
}

func (quotes *managerQuotes) Health(context.Context) error {
	quotes.healthCalls++
	return quotes.healthErr
}

type managerPositions struct {
	open          []domain.PaperPosition
	inputs        []application.PositionMarkInput
	noRouteInputs []application.PositionNoRouteInput
}

func (positions *managerPositions) ListOpen(context.Context) ([]domain.PaperPosition, error) {
	return positions.open, nil
}

func (positions *managerPositions) RecordMark(_ context.Context, input application.PositionMarkInput) error {
	positions.inputs = append(positions.inputs, input)
	return nil
}

func (positions *managerPositions) RecordNoRoute(_ context.Context, input application.PositionNoRouteInput) error {
	positions.noRouteInputs = append(positions.noRouteInputs, input)
	return nil
}

func managerOpenPosition(now time.Time) domain.PaperPosition {
	return domain.PaperPosition{ID: 1, State: domain.PositionOpen, QuoteMint: "quote-mint", MintAddress: "candidate-mint", EntryInputAmount: 10_000_000, EntryNetworkFeeMicros: 1_000, EntryPriorityFeeMicros: 1_000, TokenQuantity: 25_000_000, OpenedAt: now.Add(-10 * time.Minute)}
}

func sameManagerQuoteCalls(left, right []managerQuoteCall) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
