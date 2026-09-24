package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestPaperBrokerWaitsForSecondFullSizeQuoteAndOpensOnce(t *testing.T) {
	clock := &brokerClock{now: time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)}
	quotes := &brokerQuotes{quote: validBrokerQuote(clock.now.Add(2 * time.Second))}
	positions := &brokerPositions{result: application.OpenPositionResult{Opened: true, Position: domain.PaperPosition{ID: 7, State: domain.PositionOpen}}}
	broker := application.NewPaperBroker(application.PaperBrokerOptions{
		Now: clock.Now, Sleep: clock.Sleep, Quotes: quotes, Positions: positions,
		SimulatedLatency: 2 * time.Second, MaxEntryPriceImpactBPS: 500,
		NetworkFeeMicros: 1_000, PriorityFeeMicros: 2_000,
	})

	result, err := broker.Open(context.Background(), application.OpenPositionRequest{
		IdempotencyKey: "candidate-mint:2026-08-19T12:00:00Z", QuoteMint: "quote-mint", MintAddress: "candidate-mint", EntryInputAmount: 10_000_000,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if !result.Opened || result.Position.ID != 7 || result.Position.State != domain.PositionOpen {
		t.Fatalf("Open() result = %#v, want newly opened position", result)
	}
	if got, want := clock.sleeps, []time.Duration{2 * time.Second}; !sameDurations(got, want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
	if len(quotes.calls) != 1 || quotes.calls[0] != (brokerQuoteCall{inMint: "quote-mint", outMint: "candidate-mint", amount: 10_000_000}) {
		t.Fatalf("quote calls = %#v, want one full-size entry quote", quotes.calls)
	}
	if len(positions.inputs) != 1 {
		t.Fatalf("position writes = %d, want 1", len(positions.inputs))
	}
	fill := positions.inputs[0].Fill
	if fill.TokenQuantity != 25_000_000 || fill.EntryPrice != "10000000/25000000" || fill.NetworkFeeMicros != 1_000 || fill.PriorityFeeMicros != 2_000 || len(fill.QuoteHash) != 64 {
		t.Fatalf("persisted fill = %#v, want quote-native fill with bounded fee estimates and hash", fill)
	}
}

func TestPaperBrokerRejectsUnusableSecondQuotesBeforePersisting(t *testing.T) {
	now := time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC)
	tests := []struct {
		name  string
		quote domain.QuoteEvidence
	}{

		{name: "missing route", quote: domain.QuoteEvidence{ObservedAt: now, InputMint: "quote-mint", OutputMint: "candidate-mint", InAmount: 10_000_000, OutAmount: 25_000_000}},
		{name: "excessive impact", quote: domain.QuoteEvidence{ObservedAt: now, InputMint: "quote-mint", OutputMint: "candidate-mint", InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 501, RoutePlan: []domain.RouteLeg{{AMMKey: "route-one"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := &brokerClock{now: now}
			positions := &brokerPositions{}
			broker := application.NewPaperBroker(application.PaperBrokerOptions{
				Now: clock.Now, Sleep: clock.Sleep, Quotes: &brokerQuotes{quote: tt.quote}, Positions: positions,
				SimulatedLatency: 2 * time.Second, MaxEntryPriceImpactBPS: 500,
			})
			_, err := broker.Open(context.Background(), validOpenPositionRequest())
			if err == nil {
				t.Fatal("Open() accepted an unusable second quote")
			}
			if len(positions.inputs) != 0 {
				t.Fatalf("position writes = %d, want 0", len(positions.inputs))
			}
		})
	}
}

func TestPaperBrokerBoldMomentumSecondQuoteImpactBoundary(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 2, 0, time.UTC)
	tests := []struct {
		name       string
		impactBPS  int64
		wantOpened bool
	}{
		{name: "accepts exact one-thousand-bps boundary", impactBPS: 1_000, wantOpened: true},
		{name: "rejects one basis point above boundary", impactBPS: 1_001},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quote := validBrokerQuote(now)
			quote.PriceImpactBPS = test.impactBPS
			positions := &brokerPositions{result: application.OpenPositionResult{Opened: true, Position: domain.PaperPosition{ID: 7, State: domain.PositionOpen}}}
			broker := application.NewPaperBroker(application.PaperBrokerOptions{
				Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) error { return nil },
				Quotes: &brokerQuotes{quote: quote}, Positions: positions, SimulatedLatency: time.Second,
				MaxEntryPriceImpactBPS: 1_000,
			})

			result, err := broker.Open(context.Background(), validOpenPositionRequest())
			if test.wantOpened {
				if err != nil || !result.Opened || len(positions.inputs) != 1 {
					t.Fatalf("Open() = %#v error=%v writes=%d, want one virtual open", result, err, len(positions.inputs))
				}
				return
			}
			if err == nil || result.Opened || len(positions.inputs) != 0 {
				t.Fatalf("Open() = %#v error=%v writes=%d, want rejection before persistence", result, err, len(positions.inputs))
			}
		})
	}
}

type brokerClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (clock *brokerClock) Now() time.Time { return clock.now }
func (clock *brokerClock) Sleep(_ context.Context, duration time.Duration) error {
	clock.sleeps = append(clock.sleeps, duration)
	clock.now = clock.now.Add(duration)
	return nil
}

type brokerQuoteCall struct {
	inMint, outMint string
	amount          uint64
}
type brokerQuotes struct {
	quote domain.QuoteEvidence
	err   error
	calls []brokerQuoteCall
}

func (quotes *brokerQuotes) Quote(_ context.Context, inMint, outMint string, amount uint64) (domain.QuoteEvidence, error) {
	quotes.calls = append(quotes.calls, brokerQuoteCall{inMint: inMint, outMint: outMint, amount: amount})
	if quotes.err != nil {
		return domain.QuoteEvidence{}, quotes.err
	}
	return quotes.quote, nil
}

type brokerPositions struct {
	inputs []application.OpenPositionInput
	result application.OpenPositionResult
	err    error
}

func (positions *brokerPositions) Open(_ context.Context, input application.OpenPositionInput) (application.OpenPositionResult, error) {
	positions.inputs = append(positions.inputs, input)
	if positions.err != nil {
		return application.OpenPositionResult{}, positions.err
	}
	return positions.result, nil
}

func validBrokerQuote(observedAt time.Time) domain.QuoteEvidence {
	return domain.QuoteEvidence{ObservedAt: observedAt, InputMint: "quote-mint", OutputMint: "candidate-mint", InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 125, RoutePlan: []domain.RouteLeg{{AMMKey: "route-one"}}}
}
func validOpenPositionRequest() application.OpenPositionRequest {
	return application.OpenPositionRequest{IdempotencyKey: "candidate-mint:2026-08-19T12:00:00Z", QuoteMint: "quote-mint", MintAddress: "candidate-mint", EntryInputAmount: 10_000_000}
}
func sameDurations(left, right []time.Duration) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

var _ application.QuoteProvider = (*brokerQuotes)(nil)
var _ application.PositionOpener = (*brokerPositions)(nil)
var _ = errors.New
