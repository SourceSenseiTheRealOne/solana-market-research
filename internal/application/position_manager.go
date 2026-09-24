package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type PositionQuoteProvider interface {
	Quote(context.Context, string, string, uint64) (domain.ExecutableQuote, error)
	Health(context.Context) error
}

type PositionMarkInput struct {
	PositionID  int
	Mark        domain.ExitMark
	CloseReason domain.PositionCloseReason
}

type PositionNoRouteInput struct {
	PositionID int
	ObservedAt time.Time
}

type PositionManagerRepository interface {
	ListOpen(context.Context) ([]domain.PaperPosition, error)
	RecordMark(context.Context, PositionMarkInput) error
	RecordNoRoute(context.Context, PositionNoRouteInput) error
}

type PositionManagerOptions struct {
	Now               time.Time
	Quotes            PositionQuoteProvider
	Positions         PositionManagerRepository
	Policy            domain.ExitPolicy
	NetworkFeeMicros  int64
	PriorityFeeMicros int64
}

type PositionManager struct{ options PositionManagerOptions }

func NewPositionManager(options PositionManagerOptions) *PositionManager {
	return &PositionManager{options: options}
}

func (manager *PositionManager) RunOnce(ctx context.Context) error {
	if manager == nil || manager.options.Now.IsZero() || manager.options.Quotes == nil || manager.options.Positions == nil || manager.options.NetworkFeeMicros < 0 || manager.options.PriorityFeeMicros < 0 {
		return errors.New("position manager is not completely configured")
	}
	positions, err := manager.options.Positions.ListOpen(ctx)
	if err != nil {
		return fmt.Errorf("load open paper positions: %w", err)
	}
	for _, position := range positions {
		quote, err := manager.options.Quotes.Quote(ctx, position.MintAddress, position.QuoteMint, position.TokenQuantity)
		if err != nil {
			if healthErr := manager.options.Quotes.Health(ctx); healthErr != nil {
				return fmt.Errorf("check quote provider health after position %d quote failure: %w", position.ID, healthErr)
			}
			if errors.Is(err, domain.ErrNoExecutableRoute) {
				if err := manager.options.Positions.RecordNoRoute(ctx, PositionNoRouteInput{PositionID: position.ID, ObservedAt: manager.options.Now.UTC()}); err != nil {
					return fmt.Errorf("persist no-route observation for position %d: %w", position.ID, err)
				}
				continue
			}
			return fmt.Errorf("quote paper liquidation for position %d: %w", position.ID, err)
		}
		mark, err := domain.NewExitMark(position, quote, manager.options.NetworkFeeMicros, manager.options.PriorityFeeMicros)
		if err != nil {
			return fmt.Errorf("derive paper liquidation mark for position %d: %w", position.ID, err)
		}
		decision, err := manager.options.Policy.Decide(manager.options.Now.UTC(), position, mark)
		if err != nil {
			return fmt.Errorf("evaluate exit policy for position %d: %w", position.ID, err)
		}
		if err := manager.options.Positions.RecordMark(ctx, PositionMarkInput{PositionID: position.ID, Mark: mark, CloseReason: decision.Reason}); err != nil {
			return fmt.Errorf("persist paper liquidation mark for position %d: %w", position.ID, err)
		}
	}
	return nil
}
