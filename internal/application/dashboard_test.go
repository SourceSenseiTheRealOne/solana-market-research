package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestDashboardReadServiceReturnsSafeProjectionsForUTCDate(t *testing.T) {
	date := time.Date(2026, time.August, 20, 18, 30, 0, 0, time.FixedZone("UTC+4", 4*60*60))
	positions := &dashboardPositionsFake{positions: []domain.PaperPosition{{ID: 7, State: domain.PositionOpen}}}
	dailyResults := &dashboardDailyResultsFake{results: []domain.DailyResult{{UTCDate: date.UTC(), StrategyVersion: "strategy-a"}}}
	twitterAnalytics := &dashboardTwitterAnalyticsFake{analytics: []domain.TwitterAnalytics{{MintAddress: "public-mint", SearchedAt: date.UTC(), SearchCount: 1, Score: 72, Posts: 8, UniqueAuthors: 6, ExactMintMentions: 5, WarningPosts: 1}}}
	activity := &dashboardActivityFake{activities: []domain.AutomationActivity{{OccurredAt: date.UTC(), Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeCompleted}}}
	reviewed := &dashboardReviewedCandidatesFake{candidates: []domain.ReviewedCandidate{{MintAddress: "reviewed-mint", CheckedAt: date.UTC(), Outcome: domain.ReviewedCandidateNotTraded}}}
	service := application.NewDashboardReadService(application.DashboardReadServiceOptions{StrategyVersion: "bold-momentum-v2", Positions: positions, DailyResults: dailyResults, TwitterAnalytics: twitterAnalytics, AutomationActivity: activity, ReviewedCandidates: reviewed})

	snapshot, err := service.Read(context.Background(), date)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	wantDate := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	if positions.calls != 1 || dailyResults.date != wantDate || twitterAnalytics.calls != 1 || activity.calls != 1 || reviewed.calls != 1 || snapshot.UTCDate != wantDate || len(snapshot.OpenPositions) != 1 || snapshot.OpenPositions[0].ID != 7 || len(snapshot.DailyResults) != 1 || snapshot.DailyResults[0].StrategyVersion != "strategy-a" || len(snapshot.TwitterAnalytics) != 1 || snapshot.TwitterAnalytics[0].Score != 72 || len(snapshot.AutomationActivity) != 1 || snapshot.AutomationActivity[0].Outcome != domain.AutomationOutcomeCompleted || len(snapshot.ReviewedCandidates) != 1 || snapshot.ReviewedCandidates[0].MintAddress != "reviewed-mint" {
		t.Fatalf("Read() snapshot=%#v positions=%#v daily=%#v Twitter=%#v activity=%#v reviewed=%#v, want UTC-normalized bounded dashboard data", snapshot, positions, dailyResults, twitterAnalytics, activity, reviewed)
	}
}

type dashboardPositionsFake struct {
	positions []domain.PaperPosition
	calls     int
}

func (fake *dashboardPositionsFake) ListOpen(context.Context) ([]domain.PaperPosition, error) {
	fake.calls++
	return fake.positions, nil
}

type dashboardDailyResultsFake struct {
	results []domain.DailyResult
	date    time.Time
}

func (fake *dashboardDailyResultsFake) List(_ context.Context, date time.Time) ([]domain.DailyResult, error) {
	fake.date = date
	return fake.results, nil
}

type dashboardTwitterAnalyticsFake struct {
	analytics []domain.TwitterAnalytics
	calls     int
}

func (fake *dashboardTwitterAnalyticsFake) ListTwitterAnalytics(context.Context) ([]domain.TwitterAnalytics, error) {
	fake.calls++
	return fake.analytics, nil
}

type dashboardActivityFake struct {
	activities []domain.AutomationActivity
	calls      int
}

func (fake *dashboardActivityFake) List(context.Context) ([]domain.AutomationActivity, error) {
	fake.calls++
	return fake.activities, nil
}

type dashboardReviewedCandidatesFake struct {
	candidates []domain.ReviewedCandidate
	calls      int
}

func (fake *dashboardReviewedCandidatesFake) ListReviewed(context.Context) ([]domain.ReviewedCandidate, error) {
	fake.calls++
	return fake.candidates, nil
}
