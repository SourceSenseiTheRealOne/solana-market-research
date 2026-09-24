package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/dailyresult"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/positionmark"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
)

func TestDailyResultRepositoryReconcilesTerminalPositionsIdempotently(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	strategyA := admissionTestStrategyVersion("daily-a")
	strategyB := admissionTestStrategyVersion("daily-b")
	if err := client.DailyResult.Create().SetUtcDate(date).SetStrategyVersion(strategyA).SetDailyAdmittedCount(2).SetRealizedPnlMicros(99).Exec(context.Background()); err != nil {
		t.Fatalf("seed strategy A daily result: %v", err)
	}
	if err := client.DailyResult.Create().SetUtcDate(date).SetStrategyVersion(strategyB).SetDailyAdmittedCount(1).SetRealizedPnlMicros(99).Exec(context.Background()); err != nil {
		t.Fatalf("seed strategy B daily result: %v", err)
	}
	createDailyTerminalPosition(t, client, "daily-a-profit", strategyA, date.Add(2*time.Hour), 3_000)
	createDailyTerminalPosition(t, client, "daily-a-loss", strategyA, date.Add(3*time.Hour), -1_500)
	createDailyTerminalPosition(t, client, "daily-b-unsellable", strategyB, date.Add(4*time.Hour), -10_000)
	createDailyTerminalPosition(t, client, "outside-day", strategyA, date.AddDate(0, 0, 1), 3_000)

	repository, err := postgres.NewDailyResultRepository(client, func() time.Time { return date.Add(12 * time.Hour) })
	if err != nil {
		t.Fatalf("NewDailyResultRepository() error = %v", err)
	}
	if err := repository.Reconcile(context.Background(), date); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if err := repository.Reconcile(context.Background(), date); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	results, err := repository.List(context.Background(), date)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(results) != 2 || results[0].UTCDate != date || results[0].StrategyVersion != strategyA || results[0].DailyAdmittedCount != 2 || results[0].RealizedPNLMicros != 1_500_000 || results[1].UTCDate != date || results[1].StrategyVersion != strategyB || results[1].DailyAdmittedCount != 1 || results[1].RealizedPNLMicros != -10_000_000 {
		t.Fatalf("List() = %#v, want ordered reconciled results", results)
	}

	assertDailyResult(t, client, date, strategyA, 2, 1_500_000)
	assertDailyResult(t, client, date, strategyB, 1, -10_000_000)
	if got, err := client.DailyResult.Query().Where(dailyresult.UtcDateEQ(date.AddDate(0, 0, 1)), dailyresult.StrategyVersionEQ(strategyA)).Count(context.Background()); err != nil || got != 0 {
		t.Fatalf("next UTC day daily result rows = %d, %v; want 0, nil", got, err)
	}
}

func createDailyTerminalPosition(t *testing.T, client *ent.Client, suffix, strategyVersion string, closedAt time.Time, returnBPS int64) {
	t.Helper()
	candidate := createAdmissionCandidate(t, client, suffix, closedAt)
	decision, err := client.TradeDecision.Create().SetIdempotencyKey("daily-" + suffix).SetOutcome("BUY").SetRuleResults(map[string]any{"source": "daily-result-test"}).SetCandidateID(candidate.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("create terminal decision %q: %v", suffix, err)
	}
	position, err := client.PaperPosition.Create().SetState(paperposition.StateCLOSED).SetNotionalMicros(10_000_000).SetStrategyVersion(strategyVersion).SetClosedAt(closedAt).SetCandidateID(candidate.ID).SetDecisionID(decision.ID).Save(context.Background())
	if err != nil {
		t.Fatalf("create terminal position %q: %v", suffix, err)
	}
	if err := client.PositionMark.Create().SetPrice("terminal").SetReturnBps(returnBPS).SetRouteState(positionmark.RouteStateEXECUTABLE).SetNoRouteCount(0).SetObservedAt(closedAt).SetCreatedAt(closedAt).SetPositionID(position.ID).Exec(context.Background()); err != nil {
		t.Fatalf("create terminal mark %q: %v", suffix, err)
	}
}

func assertDailyResult(t *testing.T, client *ent.Client, date time.Time, strategyVersion string, admittedCount int, realizedPNLMicros int64) {
	t.Helper()
	stored, err := client.DailyResult.Query().Where(dailyresult.UtcDateEQ(date), dailyresult.StrategyVersionEQ(strategyVersion)).Only(context.Background())
	if err != nil {
		t.Fatalf("load daily result for %q: %v", strategyVersion, err)
	}
	if stored.DailyAdmittedCount != admittedCount || stored.RealizedPnlMicros != realizedPNLMicros {
		t.Fatalf("daily result for %q = %#v, want admitted=%d P&L=%d", strategyVersion, stored, admittedCount, realizedPNLMicros)
	}
}
