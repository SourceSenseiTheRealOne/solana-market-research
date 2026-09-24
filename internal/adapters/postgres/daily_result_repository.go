package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/dailyresult"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/positionmark"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type DailyResultRepository struct {
	client *ent.Client
	now    func() time.Time
}

func NewDailyResultRepository(client *ent.Client, now func() time.Time) (*DailyResultRepository, error) {
	if client == nil || now == nil {
		return nil, errors.New("daily result repository requires an Ent client and clock")
	}
	return &DailyResultRepository{client: client, now: now}, nil
}

func (repository *DailyResultRepository) List(ctx context.Context, date time.Time) ([]domain.DailyResult, error) {
	if repository == nil || repository.client == nil || date.IsZero() {
		return nil, errors.New("daily result list request is incomplete")
	}
	utcDate := time.Date(date.UTC().Year(), date.UTC().Month(), date.UTC().Day(), 0, 0, 0, 0, time.UTC)
	stored, err := repository.client.DailyResult.Query().Where(dailyresult.UtcDateEQ(utcDate)).Order(dailyresult.ByStrategyVersion()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query daily results: %w", err)
	}
	results := make([]domain.DailyResult, 0, len(stored))
	for _, result := range stored {
		results = append(results, domain.DailyResult{UTCDate: result.UtcDate.UTC(), StrategyVersion: result.StrategyVersion, DailyAdmittedCount: result.DailyAdmittedCount, RealizedPNLMicros: result.RealizedPnlMicros})
	}
	return results, nil
}

// Reconcile recomputes one UTC day's realized paper P&L from terminal positions.
func (repository *DailyResultRepository) Reconcile(ctx context.Context, date time.Time) error {
	if repository == nil || repository.client == nil || repository.now == nil || date.IsZero() {
		return errors.New("daily result reconciliation request is incomplete")
	}
	utcDate := time.Date(date.UTC().Year(), date.UTC().Month(), date.UTC().Day(), 0, 0, 0, 0, time.UTC)
	now := repository.now().UTC()
	if now.IsZero() {
		return errors.New("daily result repository clock returned zero time")
	}
	tx, err := repository.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin daily result reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := tx.DailyResult.Update().Where(dailyresult.UtcDateEQ(utcDate)).SetRealizedPnlMicros(0).SetUpdatedAt(now).Exec(ctx); err != nil {
		return fmt.Errorf("reset daily realized paper P&L: %w", err)
	}
	positions, err := tx.PaperPosition.Query().
		Where(
			paperposition.StateIn(paperposition.StateCLOSED, paperposition.StateUNSELLABLE),
			paperposition.ClosedAtGTE(utcDate),
			paperposition.ClosedAtLT(utcDate.AddDate(0, 0, 1)),
		).
		All(ctx)
	if err != nil {
		return fmt.Errorf("query terminal paper positions: %w", err)
	}
	totals := make(map[string]int64)
	for _, position := range positions {
		mark, err := tx.PositionMark.Query().Where(positionmark.HasPositionWith(paperposition.IDEQ(position.ID))).Order(positionmark.ByCreatedAt(sql.OrderDesc())).First(ctx)
		if ent.IsNotFound(err) {
			return fmt.Errorf("terminal paper position %d is missing a typed terminal return", position.ID)
		}
		if err != nil {
			return fmt.Errorf("load terminal paper position mark %d: %w", position.ID, err)
		}
		if mark.ReturnBps == nil {
			return fmt.Errorf("terminal paper position %d is missing a typed terminal return", position.ID)
		}
		pnl, err := domain.RealizedPNLMicros(position.NotionalMicros, *mark.ReturnBps)
		if err != nil {
			return fmt.Errorf("calculate realized paper P&L for position %d: %w", position.ID, err)
		}
		if (pnl > 0 && totals[position.StrategyVersion] > int64(^uint64(0)>>1)-pnl) || (pnl < 0 && totals[position.StrategyVersion] < -int64(^uint64(0)>>1)-pnl) {
			return fmt.Errorf("realized paper P&L overflow for strategy %q", position.StrategyVersion)
		}
		totals[position.StrategyVersion] += pnl
	}
	for strategyVersion, total := range totals {
		if err := tx.DailyResult.Create().
			SetUtcDate(utcDate).
			SetStrategyVersion(strategyVersion).
			SetDailyAdmittedCount(0).
			SetRealizedPnlMicros(0).
			SetCreatedAt(now).
			SetUpdatedAt(now).
			OnConflictColumns(dailyresult.FieldUtcDate, dailyresult.FieldStrategyVersion).
			Ignore().
			Exec(ctx); err != nil {
			return fmt.Errorf("ensure daily result for strategy %q: %w", strategyVersion, err)
		}
		result, err := tx.DailyResult.Query().Where(dailyresult.UtcDateEQ(utcDate), dailyresult.StrategyVersionEQ(strategyVersion)).Only(ctx)
		if err != nil {
			return fmt.Errorf("load daily result for strategy %q: %w", strategyVersion, err)
		}
		if err := tx.DailyResult.UpdateOneID(result.ID).SetRealizedPnlMicros(total).SetUpdatedAt(now).Exec(ctx); err != nil {
			return fmt.Errorf("persist realized paper P&L for strategy %q: %w", strategyVersion, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit daily result reconciliation: %w", err)
	}
	return nil
}
