package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/reporting"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/config"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	automationUserAgent       = "solana-hype-paper-bot/production-paper"
	dexScreenerBaseURL        = "https://api.dexscreener.com"
	geckoTerminalBaseURL      = "https://api.geckoterminal.com"
	twitterAPIIOBaseURL       = "https://api.twitterapi.io"
	providerTimeout           = 10 * time.Second
	hermesTimeout             = 30 * time.Second
	discoverySource           = "dexscreener-geckoterminal"
	productionScanTimeout     = 5 * time.Minute
	productionMonitorTimeout  = 30 * time.Second
	productionRetryTimeout    = 45 * time.Second
	automationPersistTimeout  = 5 * time.Second
	providerMaxBodyBytes      = 1 << 20
	providerSmallMaxBodyBytes = 512 << 10
)

type automationHTTPClientSpecs struct {
	DexScreener   httpclient.Options
	GeckoTerminal httpclient.Options
	SolanaRPC     httpclient.Options
	Jupiter       httpclient.Options
	TwitterAPIIO  httpclient.Options
	Hermes        httpclient.Options
}

type automationDependencies struct {
	cfg        config.Config
	now        func() time.Time
	candidates *postgres.CandidateRepository
	social     *postgres.SocialEvidenceRepository
	verdicts   *postgres.VerdictRepository
	admissions *postgres.AdmissionRepository
	positions  *postgres.PositionRepository
	reviewed   *postgres.DashboardActivityRepository
	reporter   *application.DailyReporter
	dex        *httpclient.DexScreener
	gecko      *httpclient.GeckoTerminal
	market     application.MarketProvider
	token      *httpclient.SolanaRPC
	quotes     *httpclient.Jupiter
	posts      *httpclient.TwitterAPIIO
	hermes     *httpclient.Hermes
}

type automationRunner interface {
	Run(context.Context) error
}

func startAutomation(ctx context.Context, cfg config.Config, client *ent.Client, now func() time.Time) (func(), error) {
	if !cfg.PaperAutomationEnabled {
		return func() {}, nil
	}
	dependencies, err := newAutomationDependencies(cfg, client, now)
	if err != nil {
		return nil, err
	}
	cadence := application.NewSplitCadence(productionCadenceOptions(
		dynamicProductionScanJob{dependencies: dependencies},
		dynamicPositionMonitorJob{dependencies: dependencies},
		dynamicMarketRetryJob{dependencies: dependencies},
	))
	automationContext, cancel := context.WithCancel(ctx)
	errors := launchAutomation(automationContext, true, cadence)
	return func() {
		cancel()
		if err := <-errors; err != nil {
			slog.Warn("paper automation stopped")
		}
	}, nil
}

func launchAutomation(ctx context.Context, enabled bool, runner automationRunner) <-chan error {
	if !enabled {
		return nil
	}
	errors := make(chan error, 1)
	go func() {
		errors <- runner.Run(ctx)
	}()
	return errors
}

func automationErrorCategory(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context_deadline_exceeded"
	case errors.Is(err, httpclient.ErrRateLimited):
		return "provider_rate_limited"
	case errors.Is(err, httpclient.ErrResponseTooLarge):
		return "provider_response_too_large"
	case errors.Is(err, httpclient.ErrUpstream):
		return "provider_upstream_error"
	default:
		return "internal_error"
	}
}

func automationFailureStage(err error) string {
	message := err.Error()
	switch {
	case strings.Contains(message, "discover fresh two-source pools:"):
		return "discovery"
	case strings.Contains(message, "evaluate deterministic candidate ") && strings.Contains(message, "fetch market evidence:"):
		return "market_evidence"
	case strings.Contains(message, "evaluate deterministic candidate ") && strings.Contains(message, "inspect token:"):
		return "mint_inspection"
	case strings.Contains(message, "evaluate deterministic candidate ") && strings.Contains(message, "quote entry:"):
		return "entry_quote"
	case strings.Contains(message, "evaluate deterministic candidate ") && strings.Contains(message, "quote exit:"):
		return "exit_quote"
	case strings.Contains(message, "evaluate deterministic candidate ") && strings.Contains(message, "save candidate snapshot:"):
		return "candidate_snapshot"
	case strings.Contains(message, "evaluate deterministic candidate "):
		return "evaluation"
	case strings.Contains(message, "analyze eligible social evidence "):
		return "social_evidence"
	case strings.Contains(message, "request eligible Hermes verdict "),
		strings.Contains(message, "validate eligible Hermes verdict "),
		strings.Contains(message, "store eligible Hermes verdict "):
		return "hermes_verdict"
	case strings.Contains(message, "quote eligible paper admission "):
		return "paper_entry_quote"
	case strings.Contains(message, "load eligible candidate identity "):
		return "candidate_identity"
	case strings.Contains(message, "admit eligible paper candidate "):
		return "paper_admission"
	case strings.Contains(message, "open admitted paper position "):
		return "paper_open"
	case strings.Contains(message, "generate daily paper report:"):
		return dailyReportFailureStage(message)
	default:
		return "internal"
	}
}

func dailyReportFailureStage(message string) string {
	switch {
	case strings.Contains(message, "reconcile daily result:"):
		return "daily_reconcile"
	case strings.Contains(message, "load reconciled daily results:"):
		return "daily_results_load"
	case strings.Contains(message, "write retained daily report:"):
		return "daily_report_write"
	default:
		return "daily_report"
	}
}

func productionCadenceOptions(scan, monitor, retry application.ScheduledJob) application.SplitCadenceOptions {
	return application.SplitCadenceOptions{
		Scan:            scan,
		Monitor:         monitor,
		Retry:           retry,
		ScanInterval:    productionScanTimeout,
		MonitorInterval: productionMonitorTimeout,
		ScanTimeout:     productionScanTimeout,
		MonitorTimeout:  productionMonitorTimeout,
		RetryTimeout:    productionRetryTimeout,
		OnError: func(err error) {
			slog.Warn("paper automation job failed", "category", automationErrorCategory(err), "stage", automationFailureStage(err))
		},
	}
}

func newAutomationDependencies(cfg config.Config, client *ent.Client, now func() time.Time) (automationDependencies, error) {
	specs, err := newAutomationHTTPClientSpecs(cfg)
	if err != nil {
		return automationDependencies{}, err
	}
	candidates, err := postgres.NewCandidateRepository(client)
	if err != nil {
		return automationDependencies{}, err
	}
	social, err := postgres.NewSocialEvidenceRepository(client)
	if err != nil {
		return automationDependencies{}, err
	}
	verdicts, err := postgres.NewVerdictRepository(client)
	if err != nil {
		return automationDependencies{}, err
	}
	admissions, err := postgres.NewAdmissionRepository(client, postgres.AdmissionRepositoryOptions{
		Now:                      now,
		MaxOpenPositions:         cfg.MaxOpenPositions,
		MaxDailyAdmissions:       cfg.MaxDailyTrades,
		MaxSerializationAttempts: 1,
		StrategyVersion:          cfg.StrategyVersion,
	})
	if err != nil {
		return automationDependencies{}, err
	}
	positions, err := postgres.NewPositionRepository(client, now)
	if err != nil {
		return automationDependencies{}, err
	}
	reviewed, err := postgres.NewDashboardActivityRepository(client)
	if err != nil {
		return automationDependencies{}, err
	}
	dailyResults, err := postgres.NewDailyResultRepository(client, now)
	if err != nil {
		return automationDependencies{}, err
	}
	reportWriter, err := reporting.NewDailyJSONReportWriter(cfg.ReportDir)
	if err != nil {
		return automationDependencies{}, fmt.Errorf("configure daily paper report writer: %w", err)
	}
	dexClient, err := httpclient.New(specs.DexScreener)
	if err != nil {
		return automationDependencies{}, fmt.Errorf("configure DexScreener client: %w", err)
	}
	geckoClient, err := httpclient.New(specs.GeckoTerminal)
	if err != nil {
		return automationDependencies{}, fmt.Errorf("configure GeckoTerminal client: %w", err)
	}
	solanaClient, err := httpclient.New(specs.SolanaRPC)
	if err != nil {
		return automationDependencies{}, fmt.Errorf("configure Helius Solana RPC client: %w", err)
	}
	jupiterClient, err := httpclient.New(specs.Jupiter)
	if err != nil {
		return automationDependencies{}, fmt.Errorf("configure Jupiter quote client: %w", err)
	}
	twitterClient, err := httpclient.New(specs.TwitterAPIIO)
	if err != nil {
		return automationDependencies{}, fmt.Errorf("configure TwitterAPI.io client: %w", err)
	}
	hermesClient, err := httpclient.New(specs.Hermes)
	if err != nil {
		return automationDependencies{}, fmt.Errorf("configure Hermes client: %w", err)
	}
	dex := httpclient.NewDexScreener(dexClient)
	gecko := httpclient.NewGeckoTerminal(geckoClient)
	return automationDependencies{
		cfg:        cfg,
		now:        now,
		candidates: candidates,
		social:     social,
		verdicts:   verdicts,
		admissions: admissions,
		positions:  positions,
		reviewed:   reviewed,
		reporter:   application.NewDailyReporter(application.DailyReporterOptions{Repository: dailyResults, Writer: reportWriter}),
		dex:        dex,
		gecko:      gecko,
		market:     application.NewSourceRoutedMarket(dex, gecko),
		token:      httpclient.NewSolanaRPC(solanaClient),
		quotes:     httpclient.NewJupiter(jupiterClient, cfg.JupiterAPIKey),
		posts:      httpclient.NewTwitterAPIIO(twitterClient, cfg.TwitterAPIKey),
		hermes:     httpclient.NewHermes(hermesClient, httpclient.HermesOptions{BearerToken: cfg.HermesAPIKey, Provider: "openai-codex", Model: "gpt-5.6-sol"}),
	}, nil
}

func newAutomationHTTPClientSpecs(cfg config.Config) (automationHTTPClientSpecs, error) {
	heliusURL, err := cfg.HeliusMainnetRPCURL()
	if err != nil {
		return automationHTTPClientSpecs{}, err
	}
	return automationHTTPClientSpecs{
		DexScreener:   boundedProviderOptions(dexScreenerBaseURL, providerTimeout, providerMaxBodyBytes),
		GeckoTerminal: boundedProviderOptions(geckoTerminalBaseURL, providerTimeout, providerMaxBodyBytes),
		SolanaRPC:     boundedProviderOptions(heliusURL, providerTimeout, providerSmallMaxBodyBytes),
		Jupiter:       boundedProviderOptions(cfg.JupiterAPIURL, providerTimeout, providerMaxBodyBytes),
		TwitterAPIIO:  boundedProviderOptions(twitterAPIIOBaseURL, providerTimeout, providerMaxBodyBytes),
		Hermes:        boundedProviderOptions(cfg.HermesAPIURL, hermesTimeout, providerSmallMaxBodyBytes),
	}, nil
}

func boundedProviderOptions(baseURL string, timeout time.Duration, maxBodyBytes int64) httpclient.Options {
	return httpclient.Options{BaseURL: baseURL, Timeout: timeout, MaxBodyBytes: maxBodyBytes, MaxAttempts: 1, UserAgent: automationUserAgent}
}

type dynamicProductionScanJob struct {
	dependencies automationDependencies
}

func (job dynamicProductionScanJob) RunOnce(ctx context.Context) error {
	deps := job.dependencies
	now := deps.now().UTC()
	freshPools := application.NewTwoSourcePoolDiscovery(deps.dex, deps.gecko)
	discovery := application.NewDiscovery(freshPools, deps.candidates, application.DiscoveryOptions{Source: discoverySource, MaxPages: 1})
	scan := buildProductionScanJob(deps, now, discovery, deps.candidates, deps.reviewed)
	return runScanAndReport(ctx, scan, deps.reporter, now)
}

type dynamicMarketRetryJob struct {
	dependencies automationDependencies
}

func (job dynamicMarketRetryJob) RunOnce(ctx context.Context) error {
	deps := job.dependencies
	now := deps.now().UTC()
	discovery := application.NewMarketRetryDiscovery(application.MarketRetryDiscoveryOptions{
		Store:      deps.candidates,
		Now:        now,
		LeaseUntil: now.Add(application.MarketRetryInterval),
		Limit:      1,
	})
	return buildProductionScanJob(deps, now, discovery, nil, nil).RunOnce(ctx)
}

func buildProductionScanJob(
	deps automationDependencies,
	now time.Time,
	discovery application.ProductionDiscovery,
	futureWatches application.FuturePoolWatchStore,
	activity application.AutomationActivityStore,
) *application.ProductionScanJob {
	socialPolicy, verdictPolicy := productionScanPolicies(deps.cfg)
	evaluator := application.NewEvaluator(application.EvaluatorOptions{
		Now:              now,
		Policy:           productionCandidatePolicy(deps.cfg),
		QuoteMint:        deps.cfg.PaperQuoteMint,
		EntryInputAmount: uint64(deps.cfg.PaperTradeUSD.Micros),
		Market:           deps.market,
		Token:            deps.token,
		Quotes:           deps.quotes,
		Snapshots:        deps.candidates,
	})
	social := application.NewSocialAnalyzer(application.SocialAnalyzerOptions{
		Now:                  now,
		Provider:             deps.posts,
		Budget:               deps.social,
		Snapshots:            deps.social,
		MaxDailyRequests:     deps.cfg.MaxDailyTrades,
		EstimatedCostPerPost: domain.USD{},
	})
	admission := application.NewAdmission(application.AdmissionOptions{
		Now:                    now,
		VerdictPolicy:          verdictPolicy,
		MaxEvidenceAge:         deps.cfg.MaxAdmissionEvidenceAge,
		MaxEntryPriceImpactBPS: deps.cfg.MaxEntryPriceImpactBPS,
		Repository:             deps.admissions,
	})
	return application.NewProductionScanJob(application.ProductionScanJobOptions{
		Now:              now,
		MaxPoolAge:       deps.cfg.MaxPoolAge,
		Discovery:        discovery,
		Evaluator:        evaluator,
		Social:           social,
		SocialPolicy:     socialPolicy,
		Verdicts:         hermesVerdictProvider{provider: deps.hermes},
		VerdictPolicy:    verdictPolicy,
		StrategyVersion:  deps.cfg.StrategyVersion,
		VerdictStore:     postgresVerdictStore{repository: deps.verdicts},
		Candidates:       deps.candidates,
		FutureWatches:    futureWatches,
		MarketRetries:    deps.candidates,
		Reviewed:         deps.reviewed,
		Activity:         activity,
		Quotes:           deps.quotes,
		Admissions:       admission,
		Broker:           deps.paperBroker(),
		QuoteMint:        deps.cfg.PaperQuoteMint,
		EntryInputAmount: uint64(deps.cfg.PaperTradeUSD.Micros),
		NotionalMicros:   deps.cfg.PaperTradeUSD.Micros,
	})
}

type dynamicPositionMonitorJob struct {
	dependencies automationDependencies
}

func (job dynamicPositionMonitorJob) RunOnce(ctx context.Context) error {
	deps := job.dependencies
	now := deps.now().UTC()
	manager := application.NewPositionManager(application.PositionManagerOptions{
		Now:               now,
		Quotes:            deps.quotes,
		Positions:         deps.positions,
		Policy:            productionExitPolicy(deps.cfg),
		NetworkFeeMicros:  deps.cfg.PaperNetworkFeeMicros,
		PriorityFeeMicros: deps.cfg.PaperPriorityFeeMicros,
	})
	return runPositionMonitorWithActivity(ctx, manager, deps.reviewed, now)
}

func runPositionMonitorWithActivity(ctx context.Context, monitor application.ScheduledJob, activity application.AutomationActivityStore, now time.Time) error {
	if ctx == nil || monitor == nil || activity == nil || now.IsZero() {
		return errors.New("position monitor activity is not completely configured")
	}
	now = now.UTC()
	if err := activity.Append(ctx, domain.AutomationActivity{OccurredAt: now, Job: domain.AutomationJobMonitor, Outcome: domain.AutomationOutcomeStarted}); err != nil {
		return fmt.Errorf("record position monitor start: %w", err)
	}

	monitorErr := monitor.RunOnce(ctx)
	result := domain.AutomationActivity{OccurredAt: now, Job: domain.AutomationJobMonitor, Outcome: domain.AutomationOutcomeCompleted}
	if monitorErr != nil {
		result.Outcome = domain.AutomationOutcomeFailed
		result.Category = automationErrorCategory(monitorErr)
		result.Stage = "position_monitor"
	}
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), automationPersistTimeout)
	defer cancel()
	if err := activity.Append(persistCtx, result); err != nil {
		return errors.Join(monitorErr, fmt.Errorf("record position monitor result: %w", err))
	}
	return monitorErr
}

func (deps automationDependencies) paperBroker() *application.PaperBroker {
	return application.NewPaperBroker(application.PaperBrokerOptions{
		Now:                    deps.now,
		Sleep:                  sleepContext,
		Quotes:                 deps.quotes,
		Positions:              deps.positions,
		SimulatedLatency:       deps.cfg.SimulatedEntryLatency,
		MaxEntryPriceImpactBPS: deps.cfg.MaxEntryPriceImpactBPS,
		NetworkFeeMicros:       deps.cfg.PaperNetworkFeeMicros,
		PriorityFeeMicros:      deps.cfg.PaperPriorityFeeMicros,
	})
}

func productionCandidatePolicy(cfg config.Config) domain.CandidatePolicy {
	return domain.CandidatePolicy{
		MaxPoolAge:                  cfg.MaxPoolAge,
		MinLiquidityUSD:             cfg.MinLiquidityUSD,
		MinFiveMinuteTransactions:   cfg.MinFiveMinuteTransactions,
		MinFiveMinuteBuyShareBPS:    cfg.MinFiveMinuteBuyShareBPS,
		MinFiveMinuteTurnoverBPS:    cfg.MinFiveMinuteTurnoverBPS,
		MinFiveMinutePriceChangeBPS: cfg.MinFiveMinutePriceChangeBPS,
		MaxFiveMinutePriceChangeBPS: cfg.MaxFiveMinutePriceChangeBPS,
		MaxEntryPriceImpactBPS:      cfg.MaxEntryPriceImpactBPS,
	}
}

func productionSocialPolicy(cfg config.Config) domain.SocialPolicy {
	return domain.SocialPolicy{
		MinScore: cfg.MinSocialScore, MinUniqueAuthors: cfg.MinSocialUniqueAuthors,
		MinOriginalPosts: cfg.MinSocialOriginalPosts, MinExactMintMentions: cfg.MinSocialExactMintMentions,
		MaxWarningPosts: cfg.MaxSocialWarningPosts,
	}
}

func productionVerdictPolicy(cfg config.Config) domain.VerdictPolicy {
	return domain.VerdictPolicy{
		MinimumConfidence: cfg.MinHermesConfidence, MinimumHypeQuality: cfg.MinHermesHypeQuality,
		MaximumManipulationProbability: cfg.MaxHermesManipulationRisk,
	}
}

func productionScanPolicies(cfg config.Config) (domain.SocialPolicy, domain.VerdictPolicy) {
	return productionSocialPolicy(cfg), productionVerdictPolicy(cfg)
}

func productionExitPolicy(cfg config.Config) domain.ExitPolicy {
	return domain.ExitPolicy{TakeProfitBPS: cfg.TakeProfitBPS, StopLossBPS: cfg.StopLossBPS, MaxHoldDuration: cfg.MaxHoldDuration}
}

type hermesVerdictProvider struct {
	provider *httpclient.Hermes
}

func (provider hermesVerdictProvider) Request(ctx context.Context, posts []domain.SocialPost) (application.VerdictRecord, error) {
	result, err := provider.provider.Request(ctx, posts)
	if err != nil {
		return application.VerdictRecord{}, err
	}
	return application.VerdictRecord{
		Provider:      result.Provider,
		Model:         result.Model,
		PromptVersion: result.PromptVersion,
		InputSHA256:   result.InputSHA256,
		Latency:       result.Latency,
		InputTokens:   result.Usage.InputTokens,
		OutputTokens:  result.Usage.OutputTokens,
		TotalTokens:   result.Usage.TotalTokens,
		Verdict:       result.Verdict,
	}, nil
}

type postgresVerdictStore struct {
	repository *postgres.VerdictRepository
}

func (store postgresVerdictStore) Save(ctx context.Context, pool domain.DiscoveredPool, record application.VerdictRecord) error {
	return store.repository.Save(ctx, pool, postgres.VerdictRecord{
		Provider:      record.Provider,
		Model:         record.Model,
		PromptVersion: record.PromptVersion,
		InputSHA256:   record.InputSHA256,
		Latency:       record.Latency,
		InputTokens:   record.InputTokens,
		OutputTokens:  record.OutputTokens,
		TotalTokens:   record.TotalTokens,
		Verdict:       record.Verdict,
	})
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type dailyReportGenerator interface {
	Generate(context.Context, time.Time) ([]domain.DailyResult, error)
}

func runScanAndReport(ctx context.Context, scan application.ScheduledJob, reporter dailyReportGenerator, now time.Time) error {
	if scan == nil || reporter == nil || now.IsZero() {
		return fmt.Errorf("scan reporting job is not completely configured")
	}
	if err := scan.RunOnce(ctx); err != nil {
		return fmt.Errorf("run production scan: %w", err)
	}
	if _, err := reporter.Generate(ctx, now.UTC()); err != nil {
		return fmt.Errorf("generate daily paper report: %w", err)
	}
	return nil
}
