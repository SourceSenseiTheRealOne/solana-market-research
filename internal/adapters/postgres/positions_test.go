package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/positionevent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/positionmark"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestPositionRepositoryTransitionsPendingToOpenExactlyOnce(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC)
	storedCandidate := createAdmissionCandidate(t, client, "position-open", now)
	const idempotencyKey = "position-open-key"
	decision, err := client.TradeDecision.Create().SetIdempotencyKey(idempotencyKey).SetOutcome("BUY").SetRuleResults(map[string]any{"source": "test"}).SetCandidateID(storedCandidate.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed trade decision: %v", err)
	}
	pending, err := client.PaperPosition.Create().SetState(paperposition.StatePENDING).SetNotionalMicros(10_000_000).SetCandidateID(storedCandidate.ID).SetDecisionID(decision.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed pending position: %v", err)
	}
	if err := client.PositionEvent.Create().SetEventType(string(domain.PositionPending)).SetDetails(map[string]any{"source": "test"}).SetPositionID(pending.ID).SetCreatedAt(now.Add(-2 * time.Second)).Exec(context.Background()); err != nil {
		t.Fatalf("seed pending event: %v", err)
	}

	repository, err := postgres.NewPositionRepository(client, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewPositionRepository() error = %v", err)
	}
	fill := validPositionFill(t, now)
	first, err := repository.Open(context.Background(), application.OpenPositionInput{IdempotencyKey: idempotencyKey, Fill: fill})
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if !first.Opened || first.Position.State != domain.PositionOpen || first.Position.QuoteMint != fill.Quote.InputMint || first.Position.MintAddress != fill.Quote.OutputMint || first.Position.EntryInputAmount != fill.Quote.InAmount || first.Position.EntryNetworkFeeMicros != fill.NetworkFeeMicros || first.Position.EntryPriorityFeeMicros != fill.PriorityFeeMicros || first.Position.EntryPrice != "10000000/25000000" || first.Position.TokenQuantity != 25_000_000 {
		t.Fatalf("first Open() = %#v, want persisted OPEN fill", first)
	}

	second, err := repository.Open(context.Background(), application.OpenPositionInput{IdempotencyKey: idempotencyKey, Fill: fill})
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if second.Opened || second.Position.ID != first.Position.ID || second.Position.State != domain.PositionOpen {
		t.Fatalf("second Open() = %#v, want the existing OPEN position", second)
	}
	if got, err := client.PositionEvent.Query().Where(positionevent.HasPositionWith(paperposition.IDEQ(pending.ID)), positionevent.EventTypeEQ(string(domain.PositionPending))).Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("PENDING events = %d, %v; want 1, nil", got, err)
	}
	if got, err := client.PositionEvent.Query().Where(positionevent.HasPositionWith(paperposition.IDEQ(pending.ID)), positionevent.EventTypeEQ(string(domain.PositionOpen))).Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("OPEN events = %d, %v; want 1, nil", got, err)
	}
	event, err := client.PositionEvent.Query().Where(positionevent.HasPositionWith(paperposition.IDEQ(pending.ID)), positionevent.EventTypeEQ(string(domain.PositionOpen))).Only(context.Background())
	if err != nil {
		t.Fatalf("load OPEN event: %v", err)
	}
	if got, want := event.Details["quote_hash"], fill.QuoteHash; got != want {
		t.Fatalf("OPEN event quote hash = %#v, want %#v", got, want)
	}
}

func TestPositionRepositoryRecordsTerminalMarkExactlyOnce(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC)
	storedCandidate := createAdmissionCandidate(t, client, "position-close", now)
	decision, err := client.TradeDecision.Create().SetIdempotencyKey("position-close-key").SetOutcome("BUY").SetRuleResults(map[string]any{"source": "test"}).SetCandidateID(storedCandidate.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed trade decision: %v", err)
	}
	pending, err := client.PaperPosition.Create().SetState(paperposition.StatePENDING).SetNotionalMicros(10_000_000).SetCandidateID(storedCandidate.ID).SetDecisionID(decision.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed pending position: %v", err)
	}
	repository, err := postgres.NewPositionRepository(client, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewPositionRepository() error = %v", err)
	}
	opened, err := repository.Open(context.Background(), application.OpenPositionInput{IdempotencyKey: "position-close-key", Fill: validPositionFill(t, now)})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	exitQuote := domain.QuoteEvidence{ObservedAt: now.Add(30 * time.Second), InputMint: opened.Position.MintAddress, OutputMint: opened.Position.QuoteMint, InAmount: opened.Position.TokenQuantity, OutAmount: 13_006_900, RoutePlan: []domain.RouteLeg{{AMMKey: "route-one"}}}
	mark, err := domain.NewExitMark(opened.Position, exitQuote, 1_000, 2_000)
	if err != nil {
		t.Fatalf("NewExitMark() error = %v", err)
	}
	input := application.PositionMarkInput{PositionID: pending.ID, Mark: mark, CloseReason: domain.PositionCloseTakeProfit}
	if err := repository.RecordMark(context.Background(), input); err != nil {
		t.Fatalf("first RecordMark() error = %v", err)
	}
	if err := repository.RecordMark(context.Background(), input); err != nil {
		t.Fatalf("second RecordMark() error = %v", err)
	}

	position, err := client.PaperPosition.Get(context.Background(), pending.ID)
	if err != nil {
		t.Fatalf("load terminal position: %v", err)
	}
	if position.State != paperposition.StateCLOSED || position.ClosedAt == nil || !position.ClosedAt.Equal(now) {
		t.Fatalf("terminal position = %#v, want CLOSED at %s", position, now)
	}
	storedMark, err := client.PositionMark.Query().Where(positionmark.HasPositionWith(paperposition.IDEQ(pending.ID))).Only(context.Background())
	if err != nil {
		t.Fatalf("load terminal mark: %v", err)
	}
	if storedMark.NetOutputAmount == nil || *storedMark.NetOutputAmount != "13003900" || storedMark.FeeEstimate == nil || *storedMark.FeeEstimate != 3_000 || storedMark.ReturnBps == nil || *storedMark.ReturnBps != 3_000 || storedMark.QuoteHash == nil || *storedMark.QuoteHash != mark.QuoteHash || storedMark.RouteState != positionmark.RouteStateEXECUTABLE {
		t.Fatalf("stored terminal mark = %#v, want bounded executable mark", storedMark)
	}
	if got, err := client.PositionEvent.Query().Where(positionevent.HasPositionWith(paperposition.IDEQ(pending.ID)), positionevent.EventTypeEQ("CLOSED")).Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("CLOSED events = %d, %v; want 1, nil", got, err)
	}
}

func TestPositionRepositoryListsOpenPositionsWithEntryBasis(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC)
	storedCandidate := createAdmissionCandidate(t, client, "position-list-open", now)
	decision, err := client.TradeDecision.Create().SetIdempotencyKey("position-list-open-key").SetOutcome("BUY").SetRuleResults(map[string]any{"source": "test"}).SetCandidateID(storedCandidate.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed trade decision: %v", err)
	}
	if _, err := client.PaperPosition.Create().SetState(paperposition.StatePENDING).SetNotionalMicros(10_000_000).SetCandidateID(storedCandidate.ID).SetDecisionID(decision.ID).Save(context.Background()); err != nil {
		t.Fatalf("seed pending position: %v", err)
	}
	repository, err := postgres.NewPositionRepository(client, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewPositionRepository() error = %v", err)
	}
	opened, err := repository.Open(context.Background(), application.OpenPositionInput{IdempotencyKey: "position-list-open-key", Fill: validPositionFill(t, now)})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := repository.RecordNoRoute(context.Background(), application.PositionNoRouteInput{PositionID: opened.Position.ID, ObservedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("RecordNoRoute() error = %v", err)
	}
	recoveredRepository, err := postgres.NewPositionRepository(client, func() time.Time { return now.Add(2 * time.Minute) })
	if err != nil {
		t.Fatalf("recreate PositionRepository() error = %v", err)
	}

	positions, err := recoveredRepository.ListOpen(context.Background())
	if err != nil {
		t.Fatalf("ListOpen() error = %v", err)
	}
	for _, position := range positions {
		if position.ID != opened.Position.ID {
			continue
		}
		if position.NoRouteCount != 1 || position.QuoteMint != opened.Position.QuoteMint || position.MintAddress != opened.Position.MintAddress || position.EntryInputAmount != opened.Position.EntryInputAmount || position.TokenQuantity != opened.Position.TokenQuantity {
			t.Fatalf("recovered position = %#v, want entry basis with no-route count", position)
		}
		return
	}
	t.Fatalf("ListOpen() = %#v, want the recovered open position", positions)
}

func TestPositionRepositoryMarksUnsellableAfterFiveConfirmedNoRoutes(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC)
	storedCandidate := createAdmissionCandidate(t, client, "position-no-route", now)
	decision, err := client.TradeDecision.Create().SetIdempotencyKey("position-no-route-key").SetOutcome("BUY").SetRuleResults(map[string]any{"source": "test"}).SetCandidateID(storedCandidate.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed trade decision: %v", err)
	}
	pending, err := client.PaperPosition.Create().SetState(paperposition.StatePENDING).SetNotionalMicros(10_000_000).SetCandidateID(storedCandidate.ID).SetDecisionID(decision.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed pending position: %v", err)
	}
	repository, err := postgres.NewPositionRepository(client, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewPositionRepository() error = %v", err)
	}
	if _, err := repository.Open(context.Background(), application.OpenPositionInput{IdempotencyKey: "position-no-route-key", Fill: validPositionFill(t, now)}); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	for attempt := 1; attempt <= 6; attempt++ {
		if err := repository.RecordNoRoute(context.Background(), application.PositionNoRouteInput{PositionID: pending.ID, ObservedAt: now.Add(time.Duration(attempt) * time.Minute)}); err != nil {
			t.Fatalf("RecordNoRoute() attempt %d error = %v", attempt, err)
		}
	}

	position, err := client.PaperPosition.Get(context.Background(), pending.ID)
	if err != nil {
		t.Fatalf("load terminal position: %v", err)
	}
	if position.State != paperposition.StateUNSELLABLE || position.ClosedAt == nil || !position.ClosedAt.Equal(now) {
		t.Fatalf("terminal position = %#v, want UNSELLABLE at %s", position, now)
	}
	marks, err := client.PositionMark.Query().Where(positionmark.HasPositionWith(paperposition.IDEQ(pending.ID))).Order(positionmark.ByNoRouteCount()).All(context.Background())
	if err != nil {
		t.Fatalf("load no-route marks: %v", err)
	}
	if len(marks) != 5 {
		t.Fatalf("NO_ROUTE marks = %d, want 5", len(marks))
	}
	for index, mark := range marks {
		if mark.RouteState != positionmark.RouteStateNO_ROUTE || mark.NoRouteCount != index+1 || mark.NetOutputAmount != nil || mark.FeeEstimate != nil || mark.QuoteHash != nil {
			t.Fatalf("NO_ROUTE mark %d = %#v, want bounded no-route observation", index+1, mark)
		}
		if index < 4 && mark.ReturnBps != nil {
			t.Fatalf("non-terminal NO_ROUTE mark %d return BPS = %#v, want nil", index+1, mark.ReturnBps)
		}
	}
	if marks[4].ReturnBps == nil || *marks[4].ReturnBps != -10_000 {
		t.Fatalf("terminal NO_ROUTE return BPS = %#v, want -10000", marks[4].ReturnBps)
	}
	unsellableEvent, err := client.PositionEvent.Query().Where(positionevent.HasPositionWith(paperposition.IDEQ(pending.ID)), positionevent.EventTypeEQ("UNSELLABLE")).Only(context.Background())
	if err != nil {
		t.Fatalf("load UNSELLABLE event: %v", err)
	}
	if got, want := unsellableEvent.Details["return_bps"], float64(domain.UnsellableReturnBPS); got != want {
		t.Fatalf("UNSELLABLE event return BPS = %#v, want %#v", got, want)
	}
}

func TestPositionRepositoryResetsNoRouteStreakAfterExecutableMark(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 2, 0, time.UTC)
	storedCandidate := createAdmissionCandidate(t, client, "position-reset-no-route", now)
	decision, err := client.TradeDecision.Create().SetIdempotencyKey("position-reset-no-route-key").SetOutcome("BUY").SetRuleResults(map[string]any{"source": "test"}).SetCandidateID(storedCandidate.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed trade decision: %v", err)
	}
	pending, err := client.PaperPosition.Create().SetState(paperposition.StatePENDING).SetNotionalMicros(10_000_000).SetCandidateID(storedCandidate.ID).SetDecisionID(decision.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("seed pending position: %v", err)
	}
	repository, err := postgres.NewPositionRepository(client, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewPositionRepository() error = %v", err)
	}
	opened, err := repository.Open(context.Background(), application.OpenPositionInput{IdempotencyKey: "position-reset-no-route-key", Fill: validPositionFill(t, now)})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := repository.RecordNoRoute(context.Background(), application.PositionNoRouteInput{PositionID: pending.ID, ObservedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("RecordNoRoute() error = %v", err)
	}
	exitQuote := domain.QuoteEvidence{ObservedAt: now.Add(2 * time.Minute), InputMint: opened.Position.MintAddress, OutputMint: opened.Position.QuoteMint, InAmount: opened.Position.TokenQuantity, OutAmount: 10_004_000, RoutePlan: []domain.RouteLeg{{AMMKey: "route-one"}}}
	mark, err := domain.NewExitMark(opened.Position, exitQuote, 1_000, 2_000)
	if err != nil {
		t.Fatalf("NewExitMark() error = %v", err)
	}
	if err := repository.RecordMark(context.Background(), application.PositionMarkInput{PositionID: pending.ID, Mark: mark, CloseReason: domain.PositionCloseNone}); err != nil {
		t.Fatalf("RecordMark() error = %v", err)
	}

	position, err := client.PaperPosition.Get(context.Background(), pending.ID)
	if err != nil {
		t.Fatalf("load position: %v", err)
	}
	if position.State != paperposition.StateOPEN || position.NoRouteCount != 0 || position.ClosedAt != nil {
		t.Fatalf("position after executable mark = %#v, want OPEN with a reset no-route count", position)
	}
}

func validPositionFill(t *testing.T, observedAt time.Time) domain.EntryFill {
	t.Helper()
	fill, err := domain.NewEntryFill(domain.QuoteEvidence{
		ObservedAt: observedAt, InputMint: "quote-mint", OutputMint: "candidate-mint", InAmount: 10_000_000, OutAmount: 25_000_000,
		PriceImpactBPS: 125, RoutePlan: []domain.RouteLeg{{AMMKey: "route-one", Label: "Raydium", Fee: domain.QuoteFee{Amount: 31, Mint: "candidate-mint"}}},
	}, 1_000, 2_000)
	if err != nil {
		t.Fatalf("NewEntryFill() error = %v", err)
	}
	return fill
}
