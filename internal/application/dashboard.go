package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type OpenPositionsReader interface {
	ListOpen(context.Context) ([]domain.PaperPosition, error)
}

type DashboardDailyResultsReader interface {
	List(context.Context, time.Time) ([]domain.DailyResult, error)
}

type TwitterAnalyticsReader interface {
	ListTwitterAnalytics(context.Context) ([]domain.TwitterAnalytics, error)
}

type DashboardSnapshot struct {
	StrategyVersion    string
	UTCDate            time.Time
	DailyResults       []domain.DailyResult
	OpenPositions      []domain.PaperPosition
	TwitterAnalytics   []domain.TwitterAnalytics
	AutomationActivity []domain.AutomationActivity
	ReviewedCandidates []domain.ReviewedCandidate
}

type DashboardReader interface {
	Read(context.Context, time.Time) (DashboardSnapshot, error)
}

type DashboardReadServiceOptions struct {
	StrategyVersion    string
	Positions          OpenPositionsReader
	DailyResults       DashboardDailyResultsReader
	TwitterAnalytics   TwitterAnalyticsReader
	AutomationActivity AutomationActivityReader
	ReviewedCandidates ReviewedCandidatesReader
}

type DashboardReadService struct{ options DashboardReadServiceOptions }

func NewDashboardReadService(options DashboardReadServiceOptions) *DashboardReadService {
	return &DashboardReadService{options: options}
}

func (service *DashboardReadService) Read(ctx context.Context, date time.Time) (DashboardSnapshot, error) {
	if service == nil || strings.TrimSpace(service.options.StrategyVersion) == "" || service.options.Positions == nil || service.options.DailyResults == nil || service.options.TwitterAnalytics == nil || service.options.AutomationActivity == nil || service.options.ReviewedCandidates == nil || date.IsZero() {
		return DashboardSnapshot{}, errors.New("dashboard reader is not completely configured")
	}
	utcDate := time.Date(date.UTC().Year(), date.UTC().Month(), date.UTC().Day(), 0, 0, 0, 0, time.UTC)
	openPositions, err := service.options.Positions.ListOpen(ctx)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load open paper positions: %w", err)
	}
	dailyResults, err := service.options.DailyResults.List(ctx, utcDate)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load daily results: %w", err)
	}
	twitterAnalytics, err := service.options.TwitterAnalytics.ListTwitterAnalytics(ctx)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load Twitter analytics: %w", err)
	}
	activity, err := service.options.AutomationActivity.List(ctx)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load automation activity: %w", err)
	}
	reviewed, err := service.options.ReviewedCandidates.ListReviewed(ctx)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load reviewed candidates: %w", err)
	}
	return DashboardSnapshot{StrategyVersion: strings.TrimSpace(service.options.StrategyVersion), UTCDate: utcDate, DailyResults: dailyResults, OpenPositions: openPositions, TwitterAnalytics: twitterAnalytics, AutomationActivity: activity, ReviewedCandidates: reviewed}, nil
}
