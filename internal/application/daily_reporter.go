package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type DailyResultsRepository interface {
	Reconcile(context.Context, time.Time) error
	List(context.Context, time.Time) ([]domain.DailyResult, error)
}

type DailyReportWriter interface {
	Write(context.Context, time.Time, []domain.DailyResult) error
}

type DailyReporterOptions struct {
	Repository DailyResultsRepository
	Writer     DailyReportWriter
}

type DailyReporter struct{ options DailyReporterOptions }

func NewDailyReporter(options DailyReporterOptions) *DailyReporter {
	return &DailyReporter{options: options}
}

func (reporter *DailyReporter) Generate(ctx context.Context, date time.Time) ([]domain.DailyResult, error) {
	if reporter == nil || reporter.options.Repository == nil || reporter.options.Writer == nil || date.IsZero() {
		return nil, errors.New("daily reporter is not completely configured")
	}
	utcDate := time.Date(date.UTC().Year(), date.UTC().Month(), date.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if err := reporter.options.Repository.Reconcile(ctx, utcDate); err != nil {
		return nil, fmt.Errorf("reconcile daily result: %w", err)
	}
	results, err := reporter.options.Repository.List(ctx, utcDate)
	if err != nil {
		return nil, fmt.Errorf("load reconciled daily results: %w", err)
	}
	if err := reporter.options.Writer.Write(ctx, utcDate, results); err != nil {
		return nil, fmt.Errorf("write retained daily report: %w", err)
	}
	return results, nil
}
