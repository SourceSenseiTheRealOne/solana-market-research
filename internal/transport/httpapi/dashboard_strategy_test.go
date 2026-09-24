package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/transport/httpapi"
)

func TestHandlerServesActiveStrategyWithEmptyTradeData(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	reader := &dashboardReaderFake{snapshot: application.DashboardSnapshot{StrategyVersion: "bold-momentum-v2", UTCDate: date}}
	handler := httpapi.NewHandler(httpapi.Options{Reader: reader, Now: func() time.Time { return date }})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard?date=2026-08-20", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	want := `{"strategy_version":"bold-momentum-v2","utc_date":"2026-08-20","daily_results":[],"open_positions":[],"twitter_analytics":[],"automation_activity":[],"reviewed_candidates":[]}`
	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != want {
		t.Fatalf("status=%d body=%s, want exact empty safe projection", response.Code, response.Body.String())
	}
}
