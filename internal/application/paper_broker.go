package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type PositionOpener interface {
	Open(context.Context, OpenPositionInput) (OpenPositionResult, error)
}

type OpenPositionRequest struct {
	IdempotencyKey   string
	QuoteMint        string
	MintAddress      string
	EntryInputAmount uint64
}

type OpenPositionInput struct {
	IdempotencyKey string
	Fill           domain.EntryFill
}

type OpenPositionResult struct {
	Opened   bool
	Position domain.PaperPosition
}

type PaperBrokerOptions struct {
	Now                    func() time.Time
	Sleep                  func(context.Context, time.Duration) error
	Quotes                 QuoteProvider
	Positions              PositionOpener
	SimulatedLatency       time.Duration
	MaxEntryPriceImpactBPS int64
	NetworkFeeMicros       int64
	PriorityFeeMicros      int64
}

type PaperBroker struct{ options PaperBrokerOptions }

func NewPaperBroker(options PaperBrokerOptions) *PaperBroker { return &PaperBroker{options: options} }

func (broker *PaperBroker) Open(ctx context.Context, request OpenPositionRequest) (OpenPositionResult, error) {
	if broker == nil || broker.options.Now == nil || broker.options.Sleep == nil || broker.options.Quotes == nil || broker.options.Positions == nil || broker.options.SimulatedLatency <= 0 || broker.options.MaxEntryPriceImpactBPS < 0 || broker.options.NetworkFeeMicros < 0 || broker.options.PriorityFeeMicros < 0 {
		return OpenPositionResult{}, errors.New("paper broker is not completely configured")
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.QuoteMint) == "" || strings.TrimSpace(request.MintAddress) == "" || request.EntryInputAmount == 0 {
		return OpenPositionResult{}, errors.New("paper open request is incomplete")
	}
	if err := broker.options.Sleep(ctx, broker.options.SimulatedLatency); err != nil {
		return OpenPositionResult{}, fmt.Errorf("wait for simulated paper fill latency: %w", err)
	}

	quote, err := broker.options.Quotes.Quote(ctx, request.QuoteMint, request.MintAddress, request.EntryInputAmount)
	if err != nil {
		return OpenPositionResult{}, fmt.Errorf("quote executable paper entry: %w", err)
	}
	if err := validateSecondEntryQuote(broker.options, request, quote); err != nil {
		return OpenPositionResult{}, err
	}
	fill, err := domain.NewEntryFill(quote, broker.options.NetworkFeeMicros, broker.options.PriorityFeeMicros)
	if err != nil {
		return OpenPositionResult{}, fmt.Errorf("derive paper entry fill: %w", err)
	}
	result, err := broker.options.Positions.Open(ctx, OpenPositionInput{IdempotencyKey: request.IdempotencyKey, Fill: fill})
	if err != nil {
		return OpenPositionResult{}, fmt.Errorf("persist paper position fill: %w", err)
	}
	return result, nil
}

func validateSecondEntryQuote(options PaperBrokerOptions, request OpenPositionRequest, quote domain.QuoteEvidence) error {
	if err := quote.Validate(); err != nil {
		return fmt.Errorf("second entry quote is invalid: %w", err)
	}
	if quote.InputMint != request.QuoteMint || quote.OutputMint != request.MintAddress || quote.InAmount != request.EntryInputAmount || quote.PriceImpactBPS > options.MaxEntryPriceImpactBPS {
		return errors.New("second entry quote is outside paper admission limits")
	}
	return nil
}
