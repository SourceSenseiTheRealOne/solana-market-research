package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/positionevent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/reporting"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/testsupport"
)

func TestPaperFlowSurvivesRestartAndProducesBoundedDailyDashboardSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	fixture := newBoldMomentumPaperFlowFixture(now)
	client := openPaperFlowEntClient(t)
	defer func(client *ent.Client) { _ = client.Close() }(client)

	candidates, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	discovery := application.NewDiscovery(
		paperFlowPoolDiscovery{page: ports.PoolPage{Pools: []domain.DiscoveredPool{fixture.pool}}},
		candidates,
		application.DiscoveryOptions{Source: "task16", MaxPages: 1},
	)
	discovered, err := discovery.Run(ctx)
	if err != nil {
		t.Fatalf("Discovery.Run() error = %v", err)
	}
	if discovered.Inserted != 1 || discovered.Overlapped || discovered.CoverageGap {
		t.Fatalf("Discovery.Run() = %#v, want one complete synthetic candidate", discovered)
	}
	storedCandidate, err := client.Candidate.Query().Where(
		candidate.NetworkEQ(fixture.pool.Network),
		candidate.MintAddressEQ(fixture.pool.MintAddress),
		candidate.PoolAddressEQ(fixture.pool.PoolAddress),
	).Only(ctx)
	if err != nil {
		t.Fatalf("load discovered candidate: %v", err)
	}

	evaluator := application.NewEvaluator(application.EvaluatorOptions{
		Now:              now,
		Policy:           fixture.candidatePolicy,
		QuoteMint:        fixture.quoteMint,
		EntryInputAmount: fixture.entryInputAmount,
		Market:           paperFlowMarket{snapshot: fixture.market},
		Token:            paperFlowTokenInspector{},
		Quotes:           fixture.quotes,
		Snapshots:        candidates,
	})
	evaluation, err := evaluator.Evaluate(ctx, fixture.pool)
	if err != nil {
		t.Fatalf("Evaluator.Evaluate() error = %v", err)
	}
	if !evaluation.Eligible || len(evaluation.Rules) != 12 {
		t.Fatalf("Evaluator.Evaluate() = %#v, want a fully eligible deterministic evaluation", evaluation)
	}
	if !fixture.socialPolicy.Evaluate(fixture.socialMetrics, fixture.socialScore).Eligible {
		t.Fatal("Bold-v2 social boundary aggregates were rejected")
	}

	admissionRepository, err := postgres.NewAdmissionRepository(client, postgres.AdmissionRepositoryOptions{
		Now:                      func() time.Time { return now },
		MaxOpenPositions:         1,
		MaxDailyAdmissions:       1,
		MaxSerializationAttempts: 2,
		StrategyVersion:          fixture.strategyVersion,
	})
	if err != nil {
		t.Fatalf("NewAdmissionRepository() error = %v", err)
	}
	entryQuote, err := fixture.quotes.Quote(ctx, fixture.quoteMint, fixture.mintAddress, fixture.entryInputAmount)
	if err != nil {
		t.Fatalf("quote synthetic admission entry: %v", err)
	}
	admission := application.NewAdmission(application.AdmissionOptions{
		Now:                    now,
		VerdictPolicy:          fixture.verdictPolicy,
		MaxEvidenceAge:         5 * time.Minute,
		MaxEntryPriceImpactBPS: fixture.candidatePolicy.MaxEntryPriceImpactBPS,
		Repository:             admissionRepository,
	})
	admissionInput := application.AdmissionInput{
		Evaluation:         evaluation,
		Verdict:            fixture.verdict,
		EvidenceObservedAt: now,
		MintAddress:        fixture.mintAddress,
		QuoteMint:          fixture.quoteMint,
		EntryInputAmount:   fixture.entryInputAmount,
		EntryQuote:         entryQuote,
		IdempotencyKey:     fixture.idempotencyKey,
		CandidateID:        storedCandidate.ID,
		NotionalMicros:     fixture.notionalMicros,
	}
	admitted, err := admission.Admit(ctx, admissionInput)
	if err != nil {
		t.Fatalf("Admission.Admit() error = %v", err)
	}
	if !admitted.Admitted {
		t.Fatalf("Admission.Admit() = %#v, want persisted PENDING paper position", admitted)
	}
	duplicate, err := admission.Admit(ctx, admissionInput)
	if err != nil {
		t.Fatalf("duplicate Admission.Admit() error = %v", err)
	}
	if duplicate.Admitted {
		t.Fatalf("duplicate Admission.Admit() = %#v, want idempotent non-admission", duplicate)
	}

	positions, err := postgres.NewPositionRepository(client, func() time.Time { return now.Add(time.Minute) })
	if err != nil {
		t.Fatalf("NewPositionRepository() error = %v", err)
	}
	broker := application.NewPaperBroker(application.PaperBrokerOptions{
		Now:              func() time.Time { return now.Add(time.Minute) },
		Sleep:            func(context.Context, time.Duration) error { return nil },
		Quotes:           fixture.quotes,
		Positions:        positions,
		SimulatedLatency: time.Second,

		MaxEntryPriceImpactBPS: fixture.candidatePolicy.MaxEntryPriceImpactBPS,
		NetworkFeeMicros:       1_000,
		PriorityFeeMicros:      1_000,
	})
	opened, err := broker.Open(ctx, application.OpenPositionRequest{
		IdempotencyKey:   fixture.idempotencyKey,
		QuoteMint:        fixture.quoteMint,
		MintAddress:      fixture.mintAddress,
		EntryInputAmount: fixture.entryInputAmount,
	})
	if err != nil {
		t.Fatalf("PaperBroker.Open() error = %v", err)
	}
	if !opened.Opened || opened.Position.State != domain.PositionOpen || opened.Position.NotionalMicros != fixture.notionalMicros {
		t.Fatalf("PaperBroker.Open() = %#v, want an OPEN virtual paper position", opened)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("close pre-restart Ent client: %v", err)
	}
	client = openPaperFlowEntClient(t)
	defer func() { _ = client.Close() }()

	restartedPositions, err := postgres.NewPositionRepository(client, func() time.Time { return now.Add(2 * time.Minute) })
	if err != nil {
		t.Fatalf("NewPositionRepository() after restart error = %v", err)
	}
	manager := application.NewPositionManager(application.PositionManagerOptions{
		Now:               now.Add(2 * time.Minute),
		Quotes:            fixture.quotes,
		Positions:         restartedPositions,
		Policy:            fixture.exitPolicy,
		NetworkFeeMicros:  1_000,
		PriorityFeeMicros: 1_000,
	})
	if err := manager.RunOnce(ctx); err != nil {
		t.Fatalf("PositionManager.RunOnce() after restart error = %v", err)
	}
	if err := manager.RunOnce(ctx); err != nil {
		t.Fatalf("PositionManager.RunOnce() idempotent restart reload error = %v", err)
	}

	storedPosition, err := client.PaperPosition.Get(ctx, opened.Position.ID)
	if err != nil {
		t.Fatalf("load restarted paper position: %v", err)
	}
	if storedPosition.State != paperposition.StateCLOSED || storedPosition.ClosedAt == nil {
		t.Fatalf("restarted paper position = %#v, want one virtual CLOSED position", storedPosition)
	}
	closedEvents, err := client.PositionEvent.Query().Where(
		positionevent.HasPositionWith(paperposition.IDEQ(opened.Position.ID)),
		positionevent.EventTypeEQ(string(paperposition.StateCLOSED)),
	).Count(ctx)
	if err != nil {
		t.Fatalf("count CLOSED events: %v", err)
	}
	if closedEvents != 1 {
		t.Fatalf("CLOSED events = %d, want exactly one after restart/reload", closedEvents)
	}

	dailyResults, err := postgres.NewDailyResultRepository(client, func() time.Time { return now.Add(3 * time.Minute) })
	if err != nil {
		t.Fatalf("NewDailyResultRepository() error = %v", err)
	}
	reportDirectory := t.TempDir()
	writer, err := reporting.NewDailyJSONReportWriter(reportDirectory)
	if err != nil {
		t.Fatalf("NewDailyJSONReportWriter() error = %v", err)
	}
	reporter := application.NewDailyReporter(application.DailyReporterOptions{Repository: dailyResults, Writer: writer})
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	results, err := reporter.Generate(ctx, date)
	if err != nil {
		t.Fatalf("DailyReporter.Generate() error = %v", err)
	}
	if len(results) != 1 || results[0].UTCDate != date || results[0].StrategyVersion != fixture.strategyVersion || results[0].DailyAdmittedCount != 1 || results[0].RealizedPNLMicros != 50_000_000 {
		t.Fatalf("DailyReporter.Generate() = %#v, want one deterministic reconciled result", results)
	}
	if _, err := reporter.Generate(ctx, date); err != nil {
		t.Fatalf("DailyReporter.Generate() idempotent reload error = %v", err)
	}
	assertPaperFlowReport(t, filepath.Join(reportDirectory, date.Format(time.DateOnly)+".json"), fixture.strategyVersion)

	twitterAnalytics, err := postgres.NewTwitterAnalyticsRepository(client)
	if err != nil {
		t.Fatalf("NewTwitterAnalyticsRepository() error = %v", err)
	}
	dashboardActivity, err := postgres.NewDashboardActivityRepository(client)
	if err != nil {
		t.Fatalf("NewDashboardActivityRepository() error = %v", err)
	}
	dashboard := application.NewDashboardReadService(application.DashboardReadServiceOptions{
		StrategyVersion:    fixture.strategyVersion,
		Positions:          restartedPositions,
		DailyResults:       dailyResults,
		TwitterAnalytics:   twitterAnalytics,
		AutomationActivity: dashboardActivity,
		ReviewedCandidates: dashboardActivity,
	})
	snapshot, err := dashboard.Read(ctx, date)
	if err != nil {
		t.Fatalf("DashboardReadService.Read() error = %v", err)
	}
	if snapshot.StrategyVersion != fixture.strategyVersion || snapshot.UTCDate != date || len(snapshot.OpenPositions) != 0 || len(snapshot.DailyResults) != 1 || snapshot.DailyResults[0] != results[0] {
		t.Fatalf("DashboardReadService.Read() = %#v, want bounded daily result and no open virtual positions", snapshot)
	}
}

func openPaperFlowEntClient(t *testing.T) *ent.Client {
	t.Helper()
	databaseURL := testsupport.DatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := postgres.OpenEnt(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open isolated Task 16 Ent client: %v", err)
	}
	return client
}

func assertPaperFlowReport(t *testing.T, path, strategyVersion string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read retained daily report: %v", err)
	}
	var report struct {
		UTCDate string `json:"utc_date"`
		Results []struct {
			StrategyVersion    string `json:"strategy_version"`
			DailyAdmittedCount int    `json:"daily_admitted_count"`
			RealizedPNLMicros  int64  `json:"realized_pnl_micros"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("decode retained daily report: %v", err)
	}
	if report.UTCDate != "2026-08-20" || len(report.Results) != 1 || report.Results[0].StrategyVersion != strategyVersion || report.Results[0].DailyAdmittedCount != 1 || report.Results[0].RealizedPNLMicros != 50_000_000 {
		t.Fatalf("retained daily report = %#v, want one bounded deterministic result", report)
	}
}

type paperFlowPoolDiscovery struct{ page ports.PoolPage }

func (discovery paperFlowPoolDiscovery) FetchNewPools(context.Context, int) (ports.PoolPage, error) {
	return discovery.page, nil
}

type paperFlowMarket struct{ snapshot domain.MarketSnapshot }

func (market paperFlowMarket) Fetch(context.Context, domain.DiscoveredPool) (domain.MarketSnapshot, error) {
	return market.snapshot, nil
}

type paperFlowTokenInspector struct{}

func (paperFlowTokenInspector) Inspect(context.Context, string) (domain.TokenRiskSnapshot, error) {
	return domain.TokenRiskSnapshot{Program: domain.TokenProgramLegacy, MintAuthorityRevoked: true, FreezeAuthorityRevoked: true}, nil
}

type paperFlowQuotes struct {
	entry domain.ExecutableQuote
	exit  domain.ExecutableQuote
}

func (quotes paperFlowQuotes) Quote(_ context.Context, inputMint, outputMint string, amount uint64) (domain.ExecutableQuote, error) {
	if inputMint == quotes.entry.InputMint && outputMint == quotes.entry.OutputMint && amount == quotes.entry.InAmount {
		return quotes.entry, nil
	}
	if inputMint == quotes.exit.InputMint && outputMint == quotes.exit.OutputMint && amount == quotes.exit.InAmount {
		return quotes.exit, nil
	}
	return domain.ExecutableQuote{}, fmt.Errorf("unexpected synthetic quote request: %s -> %s amount=%d", inputMint, outputMint, amount)
}

func (paperFlowQuotes) Health(context.Context) error { return nil }

type boldMomentumPaperFlowFixture struct {
	quoteMint        string
	mintAddress      string
	poolAddress      string
	strategyVersion  string
	idempotencyKey   string
	entryInputAmount uint64
	notionalMicros   int64
	pool             domain.DiscoveredPool
	quotes           paperFlowQuotes
	market           domain.MarketSnapshot
	candidatePolicy  domain.CandidatePolicy
	socialMetrics    domain.SocialMetrics
	socialScore      int
	socialPolicy     domain.SocialPolicy
	verdict          domain.Verdict
	verdictPolicy    domain.VerdictPolicy
	exitPolicy       domain.ExitPolicy
}

func newBoldMomentumPaperFlowFixture(now time.Time) boldMomentumPaperFlowFixture {
	const (
		quoteMint        = "task16-quote-mint"
		mintAddress      = "task16-candidate-mint"
		poolAddress      = "task16-pool"
		strategyVersion  = "bold-momentum-v2"
		entryInputAmount = uint64(100_000_000)
		notionalMicros   = int64(100_000_000)
	)
	pool := domain.DiscoveredPool{
		Source:      domain.SourceGeckoTerminal,
		Network:     domain.NetworkSolana,
		MintAddress: mintAddress,
		PoolAddress: poolAddress,
		CreatedAt:   now.Add(-5 * time.Minute),
	}
	return boldMomentumPaperFlowFixture{
		quoteMint:        quoteMint,
		mintAddress:      mintAddress,
		poolAddress:      poolAddress,
		strategyVersion:  strategyVersion,
		idempotencyKey:   strategyVersion + ":" + pool.Identity() + ":" + pool.CreatedAt.UTC().Format(time.RFC3339Nano),
		entryInputAmount: entryInputAmount,
		notionalMicros:   notionalMicros,
		pool:             pool,
		quotes: paperFlowQuotes{
			entry: domain.ExecutableQuote{
				ObservedAt: now, InputMint: quoteMint, OutputMint: mintAddress,
				InAmount: entryInputAmount, OutAmount: 25_000_000, PriceImpactBPS: 1_000,
				RoutePlan: []domain.RouteLeg{{AMMKey: poolAddress, Label: "synthetic"}},
			},
			exit: domain.ExecutableQuote{
				ObservedAt: now.Add(2 * time.Minute), InputMint: mintAddress, OutputMint: quoteMint,
				InAmount: 25_000_000, OutAmount: 150_005_000, PriceImpactBPS: 1_000,
				RoutePlan: []domain.RouteLeg{{AMMKey: poolAddress, Label: "synthetic"}},
			},
		},
		market: domain.MarketSnapshot{
			ObservedAt: now.Add(-time.Minute), LiquidityUSD: domain.USD{Micros: 5_000_000_000},
			FiveMinuteTransactions: 20, FiveMinuteBuys: 13, FiveMinuteSells: 7,
			FiveMinuteVolumeUSD: domain.USD{Micros: 750_000_000}, FiveMinutePriceChangeBPS: 200,
		},
		candidatePolicy: domain.CandidatePolicy{
			MaxPoolAge: 90 * time.Minute, MinLiquidityUSD: domain.USD{Micros: 5_000_000_000},
			MinFiveMinuteTransactions: 20, MinFiveMinuteBuyShareBPS: 6_500, MinFiveMinuteTurnoverBPS: 1_500,
			MinFiveMinutePriceChangeBPS: 200, MaxFiveMinutePriceChangeBPS: 6_000, MaxEntryPriceImpactBPS: 1_000,
		},
		socialMetrics: domain.SocialMetrics{Posts: 3, UniqueAuthors: 3, OriginalPosts: 2, Reposts: 1, ExactMintMentions: 2, WarningPosts: 1},
		socialScore:   30,
		socialPolicy: domain.SocialPolicy{
			MinScore: 30, MinUniqueAuthors: 3, MinOriginalPosts: 2, MinExactMintMentions: 2, MaxWarningPosts: 1,
		},
		verdict:       domain.Verdict{Outcome: domain.VerdictBuy, Confidence: 70, HypeQualityScore: 60, ManipulationProbability: 35},
		verdictPolicy: domain.VerdictPolicy{MinimumConfidence: 70, MinimumHypeQuality: 60, MaximumManipulationProbability: 35},
		exitPolicy:    domain.ExitPolicy{TakeProfitBPS: 5_000, StopLossBPS: 2_000, MaxHoldDuration: 45 * time.Minute},
	}
}

func (fixture boldMomentumPaperFlowFixture) candidateEvidence() domain.CandidateEvidence {
	return domain.CandidateEvidence{
		MintAddress: fixture.mintAddress, QuoteMint: fixture.quoteMint, PoolCreatedAt: fixture.pool.CreatedAt,
		MarketObservedAt: fixture.market.ObservedAt, ReceivedAt: fixture.quotes.entry.ObservedAt,
		LiquidityUSD: fixture.market.LiquidityUSD, FiveMinuteTransactions: fixture.market.FiveMinuteTransactions,
		FiveMinuteBuys: fixture.market.FiveMinuteBuys, FiveMinuteSells: fixture.market.FiveMinuteSells,
		FiveMinuteVolumeUSD: fixture.market.FiveMinuteVolumeUSD, FiveMinutePriceChangeBPS: fixture.market.FiveMinutePriceChangeBPS,
		EntryInputAmount: fixture.entryInputAmount,
		Token:            domain.TokenSafety{Program: domain.TokenProgramLegacy, MintAuthorityRevoked: true, FreezeAuthorityRevoked: true},
		EntryQuote:       fixture.quotes.entry, ExitQuote: fixture.quotes.exit,
	}
}
