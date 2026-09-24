package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/config"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestStartAutomationDisabledDoesNotRequireProviderClients(t *testing.T) {
	stop, err := startAutomation(context.Background(), config.Config{}, nil, time.Now)
	if err != nil {
		t.Fatalf("startAutomation() disabled error = %v", err)
	}
	stop()
}

func TestAutomationHTTPClientSpecsUseHeliusMainnetAndDisableRetries(t *testing.T) {
	cfg := validAutomationConfig()
	specs, err := newAutomationHTTPClientSpecs(cfg)
	if err != nil {
		t.Fatalf("newAutomationHTTPClientSpecs() error = %v", err)
	}
	parsedRPC, err := url.Parse(specs.SolanaRPC.BaseURL)
	if err != nil {
		t.Fatalf("parse Solana RPC URL: %v", err)
	}
	if parsedRPC.Host != "mainnet.helius-rpc.com" || parsedRPC.Query().Get("api-key") != cfg.HeliusAPIKey {
		t.Fatalf("Solana RPC base URL = %q, want canonical Helius mainnet URL", specs.SolanaRPC.BaseURL)
	}
	for name, spec := range map[string]int{
		"dex":     specs.DexScreener.MaxAttempts,
		"gecko":   specs.GeckoTerminal.MaxAttempts,
		"solana":  specs.SolanaRPC.MaxAttempts,
		"jupiter": specs.Jupiter.MaxAttempts,
		"twitter": specs.TwitterAPIIO.MaxAttempts,
		"hermes":  specs.Hermes.MaxAttempts,
	} {
		if spec != 1 {
			t.Fatalf("%s MaxAttempts = %d, want no retries", name, spec)
		}
	}
	if specs.Jupiter.BaseURL != cfg.JupiterAPIURL {
		t.Fatalf("Jupiter base URL = %q, want configured read-only quote API host %q", specs.Jupiter.BaseURL, cfg.JupiterAPIURL)
	}
}

func TestProductionCadenceUsesExactBoundedIntervals(t *testing.T) {
	scan := &recordingScheduledJob{}
	monitor := &recordingScheduledJob{}
	retry := &recordingScheduledJob{}
	options := productionCadenceOptions(scan, monitor, retry)
	if options.Scan != scan || options.Monitor != monitor || options.Retry != retry {
		t.Fatal("production cadence did not preserve distinct scan, monitor, and retry jobs")
	}
	if options.ScanInterval != 5*time.Minute || options.ScanTimeout != 5*time.Minute ||
		options.MonitorInterval != 30*time.Second || options.MonitorTimeout != 30*time.Second ||
		options.RetryTimeout != 45*time.Second {
		t.Fatalf("cadence = scan %s/%s monitor %s/%s retry %s, want 5m, 30s, and 45s bounds", options.ScanInterval, options.ScanTimeout, options.MonitorInterval, options.MonitorTimeout, options.RetryTimeout)
	}
}

func TestRunPositionMonitorWithActivityRecordsStartedAndCompleted(t *testing.T) {
	now := time.Date(2026, time.August, 21, 11, 40, 0, 0, time.UTC)
	monitor := &recordingScheduledJob{}
	activity := &recordingAutomationActivityStore{}

	if err := runPositionMonitorWithActivity(context.Background(), monitor, activity, now); err != nil {
		t.Fatalf("runPositionMonitorWithActivity() error = %v", err)
	}
	if monitor.calls != 1 || len(activity.activities) != 2 {
		t.Fatalf("monitor/activity calls = %d/%d, want one monitor and two activity records", monitor.calls, len(activity.activities))
	}
	if activity.activities[0] != (domain.AutomationActivity{OccurredAt: now, Job: domain.AutomationJobMonitor, Outcome: domain.AutomationOutcomeStarted}) ||
		activity.activities[1] != (domain.AutomationActivity{OccurredAt: now, Job: domain.AutomationJobMonitor, Outcome: domain.AutomationOutcomeCompleted}) {
		t.Fatalf("monitor activity = %#v, want STARTED then COMPLETED", activity.activities)
	}
}

func TestRunPositionMonitorWithActivityRecordsSafeFailure(t *testing.T) {
	now := time.Date(2026, time.August, 21, 11, 40, 30, 0, time.UTC)
	monitor := &recordingScheduledJob{err: httpclient.ErrRateLimited}
	activity := &recordingAutomationActivityStore{}

	if err := runPositionMonitorWithActivity(context.Background(), monitor, activity, now); err == nil {
		t.Fatal("runPositionMonitorWithActivity() accepted failed monitor")
	}
	if monitor.calls != 1 || len(activity.activities) != 2 {
		t.Fatalf("monitor/activity calls = %d/%d, want one monitor and two activity records", monitor.calls, len(activity.activities))
	}
	want := domain.AutomationActivity{
		OccurredAt: now,
		Job:        domain.AutomationJobMonitor,
		Outcome:    domain.AutomationOutcomeFailed,
		Category:   "provider_rate_limited",
		Stage:      "position_monitor",
	}
	if activity.activities[1] != want {
		t.Fatalf("failed monitor activity = %#v, want %#v", activity.activities[1], want)
	}
}

func TestRunPositionMonitorWithActivityPersistsFailureAfterExecutionContextCancellation(t *testing.T) {
	now := time.Date(2026, time.August, 23, 14, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	monitor := &cancelingScheduledJob{cancel: cancel}
	activity := &recordingAutomationActivityStore{rejectCanceledContext: true, requireTerminalDeadline: true}

	err := runPositionMonitorWithActivity(ctx, monitor, activity, now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runPositionMonitorWithActivity() error = %v, want preserved context cancellation", err)
	}
	if len(activity.activities) != 2 {
		t.Fatalf("monitor activity count = %d, want STARTED and persisted FAILED after cancellation", len(activity.activities))
	}
	want := domain.AutomationActivity{
		OccurredAt: now,
		Job:        domain.AutomationJobMonitor,
		Outcome:    domain.AutomationOutcomeFailed,
		Category:   "context_canceled",
		Stage:      "position_monitor",
	}
	if activity.activities[1] != want {
		t.Fatalf("terminal monitor activity = %#v, want %#v", activity.activities[1], want)
	}
}

func TestRunScanAndReportGeneratesOnlyAfterSuccessfulScanForUTCDay(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 30, 0, 0, time.FixedZone("west", -7*60*60))
	scan := &recordingScheduledJob{}
	reporter := &recordingDailyReporter{}

	if err := runScanAndReport(context.Background(), scan, reporter, date); err != nil {
		t.Fatalf("runScanAndReport() error = %v", err)
	}
	if scan.calls != 1 || !reporter.date.Equal(date.UTC()) {
		t.Fatalf("scan/report calls = %d/%s, want one scan and UTC report date %s", scan.calls, reporter.date, date.UTC())
	}

	scan.err = context.DeadlineExceeded
	if err := runScanAndReport(context.Background(), scan, reporter, date); err == nil {
		t.Fatal("runScanAndReport() accepted a failed scan")
	}
	if reporter.calls != 1 {
		t.Fatalf("reporter calls after scan failure = %d, want 1", reporter.calls)
	}
}

func TestAutomationErrorCategoryRedactsCredentialBearingProviderURL(t *testing.T) {
	err := fmt.Errorf("wrapped provider error: %w", &url.Error{
		Op:  "Get",
		URL: "https://mainnet.helius-rpc.com/?api-key=synthetic-secret",
		Err: httpclient.ErrRateLimited,
	})

	if got, want := automationErrorCategory(err), "provider_rate_limited"; got != want {
		t.Fatalf("automationErrorCategory() = %q, want %q", got, want)
	}
}

func TestAutomationFailureStageRedactsCredentialBearingDiscoveryFailure(t *testing.T) {
	err := fmt.Errorf("run production scan: discover fresh two-source pools: decode https://provider.invalid/?api-key=synthetic-secret")

	if got, want := automationFailureStage(err), "discovery"; got != want {
		t.Fatalf("automationFailureStage() = %q, want %q", got, want)
	}
}

func TestAutomationFailureStageClassifiesMintInspectionWithoutLoggingWrappedValues(t *testing.T) {
	err := fmt.Errorf("run production scan: evaluate deterministic candidate solana:mint:pool: inspect token: provider response for synthetic-secret")

	if got, want := automationFailureStage(err), "mint_inspection"; got != want {
		t.Fatalf("automationFailureStage() = %q, want %q", got, want)
	}
}

func TestAutomationFailureStageClassifiesAdmissionEntryQuoteWithoutLoggingWrappedValues(t *testing.T) {
	err := fmt.Errorf("run production scan: quote eligible paper admission solana:mint:pool: provider response for synthetic-secret")

	if got, want := automationFailureStage(err), "paper_entry_quote"; got != want {
		t.Fatalf("automationFailureStage() = %q, want %q", got, want)
	}
}

func TestAutomationFailureStageClassifiesDailyReconciliationWithoutLoggingWrappedValues(t *testing.T) {
	err := fmt.Errorf("run production scan: generate daily paper report: reconcile daily result: database error for synthetic-secret")

	if got, want := automationFailureStage(err), "daily_reconcile"; got != want {
		t.Fatalf("automationFailureStage() = %q, want %q", got, want)
	}
}

func TestProductionCandidatePolicyWiresMomentumThresholds(t *testing.T) {
	cfg := config.Config{
		MaxPoolAge: 90 * time.Minute, MinLiquidityUSD: domain.USD{Micros: 5_000_000_000},
		MinFiveMinuteTransactions: 20, MinFiveMinuteBuyShareBPS: 6_500,
		MinFiveMinuteTurnoverBPS: 1_500, MinFiveMinutePriceChangeBPS: 200,
		MaxFiveMinutePriceChangeBPS: 6_000, MaxEntryPriceImpactBPS: 1_000,
	}

	policy := productionCandidatePolicy(cfg)
	if policy.MaxPoolAge != cfg.MaxPoolAge || policy.MinLiquidityUSD != cfg.MinLiquidityUSD ||
		policy.MinFiveMinuteTransactions != cfg.MinFiveMinuteTransactions ||
		policy.MinFiveMinuteBuyShareBPS != cfg.MinFiveMinuteBuyShareBPS ||
		policy.MinFiveMinuteTurnoverBPS != cfg.MinFiveMinuteTurnoverBPS ||
		policy.MinFiveMinutePriceChangeBPS != cfg.MinFiveMinutePriceChangeBPS ||
		policy.MaxFiveMinutePriceChangeBPS != cfg.MaxFiveMinutePriceChangeBPS ||
		policy.MaxEntryPriceImpactBPS != cfg.MaxEntryPriceImpactBPS {
		t.Fatalf("production policy = %#v, want all configured bold-v2 thresholds", policy)
	}
}

func TestProductionScanPoliciesWireSocialAndHermesThresholds(t *testing.T) {
	cfg := config.Config{
		MinSocialScore: 30, MinSocialUniqueAuthors: 3, MinSocialOriginalPosts: 2,
		MinSocialExactMintMentions: 2, MaxSocialWarningPosts: 1,
		MinHermesConfidence: 70, MinHermesHypeQuality: 60, MaxHermesManipulationRisk: 35,
	}

	social, verdict := productionScanPolicies(cfg)
	if social.MinScore != cfg.MinSocialScore || social.MinUniqueAuthors != cfg.MinSocialUniqueAuthors ||
		social.MinOriginalPosts != cfg.MinSocialOriginalPosts || social.MinExactMintMentions != cfg.MinSocialExactMintMentions ||
		social.MaxWarningPosts != cfg.MaxSocialWarningPosts {
		t.Fatalf("social policy = %#v, want configured thresholds", social)
	}
	if verdict.MinimumConfidence != cfg.MinHermesConfidence || verdict.MinimumHypeQuality != cfg.MinHermesHypeQuality ||
		verdict.MaximumManipulationProbability != cfg.MaxHermesManipulationRisk {
		t.Fatalf("verdict policy = %#v, want configured thresholds", verdict)
	}
}

func TestProductionExitPolicyWiresBoldMomentumThresholds(t *testing.T) {
	cfg := config.Config{TakeProfitBPS: 5_000, StopLossBPS: 2_000, MaxHoldDuration: 45 * time.Minute}
	policy := productionExitPolicy(cfg)
	if policy.TakeProfitBPS != 5_000 || policy.StopLossBPS != 2_000 || policy.MaxHoldDuration != 45*time.Minute {
		t.Fatalf("exit policy = %#v, want +5000/-2000/45m", policy)
	}
}

func validAutomationConfig() config.Config {
	return config.Config{
		PaperAutomationEnabled:      true,
		HeliusAPIKey:                "helius-key",
		TwitterAPIKey:               "twitter-key",
		HermesAPIURL:                "http://host.docker.internal:8642/v1",
		HermesAPIKey:                "hermes-key",
		JupiterAPIURL:               "https://api.jup.ag",
		JupiterAPIKey:               "jupiter-key",
		PaperTradeUSD:               domain.USD{Micros: 100_000_000},
		PaperQuoteMint:              "quote-mint",
		PaperNetworkFeeMicros:       1_000,
		PaperPriorityFeeMicros:      1_000,
		MaxOpenPositions:            3,
		MaxDailyTrades:              30,
		MaxPoolAge:                  90 * 24 * time.Hour,
		MinLiquidityUSD:             domain.USD{Micros: 10_000_000_000},
		MinFiveMinuteTransactions:   10,
		MinFiveMinuteBuyShareBPS:    6_500,
		MinFiveMinuteTurnoverBPS:    1_500,
		MinFiveMinutePriceChangeBPS: 200,
		MaxFiveMinutePriceChangeBPS: 6_000,
		MaxEntryPriceImpactBPS:      1_000,
		MinHermesConfidence:         70,
		MinSocialScore:              30,
		MinSocialUniqueAuthors:      3,
		MinSocialOriginalPosts:      2,
		MinSocialExactMintMentions:  2,
		MaxSocialWarningPosts:       1,
		MinHermesHypeQuality:        60,
		MaxHermesManipulationRisk:   35,
		MaxAdmissionEvidenceAge:     15 * time.Minute,
		TakeProfitBPS:               3_000,
		StopLossBPS:                 1_500,
		MaxHoldDuration:             time.Hour,
		DiscoveryInterval:           5 * time.Minute,
		PositionMarkInterval:        30 * time.Second,
		StrategyVersion:             "test-v1",
	}
}

type noopScheduledJob struct{}

func (noopScheduledJob) RunOnce(context.Context) error { return nil }

type recordingScheduledJob struct {
	calls int
	err   error
}

func (job *recordingScheduledJob) RunOnce(context.Context) error {
	job.calls++
	return job.err
}

type cancelingScheduledJob struct {
	cancel context.CancelFunc
}

func (job *cancelingScheduledJob) RunOnce(ctx context.Context) error {
	job.cancel()
	return ctx.Err()
}

type recordingDailyReporter struct {
	calls int
	date  time.Time
}

func (reporter *recordingDailyReporter) Generate(_ context.Context, date time.Time) ([]domain.DailyResult, error) {
	reporter.calls++
	reporter.date = date
	return nil, nil
}

type recordingAutomationActivityStore struct {
	activities              []domain.AutomationActivity
	rejectCanceledContext   bool
	requireTerminalDeadline bool
}

func (store *recordingAutomationActivityStore) Append(ctx context.Context, activity domain.AutomationActivity) error {
	if activity.Outcome != domain.AutomationOutcomeStarted {
		if store.rejectCanceledContext && ctx.Err() != nil {
			return ctx.Err()
		}
		if store.requireTerminalDeadline {
			if _, ok := ctx.Deadline(); !ok {
				return errors.New("terminal activity context is unbounded")
			}
		}
	}
	if err := activity.Validate(); err != nil {
		return err
	}
	store.activities = append(store.activities, activity)
	return nil
}
