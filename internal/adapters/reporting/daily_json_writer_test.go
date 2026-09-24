package reporting_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/reporting"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestDailyJSONReportWriterWritesStableUTCSnapshot(t *testing.T) {
	directory := t.TempDir()
	writer, err := reporting.NewDailyJSONReportWriter(directory)
	if err != nil {
		t.Fatalf("NewDailyJSONReportWriter() error = %v", err)
	}
	date := time.Date(2026, time.August, 20, 13, 14, 15, 0, time.FixedZone("UTC-4", -4*60*60))
	results := []domain.DailyResult{
		{UTCDate: date.UTC(), StrategyVersion: "strategy-z", DailyAdmittedCount: 1, RealizedPNLMicros: -10_000_000},
		{UTCDate: date.UTC(), StrategyVersion: "strategy-a", DailyAdmittedCount: 2, RealizedPNLMicros: 1_500_000},
	}

	if err := writer.Write(context.Background(), date, results); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	path := filepath.Join(directory, "2026-08-20.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first report: %v", err)
	}
	if err := writer.Write(context.Background(), date, results); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read second report: %v", err)
	}
	const want = "{\n  \"utc_date\": \"2026-08-20\",\n  \"results\": [\n    {\n      \"strategy_version\": \"strategy-a\",\n      \"daily_admitted_count\": 2,\n      \"realized_pnl_micros\": 1500000\n    },\n    {\n      \"strategy_version\": \"strategy-z\",\n      \"daily_admitted_count\": 1,\n      \"realized_pnl_micros\": -10000000\n    }\n  ]\n}\n"
	if string(first) != want || string(second) != want {
		t.Fatalf("report bytes = %q then %q, want %q", first, second, want)
	}
}

func TestDailyJSONReportWriterRejectsDuplicateStrategyVersions(t *testing.T) {
	writer, err := reporting.NewDailyJSONReportWriter(t.TempDir())
	if err != nil {
		t.Fatalf("NewDailyJSONReportWriter() error = %v", err)
	}
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	err = writer.Write(context.Background(), date, []domain.DailyResult{
		{UTCDate: date, StrategyVersion: "strategy-a", DailyAdmittedCount: 1},
		{UTCDate: date, StrategyVersion: "strategy-a", DailyAdmittedCount: 2},
	})
	if err == nil {
		t.Fatal("Write() error = nil, want duplicate strategy rejection")
	}
}
