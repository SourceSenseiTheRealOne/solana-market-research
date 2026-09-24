package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
)

func TestDashboardReadServiceRequiresAndProjectsStrategyWithNoTrades(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	positions := &dashboardPositionsFake{}
	options := application.DashboardReadServiceOptions{
		Positions: positions, DailyResults: &dashboardDailyResultsFake{},
		TwitterAnalytics: &dashboardTwitterAnalyticsFake{}, AutomationActivity: &dashboardActivityFake{},
		ReviewedCandidates: &dashboardReviewedCandidatesFake{},
	}
	if _, err := application.NewDashboardReadService(options).Read(context.Background(), date); err == nil {
		t.Fatal("Read() accepted an empty strategy version")
	}
	if positions.calls != 0 {
		t.Fatalf("position reads = %d, want zero before configuration validation", positions.calls)
	}

	options.StrategyVersion = "bold-momentum-v2"
	snapshot, err := application.NewDashboardReadService(options).Read(context.Background(), date)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if snapshot.StrategyVersion != "bold-momentum-v2" || len(snapshot.DailyResults) != 0 || len(snapshot.OpenPositions) != 0 {
		t.Fatalf("snapshot = %#v, want active strategy with empty trade data", snapshot)
	}
}
