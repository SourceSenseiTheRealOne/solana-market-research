package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/transport/httpapi"
)

func TestHandlerServesAllowlistedDashboardSnapshot(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	reader := &dashboardReaderFake{snapshot: application.DashboardSnapshot{
		StrategyVersion:    "bold-momentum-v2",
		UTCDate:            date,
		DailyResults:       []domain.DailyResult{{UTCDate: date, StrategyVersion: "strategy-a", DailyAdmittedCount: 2, RealizedPNLMicros: 1_500_000}},
		OpenPositions:      []domain.PaperPosition{{ID: 7, CandidateID: 9, State: domain.PositionOpen, NotionalMicros: 10_000_000, MintAddress: "public-token-mint", QuoteMint: "must-not-leak", EntryInputAmount: 10_000_000, OpenedAt: date.Add(time.Hour), NoRouteCount: 1}},
		TwitterAnalytics:   []domain.TwitterAnalytics{{MintAddress: "twitter-public-mint", SearchedAt: date.Add(time.Hour), SearchCount: 1, Score: 72, Posts: 8, UniqueAuthors: 6, ExactMintMentions: 5, WarningPosts: 1}},
		AutomationActivity: []domain.AutomationActivity{{OccurredAt: date.Add(2 * time.Hour), Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeCompleted}},
		ReviewedCandidates: []domain.ReviewedCandidate{{MintAddress: "reviewed-public-mint", CheckedAt: date.Add(3 * time.Hour), Outcome: domain.ReviewedCandidateNotTraded, Reason: domain.ReviewReasonDeterministicLiquidity}},
	}}
	handler := httpapi.NewHandler(httpapi.Options{Reader: reader, Now: func() time.Time { return date }})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard?date=2026-08-20", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" || reader.date != date {
		t.Fatalf("dashboard response status=%d headers=%v requested=%s, want 200 safe JSON/no-store UTC date", response.Code, response.Header(), reader.date)
	}
	raw := response.Body.String()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatalf("decode dashboard response keys: %v", err)
	}
	if len(keys) != 7 || keys["strategy_version"] == nil || keys["utc_date"] == nil || keys["daily_results"] == nil || keys["open_positions"] == nil || keys["twitter_analytics"] == nil || keys["automation_activity"] == nil || keys["reviewed_candidates"] == nil {
		t.Fatalf("dashboard response keys = %#v, want exact public allowlist", keys)
	}
	var body struct {
		StrategyVersion string `json:"strategy_version"`
		UTCDate         string `json:"utc_date"`
		DailyResults    []struct {
			StrategyVersion    string `json:"strategy_version"`
			DailyAdmittedCount int    `json:"daily_admitted_count"`
			RealizedPNLMicros  int64  `json:"realized_pnl_micros"`
		} `json:"daily_results"`
		OpenPositions []struct {
			ID             int    `json:"id"`
			CandidateID    int    `json:"candidate_id"`
			State          string `json:"state"`
			NotionalMicros int64  `json:"notional_micros"`
			MintAddress    string `json:"mint_address"`
			NoRouteCount   int    `json:"no_route_count"`
			OpenedAt       string `json:"opened_at"`
		} `json:"open_positions"`
		TwitterAnalytics []struct {
			MintAddress       string `json:"mint_address"`
			SearchedAt        string `json:"searched_at"`
			SearchCount       int    `json:"search_count"`
			Score             int    `json:"score"`
			Posts             int    `json:"posts"`
			UniqueAuthors     int    `json:"unique_authors"`
			ExactMintMentions int    `json:"exact_mint_mentions"`
			WarningPosts      int    `json:"warning_posts"`
		} `json:"twitter_analytics"`
		AutomationActivity []struct {
			OccurredAt string `json:"occurred_at"`
			Job        string `json:"job"`
			Outcome    string `json:"outcome"`
			Category   string `json:"category"`
			Stage      string `json:"stage"`
		} `json:"automation_activity"`
		ReviewedCandidates []struct {
			MintAddress string `json:"mint_address"`
			CheckedAt   string `json:"checked_at"`
			Outcome     string `json:"outcome"`
			Reason      string `json:"reason"`
		} `json:"reviewed_candidates"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode dashboard response: %v", err)
	}
	if body.StrategyVersion != "bold-momentum-v2" || body.UTCDate != "2026-08-20" || len(body.DailyResults) != 1 || body.DailyResults[0].StrategyVersion != "strategy-a" || body.DailyResults[0].DailyAdmittedCount != 2 || body.DailyResults[0].RealizedPNLMicros != 1_500_000 || len(body.OpenPositions) != 1 || body.OpenPositions[0].ID != 7 || body.OpenPositions[0].CandidateID != 9 || body.OpenPositions[0].State != "OPEN" || body.OpenPositions[0].NotionalMicros != 10_000_000 || body.OpenPositions[0].MintAddress != "public-token-mint" || body.OpenPositions[0].NoRouteCount != 1 || body.OpenPositions[0].OpenedAt != "2026-08-20T01:00:00Z" || len(body.TwitterAnalytics) != 1 || body.TwitterAnalytics[0].MintAddress != "twitter-public-mint" || body.TwitterAnalytics[0].SearchedAt != "2026-08-20T01:00:00Z" || body.TwitterAnalytics[0].SearchCount != 1 || body.TwitterAnalytics[0].Score != 72 || body.TwitterAnalytics[0].Posts != 8 || body.TwitterAnalytics[0].UniqueAuthors != 6 || body.TwitterAnalytics[0].ExactMintMentions != 5 || body.TwitterAnalytics[0].WarningPosts != 1 || len(body.AutomationActivity) != 1 || body.AutomationActivity[0].OccurredAt != "2026-08-20T02:00:00Z" || body.AutomationActivity[0].Job != "SCAN" || body.AutomationActivity[0].Outcome != "COMPLETED" || body.AutomationActivity[0].Category != "" || body.AutomationActivity[0].Stage != "" || len(body.ReviewedCandidates) != 1 || body.ReviewedCandidates[0].MintAddress != "reviewed-public-mint" || body.ReviewedCandidates[0].CheckedAt != "2026-08-20T03:00:00Z" || body.ReviewedCandidates[0].Outcome != "NOT_TRADED" || body.ReviewedCandidates[0].Reason != "deterministic_liquidity" {
		t.Fatalf("dashboard body = %#v, want allowlisted dashboard snapshot", body)
	}
	if strings.Contains(raw, "quote_mint") || strings.Contains(raw, "entry_input_amount") || strings.Contains(raw, "must-not-leak") || strings.Contains(raw, "social_evidence") || strings.Contains(raw, "tweet_text") || strings.Contains(raw, "author_id") {
		t.Fatalf("dashboard response leaked excluded data: %s", raw)
	}
}

func TestHandlerRejectsDashboardRequestBodyBeforeReading(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	reader := &dashboardReaderFake{}
	handler := httpapi.NewHandler(httpapi.Options{Reader: reader, Now: func() time.Time { return date }})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", strings.NewReader(`{"unexpected":"input"}`))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || !reader.date.IsZero() {
		t.Fatalf("dashboard request status=%d read-date=%s, want 400 without reader invocation", response.Code, reader.date)
	}
}

func TestHandlerRejectsMismatchedDashboardSnapshotDate(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	reader := &dashboardReaderFake{snapshot: application.DashboardSnapshot{UTCDate: date.AddDate(0, 0, 1)}}
	handler := httpapi.NewHandler(httpapi.Options{Reader: reader, Now: func() time.Time { return date }})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard?date=2026-08-20", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "2026-08-21") {
		t.Fatalf("mismatched snapshot status=%d body=%s, want generic 500 without cross-day data", response.Code, response.Body.String())
	}
}

type dashboardReaderFake struct {
	snapshot application.DashboardSnapshot
	date     time.Time
}

func (fake *dashboardReaderFake) Read(_ context.Context, date time.Time) (application.DashboardSnapshot, error) {
	fake.date = date
	return fake.snapshot, nil
}
