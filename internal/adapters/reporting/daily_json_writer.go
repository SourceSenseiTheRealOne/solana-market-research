package reporting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type DailyJSONReportWriter struct {
	directory string
}

type dailySnapshot struct {
	UTCDate string                `json:"utc_date"`
	Results []dailySnapshotResult `json:"results"`
}

type dailySnapshotResult struct {
	StrategyVersion    string `json:"strategy_version"`
	DailyAdmittedCount int    `json:"daily_admitted_count"`
	RealizedPNLMicros  int64  `json:"realized_pnl_micros"`
}

func NewDailyJSONReportWriter(directory string) (*DailyJSONReportWriter, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("daily report directory is required")
	}
	return &DailyJSONReportWriter{directory: filepath.Clean(directory)}, nil
}

func (writer *DailyJSONReportWriter) Write(ctx context.Context, date time.Time, results []domain.DailyResult) error {
	if writer == nil || strings.TrimSpace(writer.directory) == "" || date.IsZero() {
		return errors.New("daily JSON report request is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write daily JSON report: %w", err)
	}
	utcDate := dayUTC(date)
	snapshotResults := make([]dailySnapshotResult, 0, len(results))
	strategyVersions := make(map[string]struct{}, len(results))
	for _, result := range results {
		if dayUTC(result.UTCDate) != utcDate || strings.TrimSpace(result.StrategyVersion) == "" || result.DailyAdmittedCount < 0 {
			return errors.New("daily JSON report results do not match the requested UTC date")
		}
		if _, exists := strategyVersions[result.StrategyVersion]; exists {
			return fmt.Errorf("daily JSON report has duplicate strategy version %q", result.StrategyVersion)
		}
		strategyVersions[result.StrategyVersion] = struct{}{}
		snapshotResults = append(snapshotResults, dailySnapshotResult{
			StrategyVersion:    result.StrategyVersion,
			DailyAdmittedCount: result.DailyAdmittedCount,
			RealizedPNLMicros:  result.RealizedPNLMicros,
		})
	}
	sort.Slice(snapshotResults, func(left, right int) bool {
		return snapshotResults[left].StrategyVersion < snapshotResults[right].StrategyVersion
	})
	body, err := json.MarshalIndent(dailySnapshot{UTCDate: utcDate.Format(time.DateOnly), Results: snapshotResults}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daily JSON report: %w", err)
	}
	body = append(body, '\n')
	if err := os.MkdirAll(writer.directory, 0o750); err != nil {
		return fmt.Errorf("create daily report directory: %w", err)
	}
	temporary, err := os.CreateTemp(writer.directory, ".daily-report-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary daily report: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary daily report: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary daily report: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary daily report: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(writer.directory, utcDate.Format(time.DateOnly)+".json")); err != nil {
		return fmt.Errorf("replace daily report atomically: %w", err)
	}
	return nil
}

func dayUTC(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
