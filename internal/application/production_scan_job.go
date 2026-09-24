package application

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	ProductionScanCandidateCap = 5
	FuturePoolWatchTTL         = 30 * time.Minute
)

type ProductionDiscovery interface {
	Run(context.Context) (DiscoveryResult, error)
}

type ProductionEvaluator interface {
	Evaluate(context.Context, domain.DiscoveredPool) (domain.CandidateEvaluation, error)
}

type ProductionSocialAnalyzer interface {
	Analyze(context.Context, domain.DiscoveredPool, domain.CandidateEvaluation) (SocialAnalysis, error)
}

type VerdictRecord struct {
	Provider      string
	Model         string
	PromptVersion string
	InputSHA256   string
	Latency       time.Duration
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	Verdict       domain.Verdict
}

type ProductionVerdictProvider interface {
	Request(context.Context, []domain.SocialPost) (VerdictRecord, error)
}

type ProductionVerdictStore interface {
	Save(context.Context, domain.DiscoveredPool, VerdictRecord) error
}

type CandidateIdentityLookup interface {
	FindID(context.Context, domain.DiscoveredPool) (int, error)
}

type ProductionReviewRecorder interface {
	AppendReviewed(context.Context, domain.ReviewedCandidate) error
}

type ProductionAdmission interface {
	Admit(context.Context, AdmissionInput) (AdmissionResult, error)
}

type ProductionPositionBroker interface {
	Open(context.Context, OpenPositionRequest) (OpenPositionResult, error)
}

// FuturePoolWatchStore retains a bounded public pool identity until its
// advertised launch time. It is consumed only once it becomes due or expires.
type FuturePoolWatchStore interface {
	Watch(context.Context, domain.DiscoveredPool, time.Time) error
	TakeDue(context.Context, time.Time, int) ([]domain.DiscoveredPool, error)
}

type ProductionScanJobOptions struct {
	Now              time.Time
	MaxPoolAge       time.Duration
	Discovery        ProductionDiscovery
	Evaluator        ProductionEvaluator
	Social           ProductionSocialAnalyzer
	SocialPolicy     domain.SocialPolicy
	Verdicts         ProductionVerdictProvider
	VerdictPolicy    domain.VerdictPolicy
	StrategyVersion  string
	VerdictStore     ProductionVerdictStore
	Candidates       CandidateIdentityLookup
	FutureWatches    FuturePoolWatchStore
	MarketRetries    MarketRetryStore
	Reviewed         ProductionReviewRecorder
	Activity         AutomationActivityStore
	Quotes           QuoteProvider
	Admissions       ProductionAdmission
	Broker           ProductionPositionBroker
	QuoteMint        string
	EntryInputAmount uint64
	NotionalMicros   int64
}

type ProductionScanJob struct {
	options ProductionScanJobOptions
}

func NewProductionScanJob(options ProductionScanJobOptions) *ProductionScanJob {
	return &ProductionScanJob{options: options}
}

func (job *ProductionScanJob) RunOnce(ctx context.Context) (err error) {
	if err := validateProductionScanJobOptions(job); err != nil {
		return err
	}
	job.recordActivity(ctx, domain.AutomationOutcomeStarted)
	defer func() {
		if err != nil {
			job.recordActivity(ctx, domain.AutomationOutcomeFailed)
			return
		}
		job.recordActivity(ctx, domain.AutomationOutcomeCompleted)
	}()
	now := job.options.Now.UTC()
	var due []domain.DiscoveredPool
	if job.options.FutureWatches != nil {
		due, err = job.options.FutureWatches.TakeDue(ctx, now, ProductionScanCandidateCap)
		if err != nil {
			return fmt.Errorf("load due future pool watches: %w", err)
		}
	}
	result, err := job.options.Discovery.Run(ctx)
	if err != nil {
		return fmt.Errorf("discover fresh two-source pools: %w", err)
	}
	for _, pool := range firstProductionCandidates(append(due, result.Pools...)) {
		if pool.CreatedAt.After(now) {
			if job.options.FutureWatches == nil {
				return errors.New("future pool watch store is not configured")
			}
			if err := job.options.FutureWatches.Watch(ctx, pool, pool.CreatedAt.Add(FuturePoolWatchTTL)); err != nil {
				return fmt.Errorf("watch future pool %s: %w", pool.Identity(), err)
			}
			continue
		}
		if !freshProductionPool(now, pool, job.options.MaxPoolAge) {
			continue
		}
		if err := job.evaluateAndMaybeOpen(ctx, pool); err != nil {
			return err
		}
	}
	return nil
}

func (job *ProductionScanJob) evaluateAndMaybeOpen(ctx context.Context, pool domain.DiscoveredPool) error {
	evaluation, err := job.options.Evaluator.Evaluate(ctx, pool)
	if err != nil {
		if isMarketEvidenceUnavailable(err) {
			job.recordNonAdmission(ctx, pool, domain.ReviewReasonMarketEvidenceUnavailable)
			nextAttemptAt := job.options.Now.UTC().Add(MarketRetryInterval)
			expiresAt := earlierTime(
				job.options.Now.UTC().Add(MarketRetryTTL),
				pool.CreatedAt.UTC().Add(job.options.MaxPoolAge),
			)
			if expiresAt.After(nextAttemptAt) {
				if err := job.options.MarketRetries.Schedule(ctx, pool, nextAttemptAt, expiresAt); err != nil {
					return fmt.Errorf("schedule market evidence retry %s: %w", pool.Identity(), err)
				}
			}
			return nil
		}
		return fmt.Errorf("evaluate deterministic candidate %s: %w", pool.Identity(), err)
	}
	if err := job.options.MarketRetries.Complete(ctx, pool); err != nil {
		return fmt.Errorf("complete market evidence retry %s: %w", pool.Identity(), err)
	}
	if !evaluation.Eligible {
		job.recordNonAdmission(ctx, pool, reviewedReasonForEvaluation(evaluation))
		return nil
	}
	social, err := job.options.Social.Analyze(ctx, pool, evaluation)
	if err != nil {
		return fmt.Errorf("analyze eligible social evidence %s: %w", pool.Identity(), err)
	}
	if len(social.Posts) == 0 {
		job.recordNonAdmission(ctx, pool, domain.ReviewReasonSocialEvidenceUnavailable)
		return nil
	}
	if !job.options.SocialPolicy.Evaluate(social.Metrics, social.Score).Eligible {
		job.recordNonAdmission(ctx, pool, domain.ReviewReasonDeterministicSocialQuality)
		return nil
	}
	verdict, err := job.options.Verdicts.Request(ctx, social.Posts)
	if err != nil {
		return fmt.Errorf("request eligible Hermes verdict %s: %w", pool.Identity(), err)
	}
	if err := verdict.Validate(); err != nil {
		return fmt.Errorf("validate eligible Hermes verdict %s: %w", pool.Identity(), err)
	}
	if err := job.options.VerdictStore.Save(ctx, pool, verdict); err != nil {
		return fmt.Errorf("store eligible Hermes verdict %s: %w", pool.Identity(), err)
	}
	if !job.options.VerdictPolicy.Accepts(verdict.Verdict) {
		job.recordNonAdmission(ctx, pool, domain.ReviewReasonHermesVerdictThreshold)
		return nil
	}
	entryQuote, err := job.options.Quotes.Quote(ctx, job.options.QuoteMint, pool.MintAddress, job.options.EntryInputAmount)
	if err != nil {
		return fmt.Errorf("quote eligible paper admission %s: %w", pool.Identity(), err)
	}
	candidateID, err := job.options.Candidates.FindID(ctx, pool)
	if err != nil {
		return fmt.Errorf("load eligible candidate identity %s: %w", pool.Identity(), err)
	}
	idempotencyKey := productionAdmissionKey(job.options.StrategyVersion, pool)
	admitted, err := job.options.Admissions.Admit(ctx, AdmissionInput{
		Evaluation:         evaluation,
		Verdict:            verdict.Verdict,
		EvidenceObservedAt: job.options.Now.UTC(),
		MintAddress:        pool.MintAddress,
		QuoteMint:          job.options.QuoteMint,
		EntryInputAmount:   job.options.EntryInputAmount,
		EntryQuote:         entryQuote,
		IdempotencyKey:     idempotencyKey,
		CandidateID:        candidateID,
		NotionalMicros:     job.options.NotionalMicros,
	})
	if err != nil {
		return fmt.Errorf("admit eligible paper candidate %s: %w", pool.Identity(), err)
	}
	if !admitted.Admitted {
		job.recordNonAdmission(ctx, pool, domain.ReviewReasonAdmissionNotAdmitted)
		return nil
	}
	if _, err := job.options.Broker.Open(ctx, OpenPositionRequest{IdempotencyKey: idempotencyKey, QuoteMint: job.options.QuoteMint, MintAddress: pool.MintAddress, EntryInputAmount: job.options.EntryInputAmount}); err != nil {
		return fmt.Errorf("open admitted paper position %s: %w", pool.Identity(), err)
	}
	return nil
}

func (job *ProductionScanJob) recordNonAdmission(ctx context.Context, pool domain.DiscoveredPool, reason domain.ReviewedCandidateReason) {
	if job.options.Reviewed == nil {
		return
	}
	_ = job.options.Reviewed.AppendReviewed(ctx, domain.ReviewedCandidate{
		MintAddress: pool.MintAddress,
		CheckedAt:   job.options.Now.UTC(),
		Outcome:     domain.ReviewedCandidateNotTraded,
		Reason:      reason,
	})
}

func reviewedReasonForEvaluation(evaluation domain.CandidateEvaluation) domain.ReviewedCandidateReason {
	for _, rule := range evaluation.Rules {
		if rule.Passed {
			continue
		}
		switch rule.Code {
		case domain.RulePoolAge:
			return domain.ReviewReasonDeterministicPoolAge
		case domain.RuleLiquidity:
			return domain.ReviewReasonDeterministicLiquidity
		case domain.RuleFiveMinuteActivity:
			return domain.ReviewReasonDeterministicFiveMinuteVolume
		case domain.RuleFiveMinuteBuyShare:
			return domain.ReviewReasonDeterministicBuyShare
		case domain.RuleFiveMinuteTurnover:
			return domain.ReviewReasonDeterministicTurnover
		case domain.RuleFiveMinutePriceChange:
			return domain.ReviewReasonDeterministicPriceChange

		case domain.RuleEntryPriceImpact:
			return domain.ReviewReasonDeterministicEntryPriceImpact
		case domain.RuleEntryRoute:
			return domain.ReviewReasonDeterministicEntryRoute
		case domain.RuleExitRoute:
			return domain.ReviewReasonDeterministicExitRoute
		case domain.RuleMintAuthority:
			return domain.ReviewReasonDeterministicMintAuthority
		case domain.RuleFreezeAuthority:
			return domain.ReviewReasonDeterministicFreezeAuthority
		case domain.RuleTokenProgram:
			return domain.ReviewReasonDeterministicTokenProgram
		}
	}
	return domain.ReviewReasonAdmissionNotAdmitted
}

func (job *ProductionScanJob) recordActivity(ctx context.Context, outcome domain.AutomationOutcome) {
	if job.options.Activity == nil {
		return
	}
	activity := domain.AutomationActivity{OccurredAt: job.options.Now.UTC(), Job: domain.AutomationJobScan, Outcome: outcome}
	if outcome == domain.AutomationOutcomeFailed {
		activity.Category = "internal_error"
		activity.Stage = "internal"
	}
	_ = job.options.Activity.Append(ctx, activity)
}

func validateProductionScanJobOptions(job *ProductionScanJob) error {
	if job == nil || job.options.Now.IsZero() || job.options.MaxPoolAge <= 0 || job.options.Discovery == nil || job.options.Evaluator == nil || job.options.Social == nil || job.options.Verdicts == nil || job.options.VerdictStore == nil || job.options.Candidates == nil || job.options.MarketRetries == nil || job.options.Quotes == nil || job.options.Admissions == nil || job.options.Broker == nil {
		return errors.New("production scan job is not completely configured")
	}
	if job.options.SocialPolicy.Validate() != nil || job.options.VerdictPolicy.Validate() != nil {
		return errors.New("production scan job policies are invalid")
	}
	if strings.TrimSpace(job.options.StrategyVersion) == "" || strings.TrimSpace(job.options.QuoteMint) == "" || job.options.EntryInputAmount == 0 || job.options.NotionalMicros <= 0 {
		return errors.New("production scan job paper sizing is invalid")
	}
	return nil
}

func earlierTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func firstProductionCandidates(pools []domain.DiscoveredPool) []domain.DiscoveredPool {
	if len(pools) <= ProductionScanCandidateCap {
		return pools
	}
	return pools[:ProductionScanCandidateCap]
}

func freshProductionPool(now time.Time, pool domain.DiscoveredPool, maxAge time.Duration) bool {
	if pool.CreatedAt.IsZero() || pool.CreatedAt.After(now) || maxAge <= 0 {
		return false
	}
	return now.Sub(pool.CreatedAt) <= maxAge
}

func productionAdmissionKey(strategyVersion string, pool domain.DiscoveredPool) string {
	return strings.TrimSpace(strategyVersion) + ":" + pool.Identity() + ":" + pool.CreatedAt.UTC().Format(time.RFC3339Nano)
}

func (record VerdictRecord) Validate() error {
	if strings.TrimSpace(record.Provider) == "" || strings.TrimSpace(record.Model) == "" || strings.TrimSpace(record.PromptVersion) == "" {
		return errors.New("verdict provider, model, and prompt version are required")
	}
	digest, err := hex.DecodeString(record.InputSHA256)
	if err != nil || len(digest) != 32 {
		return errors.New("verdict input SHA-256 must be a 64-character hexadecimal digest")
	}
	if record.Latency < 0 || record.InputTokens < 0 || record.OutputTokens < 0 || record.TotalTokens < 0 {
		return errors.New("verdict audit metrics cannot be negative")
	}
	knownPostIDs := make(map[string]struct{}, len(record.Verdict.EvidencePostIDs))
	for _, postID := range record.Verdict.EvidencePostIDs {
		knownPostIDs[postID] = struct{}{}
	}
	return record.Verdict.Validate(knownPostIDs)
}
