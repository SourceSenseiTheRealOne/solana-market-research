package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestProductionScanJobSkipsStalePoolsBeforeDeterministicEvaluation(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	pool := productionPool(now.Add(-31 * time.Minute))
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{pool}}}
	evaluator := &productionEvaluator{evaluation: eligibleProductionEvaluation()}

	job := application.NewProductionScanJob(validProductionScanOptions(now, discovery, evaluator))
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if evaluator.calls != 0 {
		t.Fatalf("evaluator calls = %d, want 0 for stale pool", evaluator.calls)
	}
}

func TestProductionScanJobRejectsBlankStrategyVersionBeforeProviders(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	discovery := &productionDiscovery{}
	evaluator := &productionEvaluator{}
	options := validProductionScanOptions(now, discovery, evaluator)
	options.StrategyVersion = " \t "

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() accepted a blank strategy version")
	}
	if discovery.calls != 0 || evaluator.calls != 0 {
		t.Fatalf("discovery/evaluator calls = %d/%d, want zero before configuration validation", discovery.calls, evaluator.calls)
	}
}

func TestProductionScanJobCallsSocialHermesAdmissionAndBrokerOnlyAfterEligibility(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{productionPool(now.Add(-5 * time.Minute))}}}
	evaluator := &productionEvaluator{evaluation: domain.CandidateEvaluation{Eligible: false, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: false}}}}
	social := &productionSocial{analysis: eligibleProductionSocialAnalysis(now)}
	verdicts := &productionVerdicts{record: validProductionVerdictRecord()}
	store := &productionVerdictStore{}
	admissions := &productionAdmissions{result: application.AdmissionResult{Admitted: true}}
	broker := &productionBroker{}
	options := validProductionScanOptions(now, discovery, evaluator)
	options.Social = social
	options.Verdicts = verdicts
	options.VerdictStore = store
	options.Admissions = admissions
	options.Broker = broker
	quotes := options.Quotes.(*productionQuotes)

	job := application.NewProductionScanJob(options)
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if social.calls != 0 || verdicts.calls != 0 || store.calls != 0 || quotes.calls != 0 || admissions.calls != 0 || broker.calls != 0 {
		t.Fatalf("ineligible calls social/hermes/store/quote/admit/open = %d/%d/%d/%d/%d/%d, want all zero", social.calls, verdicts.calls, store.calls, quotes.calls, admissions.calls, broker.calls)
	}

	evaluator.evaluation = eligibleProductionEvaluation()
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() eligible error = %v", err)
	}
	if social.calls != 1 || verdicts.calls != 1 || store.calls != 1 || admissions.calls != 1 || broker.calls != 1 {
		t.Fatalf("eligible calls social/hermes/store/admit/open = %d/%d/%d/%d/%d, want all one", social.calls, verdicts.calls, store.calls, admissions.calls, broker.calls)
	}
	if admissions.input.CandidateID != 42 || admissions.input.MintAddress != "mint" || admissions.input.EntryQuote.OutputMint != "mint" {
		t.Fatalf("admission input = %#v, want candidate identity and fresh entry quote", admissions.input)
	}
	if broker.request.IdempotencyKey != admissions.input.IdempotencyKey || broker.request.MintAddress != "mint" {
		t.Fatalf("broker request = %#v, admission key = %q", broker.request, admissions.input.IdempotencyKey)
	}
}

func TestProductionScanJobStopsAfterSocialPolicyRejection(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{productionPool(now.Add(-5 * time.Minute))}}}
	social := &productionSocial{analysis: eligibleProductionSocialAnalysis(now)}
	social.analysis.Score = 29
	verdicts := &productionVerdicts{record: validProductionVerdictRecord()}
	store := &productionVerdictStore{}
	admissions := &productionAdmissions{result: application.AdmissionResult{Admitted: true}}
	broker := &productionBroker{}
	reviewed := &productionReviewRecorder{}
	options := validProductionScanOptions(now, discovery, &productionEvaluator{evaluation: eligibleProductionEvaluation()})
	options.Social, options.Verdicts, options.VerdictStore = social, verdicts, store
	options.Admissions, options.Broker, options.Reviewed = admissions, broker, reviewed
	quotes := options.Quotes.(*productionQuotes)

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if social.calls != 1 || verdicts.calls != 0 || store.calls != 0 || quotes.calls != 0 || admissions.calls != 0 || broker.calls != 0 {
		t.Fatalf("social/hermes/store/quote/admit/open calls = %d/%d/%d/%d/%d/%d", social.calls, verdicts.calls, store.calls, quotes.calls, admissions.calls, broker.calls)
	}
	if reviewed.candidate.Reason != domain.ReviewReasonDeterministicSocialQuality {
		t.Fatalf("review reason = %q", reviewed.candidate.Reason)
	}
}

func TestProductionScanJobStoresHermesAuditBeforePolicyRejection(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{productionPool(now.Add(-5 * time.Minute))}}}
	verdicts := &productionVerdicts{record: validProductionVerdictRecord()}
	verdicts.record.Verdict.ManipulationProbability = 36
	store := &productionVerdictStore{}
	admissions := &productionAdmissions{result: application.AdmissionResult{Admitted: true}}
	broker := &productionBroker{}
	reviewed := &productionReviewRecorder{}
	options := validProductionScanOptions(now, discovery, &productionEvaluator{evaluation: eligibleProductionEvaluation()})
	options.Verdicts, options.VerdictStore = verdicts, store
	options.Admissions, options.Broker, options.Reviewed = admissions, broker, reviewed
	quotes := options.Quotes.(*productionQuotes)

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if verdicts.calls != 1 || store.calls != 1 || quotes.calls != 0 || admissions.calls != 0 || broker.calls != 0 {
		t.Fatalf("Hermes/store/quote/admit/open calls = %d/%d/%d/%d/%d", verdicts.calls, store.calls, quotes.calls, admissions.calls, broker.calls)
	}
	if reviewed.candidate.Reason != domain.ReviewReasonHermesVerdictThreshold {
		t.Fatalf("review reason = %q", reviewed.candidate.Reason)
	}
}

func TestProductionScanJobCapsCandidatesPerCycle(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	pools := make([]domain.DiscoveredPool, 0, 6)
	for index := 0; index < 6; index++ {
		pool := productionPool(now.Add(-5 * time.Minute))
		pool.MintAddress = "mint-" + string(rune('a'+index))
		pool.PoolAddress = "pool-" + string(rune('a'+index))
		pools = append(pools, pool)
	}
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: pools}}
	evaluator := &productionEvaluator{evaluation: domain.CandidateEvaluation{Eligible: false, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: false}}}}

	job := application.NewProductionScanJob(validProductionScanOptions(now, discovery, evaluator))
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if evaluator.calls != 5 {
		t.Fatalf("evaluator calls = %d, want five candidate cap", evaluator.calls)
	}
}

func validProductionScanOptions(now time.Time, discovery *productionDiscovery, evaluator *productionEvaluator) application.ProductionScanJobOptions {
	quotes := &productionQuotes{quote: domain.ExecutableQuote{ObservedAt: now, InputMint: "quote", OutputMint: "mint", InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 100, RoutePlan: []domain.RouteLeg{{AMMKey: "amm"}}}}
	return application.ProductionScanJobOptions{
		Now:              now,
		MaxPoolAge:       30 * time.Minute,
		Discovery:        discovery,
		Evaluator:        evaluator,
		Social:           &productionSocial{analysis: eligibleProductionSocialAnalysis(now)},
		SocialPolicy:     domain.SocialPolicy{MinScore: 30, MinUniqueAuthors: 3, MinOriginalPosts: 2, MinExactMintMentions: 2, MaxWarningPosts: 1},
		Verdicts:         &productionVerdicts{record: validProductionVerdictRecord()},
		VerdictPolicy:    domain.VerdictPolicy{MinimumConfidence: 70, MinimumHypeQuality: 60, MaximumManipulationProbability: 35},
		StrategyVersion:  "bold-momentum-v2",
		VerdictStore:     &productionVerdictStore{},
		Candidates:       productionCandidateIDs{},
		MarketRetries:    &marketRetryStoreFake{},
		Quotes:           quotes,
		Admissions:       &productionAdmissions{result: application.AdmissionResult{Admitted: true}},
		Broker:           &productionBroker{},
		QuoteMint:        "quote",
		EntryInputAmount: 10_000_000,
		NotionalMicros:   10_000_000,
	}
}

func productionPool(createdAt time.Time) domain.DiscoveredPool {
	return domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: createdAt}
}

func eligibleProductionEvaluation() domain.CandidateEvaluation {
	return domain.CandidateEvaluation{Eligible: true, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: true}}}
}

func eligibleProductionSocialAnalysis(now time.Time) application.SocialAnalysis {
	return application.SocialAnalysis{
		Posts: []domain.SocialPost{
			{ID: "post-1", AuthorID: "author-1", CreatedAt: now.Add(-time.Minute), Text: "mint"},
			{ID: "post-2", AuthorID: "author-2", CreatedAt: now.Add(-2 * time.Minute), Text: "mint"},
			{ID: "post-3", AuthorID: "author-3", CreatedAt: now.Add(-3 * time.Minute), Text: "launch"},
		},
		Metrics: domain.SocialMetrics{Posts: 3, UniqueAuthors: 3, OriginalPosts: 2, Reposts: 1, ExactMintMentions: 2, WarningPosts: 1},
		Score:   30,
	}
}

func validProductionVerdictRecord() application.VerdictRecord {
	return application.VerdictRecord{Provider: "openai-codex", Model: "gpt-5.6-sol", PromptVersion: "v1", InputSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Verdict: domain.Verdict{Outcome: domain.VerdictBuy, Confidence: 90, HypeQualityScore: 70, ManipulationProbability: 20, Reasons: []string{"organic activity"}}}
}

type productionDiscovery struct {
	calls  int
	result application.DiscoveryResult
	err    error
}

func (discovery *productionDiscovery) Run(context.Context) (application.DiscoveryResult, error) {
	discovery.calls++
	return discovery.result, discovery.err
}

type productionEvaluator struct {
	calls      int
	evaluation domain.CandidateEvaluation
	err        error
}

func (evaluator *productionEvaluator) Evaluate(context.Context, domain.DiscoveredPool) (domain.CandidateEvaluation, error) {
	evaluator.calls++
	return evaluator.evaluation, evaluator.err
}

type productionSocial struct {
	calls    int
	analysis application.SocialAnalysis
	err      error
}

func (social *productionSocial) Analyze(context.Context, domain.DiscoveredPool, domain.CandidateEvaluation) (application.SocialAnalysis, error) {
	social.calls++
	return social.analysis, social.err
}

type productionVerdicts struct {
	calls  int
	record application.VerdictRecord
	err    error
}

func (verdicts *productionVerdicts) Request(context.Context, []domain.SocialPost) (application.VerdictRecord, error) {
	verdicts.calls++
	return verdicts.record, verdicts.err
}

type productionVerdictStore struct {
	calls int
	err   error
}

func (store *productionVerdictStore) Save(context.Context, domain.DiscoveredPool, application.VerdictRecord) error {
	store.calls++
	return store.err
}

type productionCandidateIDs struct{}

func (productionCandidateIDs) FindID(context.Context, domain.DiscoveredPool) (int, error) {
	return 42, nil
}

type productionQuotes struct {
	calls int
	quote domain.ExecutableQuote
	err   error
}

func (quotes *productionQuotes) Quote(context.Context, string, string, uint64) (domain.ExecutableQuote, error) {
	quotes.calls++
	if quotes.err != nil {
		return domain.ExecutableQuote{}, quotes.err
	}
	if quotes.quote.OutputMint != "mint" {
		return domain.ExecutableQuote{}, errors.New("unexpected quote")
	}
	return quotes.quote, nil
}

func (quotes *productionQuotes) Health(context.Context) error { return nil }

type productionAdmissions struct {
	calls  int
	input  application.AdmissionInput
	result application.AdmissionResult
	err    error
}

func (admissions *productionAdmissions) Admit(_ context.Context, input application.AdmissionInput) (application.AdmissionResult, error) {
	admissions.calls++
	admissions.input = input
	return admissions.result, admissions.err
}

type productionBroker struct {
	calls   int
	request application.OpenPositionRequest
	err     error
}

func (broker *productionBroker) Open(_ context.Context, request application.OpenPositionRequest) (application.OpenPositionResult, error) {
	broker.calls++
	broker.request = request
	return application.OpenPositionResult{Opened: true}, broker.err
}
