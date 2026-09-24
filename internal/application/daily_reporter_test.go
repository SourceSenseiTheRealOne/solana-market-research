package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestDailyReporterReconcilesThenWritesPersistedSnapshot(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	results := []domain.DailyResult{{UTCDate: date, StrategyVersion: "strategy-a", DailyAdmittedCount: 2, RealizedPNLMicros: 1_500_000}}
	repository := &dailyResultsRepositoryFake{results: results}
	writer := &dailyReportWriterFake{}
	reporter := application.NewDailyReporter(application.DailyReporterOptions{Repository: repository, Writer: writer})

	got, err := reporter.Generate(context.Background(), date)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !repository.reconciled || repository.listed != 1 || writer.calls != 1 || writer.date != date || len(got) != 1 || got[0] != results[0] || len(writer.results) != 1 || writer.results[0] != results[0] {
		t.Fatalf("Generate() repository=%#v writer=%#v results=%#v, want persisted daily snapshot written exactly once", repository, writer, got)
	}
}

type dailyResultsRepositoryFake struct {
	results    []domain.DailyResult
	reconciled bool
	listed     int
}

func (fake *dailyResultsRepositoryFake) Reconcile(_ context.Context, _ time.Time) error {
	fake.reconciled = true
	return nil
}

func (fake *dailyResultsRepositoryFake) List(_ context.Context, _ time.Time) ([]domain.DailyResult, error) {
	fake.listed++
	return fake.results, nil
}

type dailyReportWriterFake struct {
	calls   int
	date    time.Time
	results []domain.DailyResult
}

func (fake *dailyReportWriterFake) Write(_ context.Context, date time.Time, results []domain.DailyResult) error {
	fake.calls++
	fake.date = date
	fake.results = results
	return nil
}
