package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/positionmark"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/tradedecision"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type PositionRepository struct {
	client *ent.Client
	now    func() time.Time
}

func NewPositionRepository(client *ent.Client, now func() time.Time) (*PositionRepository, error) {
	if client == nil || now == nil {
		return nil, errors.New("position repository requires an Ent client and clock")
	}
	return &PositionRepository{client: client, now: now}, nil
}

func (repository *PositionRepository) Open(ctx context.Context, input application.OpenPositionInput) (application.OpenPositionResult, error) {
	if repository == nil || repository.client == nil || repository.now == nil || strings.TrimSpace(input.IdempotencyKey) == "" {
		return application.OpenPositionResult{}, errors.New("position open request is incomplete")
	}
	if err := input.Fill.Quote.Validate(); err != nil || input.Fill.TokenQuantity == 0 || strings.TrimSpace(input.Fill.EntryPrice) == "" || strings.TrimSpace(input.Fill.QuoteHash) == "" {
		return application.OpenPositionResult{}, errors.New("position open fill is invalid")
	}
	tx, err := repository.client.Tx(ctx)
	if err != nil {
		return application.OpenPositionResult{}, fmt.Errorf("begin paper position open transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	decision, err := tx.TradeDecision.Query().Where(tradedecision.IdempotencyKeyEQ(input.IdempotencyKey)).Only(ctx)
	if ent.IsNotFound(err) {
		return application.OpenPositionResult{}, errors.New("paper admission decision does not exist")
	}
	if err != nil {
		return application.OpenPositionResult{}, fmt.Errorf("load paper admission decision: %w", err)
	}
	position, err := tx.TradeDecision.Query().Where(tradedecision.IDEQ(decision.ID)).QueryPosition().Only(ctx)
	if ent.IsNotFound(err) {
		return application.OpenPositionResult{}, errors.New("paper admission position does not exist")
	}
	if err != nil {
		return application.OpenPositionResult{}, fmt.Errorf("load paper admission position: %w", err)
	}
	if position.State == paperposition.StateOPEN {
		result, err := paperPositionResult(position)
		if err != nil {
			return application.OpenPositionResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return application.OpenPositionResult{}, fmt.Errorf("commit existing paper position: %w", err)
		}
		return application.OpenPositionResult{Position: result}, nil
	}
	if position.State != paperposition.StatePENDING {
		return application.OpenPositionResult{}, fmt.Errorf("paper position is not pending: %s", position.State)
	}

	now := repository.now().UTC()
	if now.IsZero() {
		return application.OpenPositionResult{}, errors.New("position repository clock returned zero time")
	}
	updated, err := tx.PaperPosition.Update().Where(
		paperposition.IDEQ(position.ID),
		paperposition.StateEQ(paperposition.StatePENDING),
	).SetState(paperposition.StateOPEN).
		SetQuoteMint(input.Fill.Quote.InputMint).
		SetMintAddress(input.Fill.Quote.OutputMint).
		SetEntryPrice(input.Fill.EntryPrice).
		SetEntryInputAmount(strconv.FormatUint(input.Fill.Quote.InAmount, 10)).
		SetEntryNetworkFeeMicros(input.Fill.NetworkFeeMicros).
		SetEntryPriorityFeeMicros(input.Fill.PriorityFeeMicros).
		SetTokenQuantity(strconv.FormatUint(input.Fill.TokenQuantity, 10)).
		SetOpenedAt(input.Fill.Quote.ObservedAt.UTC()).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return application.OpenPositionResult{}, fmt.Errorf("transition pending paper position: %w", err)
	}
	if updated == 0 {
		return application.OpenPositionResult{}, errors.New("paper position transition was not applied")
	}
	if err := tx.PositionEvent.Create().
		SetEventType(string(domain.PositionOpen)).
		SetDetails(openPositionEventDetails(input.Fill)).
		SetPositionID(position.ID).
		SetCreatedAt(now).
		Exec(ctx); err != nil {
		return application.OpenPositionResult{}, fmt.Errorf("persist paper position open event: %w", err)
	}
	opened, err := tx.PaperPosition.Get(ctx, position.ID)
	if err != nil {
		return application.OpenPositionResult{}, fmt.Errorf("load opened paper position: %w", err)
	}
	result, err := paperPositionResult(opened)
	if err != nil {
		return application.OpenPositionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return application.OpenPositionResult{}, fmt.Errorf("commit paper position open: %w", err)
	}
	return application.OpenPositionResult{Opened: true, Position: result}, nil
}

func (repository *PositionRepository) ListOpen(ctx context.Context) ([]domain.PaperPosition, error) {
	if repository == nil || repository.client == nil {
		return nil, errors.New("position repository is incomplete")
	}
	stored, err := repository.client.PaperPosition.Query().Where(paperposition.StateEQ(paperposition.StateOPEN)).Order(paperposition.ByOpenedAt()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("query open paper positions: %w", err)
	}
	positions := make([]domain.PaperPosition, 0, len(stored))
	for _, position := range stored {
		converted, err := paperPositionResult(position)
		if err != nil {
			return nil, fmt.Errorf("decode open paper position %d: %w", position.ID, err)
		}
		positions = append(positions, converted)
	}
	return positions, nil
}

func (repository *PositionRepository) RecordMark(ctx context.Context, input application.PositionMarkInput) error {
	if repository == nil || repository.client == nil || repository.now == nil || input.PositionID <= 0 {
		return errors.New("position mark request is incomplete")
	}
	if err := input.Mark.Quote.Validate(); err != nil || input.Mark.NetOutputAmount == 0 || strings.TrimSpace(input.Mark.QuoteHash) == "" || input.Mark.FeeEstimate > uint64(^uint64(0)>>1) {
		return errors.New("position mark is invalid")
	}
	if input.CloseReason != domain.PositionCloseNone && input.CloseReason != domain.PositionCloseTakeProfit && input.CloseReason != domain.PositionCloseStopLoss && input.CloseReason != domain.PositionCloseTimeout {
		return errors.New("position close reason is invalid")
	}
	now := repository.now().UTC()
	if now.IsZero() {
		return errors.New("position repository clock returned zero time")
	}
	tx, err := repository.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin paper position mark transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	position, err := tx.PaperPosition.Get(ctx, input.PositionID)
	if ent.IsNotFound(err) {
		return errors.New("paper position does not exist")
	}
	if err != nil {
		return fmt.Errorf("load paper position for mark: %w", err)
	}
	if position.State == paperposition.StateCLOSED {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit existing closed paper position: %w", err)
		}
		return nil
	}
	if position.State != paperposition.StateOPEN {
		return fmt.Errorf("paper position is not open: %s", position.State)
	}
	previousMarks, err := tx.PositionMark.Query().Where(positionmark.HasPositionWith(paperposition.IDEQ(position.ID))).All(ctx)
	if err != nil {
		return fmt.Errorf("load prior paper position marks: %w", err)
	}
	mfeBPS, maeBPS := input.Mark.ReturnBPS, input.Mark.ReturnBPS
	for _, prior := range previousMarks {
		if prior.ReturnBps == nil {
			continue
		}
		if *prior.ReturnBps > mfeBPS {
			mfeBPS = *prior.ReturnBps
		}
		if *prior.ReturnBps < maeBPS {
			maeBPS = *prior.ReturnBps
		}
	}
	if err := tx.PositionMark.Create().
		SetPrice(strconv.FormatUint(input.Mark.NetOutputAmount, 10) + "/" + strconv.FormatUint(input.Mark.Quote.InAmount, 10)).
		SetNetOutputAmount(strconv.FormatUint(input.Mark.NetOutputAmount, 10)).
		SetFeeEstimate(int64(input.Mark.FeeEstimate)).
		SetReturnBps(input.Mark.ReturnBPS).
		SetQuoteHash(input.Mark.QuoteHash).
		SetRouteState(positionmark.RouteStateEXECUTABLE).
		SetMfeBps(mfeBPS).
		SetMaeBps(maeBPS).
		SetNoRouteCount(0).
		SetObservedAt(input.Mark.Quote.ObservedAt.UTC()).
		SetCreatedAt(now).
		SetPositionID(position.ID).
		Exec(ctx); err != nil {
		return fmt.Errorf("persist paper position mark: %w", err)
	}
	positionUpdate := tx.PaperPosition.Update().Where(paperposition.IDEQ(position.ID), paperposition.StateEQ(paperposition.StateOPEN), paperposition.NoRouteCountEQ(position.NoRouteCount)).SetNoRouteCount(0).SetUpdatedAt(now)
	if input.CloseReason != domain.PositionCloseNone {
		positionUpdate.SetState(paperposition.StateCLOSED).SetClosedAt(now)
	}
	updated, err := positionUpdate.Save(ctx)
	if err != nil {
		return fmt.Errorf("update paper position after executable mark: %w", err)
	}
	if updated == 0 {
		return errors.New("paper position executable mark transition was not applied")
	}
	if input.CloseReason != domain.PositionCloseNone {
		if err := tx.PositionEvent.Create().SetEventType(string(paperposition.StateCLOSED)).SetDetails(map[string]any{"reason": string(input.CloseReason), "quote_hash": input.Mark.QuoteHash, "return_bps": input.Mark.ReturnBPS, "net_output_amount": strconv.FormatUint(input.Mark.NetOutputAmount, 10)}).SetPositionID(position.ID).SetCreatedAt(now).Exec(ctx); err != nil {
			return fmt.Errorf("persist paper position closed event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit paper position mark: %w", err)
	}
	return nil
}

var errPositionNoRouteConflict = errors.New("paper position no-route transition conflicted")

func (repository *PositionRepository) RecordNoRoute(ctx context.Context, input application.PositionNoRouteInput) error {
	if repository == nil || repository.client == nil || repository.now == nil || input.PositionID <= 0 || input.ObservedAt.IsZero() {
		return errors.New("position no-route request is incomplete")
	}
	for attempt := 0; attempt < 3; attempt++ {
		err := repository.recordNoRouteOnce(ctx, input)
		if !errors.Is(err, errPositionNoRouteConflict) {
			return err
		}
	}
	return errors.New("paper position no-route transition conflicted repeatedly")
}

func (repository *PositionRepository) recordNoRouteOnce(ctx context.Context, input application.PositionNoRouteInput) error {
	now := repository.now().UTC()
	if now.IsZero() {
		return errors.New("position repository clock returned zero time")
	}
	tx, err := repository.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin paper position no-route transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	position, err := tx.PaperPosition.Get(ctx, input.PositionID)
	if ent.IsNotFound(err) {
		return errors.New("paper position does not exist")
	}
	if err != nil {
		return fmt.Errorf("load paper position for no-route observation: %w", err)
	}
	if position.State == paperposition.StateCLOSED || position.State == paperposition.StateUNSELLABLE {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit terminal paper position no-route observation: %w", err)
		}
		return nil
	}
	if position.State != paperposition.StateOPEN || position.NoRouteCount >= domain.MaxConsecutiveNoRoutes {
		return fmt.Errorf("paper position cannot record no-route observation: state=%s count=%d", position.State, position.NoRouteCount)
	}
	previousMarks, err := tx.PositionMark.Query().Where(positionmark.HasPositionWith(paperposition.IDEQ(position.ID))).All(ctx)
	if err != nil {
		return fmt.Errorf("load prior paper position marks: %w", err)
	}
	var mfeBPS, maeBPS *int64
	for _, prior := range previousMarks {
		if prior.ReturnBps == nil {
			continue
		}
		value := *prior.ReturnBps
		if mfeBPS == nil || value > *mfeBPS {
			mfeBPS = &value
		}
		if maeBPS == nil || value < *maeBPS {
			maeBPS = &value
		}
	}
	nextCount := position.NoRouteCount + 1
	terminal := nextCount == domain.MaxConsecutiveNoRoutes
	markCreate := tx.PositionMark.Create().
		SetPrice("NO_ROUTE").
		SetRouteState(positionmark.RouteStateNO_ROUTE).
		SetNoRouteCount(nextCount).
		SetNillableMfeBps(mfeBPS).
		SetNillableMaeBps(maeBPS).
		SetObservedAt(input.ObservedAt.UTC()).
		SetCreatedAt(now).
		SetPositionID(position.ID)
	if terminal {
		markCreate.SetReturnBps(domain.UnsellableReturnBPS)
	}
	if err := markCreate.Exec(ctx); err != nil {
		return fmt.Errorf("persist paper position no-route mark: %w", err)
	}
	positionUpdate := tx.PaperPosition.Update().Where(paperposition.IDEQ(position.ID), paperposition.StateEQ(paperposition.StateOPEN), paperposition.NoRouteCountEQ(position.NoRouteCount)).SetNoRouteCount(nextCount).SetUpdatedAt(now)
	if terminal {
		positionUpdate.SetState(paperposition.StateUNSELLABLE).SetClosedAt(now)
	}
	updated, err := positionUpdate.Save(ctx)
	if err != nil {
		return fmt.Errorf("update paper position no-route count: %w", err)
	}
	if updated == 0 {
		return errPositionNoRouteConflict
	}
	if terminal {
		if err := tx.PositionEvent.Create().SetEventType(string(paperposition.StateUNSELLABLE)).SetDetails(map[string]any{"reason": "NO_ROUTE_LIMIT", "no_route_count": nextCount, "return_bps": domain.UnsellableReturnBPS}).SetPositionID(position.ID).SetCreatedAt(now).Exec(ctx); err != nil {
			return fmt.Errorf("persist paper position unsellable event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit paper position no-route observation: %w", err)
	}
	return nil
}

func paperPositionResult(position *ent.PaperPosition) (domain.PaperPosition, error) {
	if position == nil || position.QuoteMint == nil || position.MintAddress == nil || position.EntryPrice == nil || position.EntryInputAmount == nil || position.EntryNetworkFeeMicros == nil || position.EntryPriorityFeeMicros == nil || position.TokenQuantity == nil || position.OpenedAt == nil {
		return domain.PaperPosition{}, errors.New("opened paper position is incomplete")
	}
	entryInputAmount, err := strconv.ParseUint(*position.EntryInputAmount, 10, 64)
	if err != nil || entryInputAmount == 0 || *position.EntryNetworkFeeMicros < 0 || *position.EntryPriorityFeeMicros < 0 {
		return domain.PaperPosition{}, errors.New("opened paper position has invalid entry metadata")
	}
	quantity, err := strconv.ParseUint(*position.TokenQuantity, 10, 64)
	if err != nil || quantity == 0 {
		return domain.PaperPosition{}, errors.New("opened paper position has invalid token quantity")
	}
	return domain.PaperPosition{ID: position.ID, State: domain.PositionState(position.State), NotionalMicros: position.NotionalMicros, NoRouteCount: position.NoRouteCount, QuoteMint: *position.QuoteMint, MintAddress: *position.MintAddress, EntryPrice: *position.EntryPrice, EntryInputAmount: entryInputAmount, EntryNetworkFeeMicros: *position.EntryNetworkFeeMicros, EntryPriorityFeeMicros: *position.EntryPriorityFeeMicros, TokenQuantity: quantity, OpenedAt: *position.OpenedAt}, nil
}

func openPositionEventDetails(fill domain.EntryFill) map[string]any {
	route := make([]map[string]any, 0, len(fill.Quote.RoutePlan))
	for _, leg := range fill.Quote.RoutePlan {
		route = append(route, map[string]any{
			"amm_key": leg.AMMKey, "label": leg.Label, "fee_amount": strconv.FormatUint(leg.Fee.Amount, 10), "fee_mint": leg.Fee.Mint,
		})
	}
	return map[string]any{
		"quote_hash": fill.QuoteHash,
		"quote": map[string]any{
			"observed_at": fill.Quote.ObservedAt.UTC().Format(time.RFC3339Nano), "input_mint": fill.Quote.InputMint, "output_mint": fill.Quote.OutputMint,
			"in_amount": strconv.FormatUint(fill.Quote.InAmount, 10), "out_amount": strconv.FormatUint(fill.Quote.OutAmount, 10), "price_impact_bps": fill.Quote.PriceImpactBPS,
			"platform_fee_amount": strconv.FormatUint(fill.Quote.PlatformFee.Amount, 10), "platform_fee_mint": fill.Quote.PlatformFee.Mint, "route_plan": route,
		},
		"entry_price": fill.EntryPrice, "token_quantity": strconv.FormatUint(fill.TokenQuantity, 10), "network_fee_micros": fill.NetworkFeeMicros, "priority_fee_micros": fill.PriorityFeeMicros,
	}
}

var _ application.PositionOpener = (*PositionRepository)(nil)
var _ application.PositionManagerRepository = (*PositionRepository)(nil)
