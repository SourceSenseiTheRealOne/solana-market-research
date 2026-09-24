package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
)

type Options struct {
	Reader application.DashboardReader
	Now    func() time.Time
}

type Handler struct {
	reader application.DashboardReader
	now    func() time.Time
}

func NewHandler(options Options) *Handler {
	return &Handler{reader: options.Reader, now: options.Now}
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setSafeHeaders(writer)
	switch request.URL.Path {
	case "/healthz":
		handler.serveHealth(writer, request)
	case "/api/v1/dashboard":
		handler.serveDashboard(writer, request)
	default:
		writeJSON(writer, http.StatusNotFound, errorResponse{Error: "not found"})
	}
}

func (handler *Handler) serveHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	writeJSON(writer, http.StatusOK, healthResponse{Status: "ok"})
}

func (handler *Handler) serveDashboard(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeJSON(writer, http.StatusMethodNotAllowed, errorResponse{Error: "method not allowed"})
		return
	}
	if request.Body != nil && request.Body != http.NoBody && request.ContentLength != 0 {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "request body is not allowed"})
		return
	}
	if handler == nil || handler.reader == nil || handler.now == nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "service unavailable"})
		return
	}
	date, ok := requestDate(request, handler.now())
	if !ok {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Error: "date must be YYYY-MM-DD"})
		return
	}
	snapshot, err := handler.reader.Read(request.Context(), date)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "service unavailable"})
		return
	}
	if snapshot.UTCDate.IsZero() || dayUTC(snapshot.UTCDate) != date {
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Error: "service unavailable"})
		return
	}
	writeJSON(writer, http.StatusOK, dashboardResponseFrom(snapshot))
}

type healthResponse struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type dashboardResponse struct {
	StrategyVersion    string                       `json:"strategy_version"`
	UTCDate            string                       `json:"utc_date"`
	DailyResults       []dailyResultResponse        `json:"daily_results"`
	OpenPositions      []openPositionResponse       `json:"open_positions"`
	TwitterAnalytics   []twitterAnalyticsResponse   `json:"twitter_analytics"`
	AutomationActivity []automationActivityResponse `json:"automation_activity"`
	ReviewedCandidates []reviewedCandidateResponse  `json:"reviewed_candidates"`
}

type dailyResultResponse struct {
	StrategyVersion    string `json:"strategy_version"`
	DailyAdmittedCount int    `json:"daily_admitted_count"`
	RealizedPNLMicros  int64  `json:"realized_pnl_micros"`
}

type openPositionResponse struct {
	ID             int    `json:"id"`
	CandidateID    int    `json:"candidate_id"`
	State          string `json:"state"`
	NotionalMicros int64  `json:"notional_micros"`
	MintAddress    string `json:"mint_address"`
	NoRouteCount   int    `json:"no_route_count"`
	OpenedAt       string `json:"opened_at"`
}

type twitterAnalyticsResponse struct {
	MintAddress       string `json:"mint_address"`
	SearchedAt        string `json:"searched_at"`
	SearchCount       int    `json:"search_count"`
	Score             int    `json:"score"`
	Posts             int    `json:"posts"`
	UniqueAuthors     int    `json:"unique_authors"`
	ExactMintMentions int    `json:"exact_mint_mentions"`
	WarningPosts      int    `json:"warning_posts"`
}

type automationActivityResponse struct {
	OccurredAt string `json:"occurred_at"`
	Job        string `json:"job"`
	Outcome    string `json:"outcome"`
	Category   string `json:"category"`
	Stage      string `json:"stage"`
}

type reviewedCandidateResponse struct {
	MintAddress string `json:"mint_address"`
	CheckedAt   string `json:"checked_at"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason"`
}

func dashboardResponseFrom(snapshot application.DashboardSnapshot) dashboardResponse {
	response := dashboardResponse{
		StrategyVersion:    snapshot.StrategyVersion,
		UTCDate:            snapshot.UTCDate.UTC().Format(time.DateOnly),
		DailyResults:       make([]dailyResultResponse, 0, len(snapshot.DailyResults)),
		OpenPositions:      make([]openPositionResponse, 0, len(snapshot.OpenPositions)),
		TwitterAnalytics:   make([]twitterAnalyticsResponse, 0, len(snapshot.TwitterAnalytics)),
		AutomationActivity: make([]automationActivityResponse, 0, len(snapshot.AutomationActivity)),
		ReviewedCandidates: make([]reviewedCandidateResponse, 0, len(snapshot.ReviewedCandidates)),
	}
	for _, result := range snapshot.DailyResults {
		response.DailyResults = append(response.DailyResults, dailyResultResponse{StrategyVersion: result.StrategyVersion, DailyAdmittedCount: result.DailyAdmittedCount, RealizedPNLMicros: result.RealizedPNLMicros})
	}
	for _, position := range snapshot.OpenPositions {
		openedAt := ""
		if !position.OpenedAt.IsZero() {
			openedAt = position.OpenedAt.UTC().Format(time.RFC3339)
		}
		response.OpenPositions = append(response.OpenPositions, openPositionResponse{ID: position.ID, CandidateID: position.CandidateID, State: string(position.State), NotionalMicros: position.NotionalMicros, MintAddress: position.MintAddress, NoRouteCount: position.NoRouteCount, OpenedAt: openedAt})
	}
	for _, analytics := range snapshot.TwitterAnalytics {
		response.TwitterAnalytics = append(response.TwitterAnalytics, twitterAnalyticsResponse{
			MintAddress:       analytics.MintAddress,
			SearchedAt:        analytics.SearchedAt.UTC().Format(time.RFC3339),
			SearchCount:       analytics.SearchCount,
			Score:             analytics.Score,
			Posts:             analytics.Posts,
			UniqueAuthors:     analytics.UniqueAuthors,
			ExactMintMentions: analytics.ExactMintMentions,
			WarningPosts:      analytics.WarningPosts,
		})
	}
	for _, activity := range snapshot.AutomationActivity {
		response.AutomationActivity = append(response.AutomationActivity, automationActivityResponse{
			OccurredAt: activity.OccurredAt.UTC().Format(time.RFC3339),
			Job:        string(activity.Job),
			Outcome:    string(activity.Outcome),
			Category:   activity.Category,
			Stage:      activity.Stage,
		})
	}
	for _, candidate := range snapshot.ReviewedCandidates {
		response.ReviewedCandidates = append(response.ReviewedCandidates, reviewedCandidateResponse{
			MintAddress: candidate.MintAddress,
			CheckedAt:   candidate.CheckedAt.UTC().Format(time.RFC3339),
			Outcome:     string(candidate.Outcome),
			Reason:      string(candidate.Reason),
		})
	}
	return response
}

func requestDate(request *http.Request, now time.Time) (time.Time, bool) {
	query := request.URL.Query()
	if len(query) == 0 {
		return dayUTC(now), !now.IsZero()
	}
	values, ok := query["date"]
	if !ok || len(query) != 1 || len(values) != 1 {
		return time.Time{}, false
	}
	date, err := time.Parse(time.DateOnly, values[0])
	if err != nil || date.Format(time.DateOnly) != values[0] {
		return time.Time{}, false
	}
	return dayUTC(date), true
}

func dayUTC(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func setSafeHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
