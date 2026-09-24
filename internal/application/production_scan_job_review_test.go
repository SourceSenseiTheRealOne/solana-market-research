package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestProductionScanJobRecordsPublicNonAdmissionForIneligibleCandidate(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{productionPool(now.Add(-5 * time.Minute))}}}
	evaluator := &productionEvaluator{evaluation: domain.CandidateEvaluation{Eligible: false, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: false}}}}
	recorder := &productionReviewRecorder{}
	options := validProductionScanOptions(now, discovery, evaluator)
	options.Reviewed = recorder

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if recorder.calls != 1 || recorder.candidate.MintAddress != "mint" || !recorder.candidate.CheckedAt.Equal(now) || recorder.candidate.Outcome != domain.ReviewedCandidateNotTraded || recorder.candidate.Reason != domain.ReviewReasonDeterministicLiquidity {
		t.Fatalf("reviewed candidate = %#v calls=%d, want one safe non-admission", recorder.candidate, recorder.calls)
	}
}

func TestProductionScanJobRecordsSafeScanLifecycle(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	discovery := &productionDiscovery{result: application.DiscoveryResult{}}
	evaluator := &productionEvaluator{}
	activity := &productionActivityRecorder{}
	options := validProductionScanOptions(now, discovery, evaluator)
	options.Activity = activity

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(activity.activities) != 2 || activity.activities[0].Job != domain.AutomationJobScan || activity.activities[0].Outcome != domain.AutomationOutcomeStarted || activity.activities[1].Outcome != domain.AutomationOutcomeCompleted {
		t.Fatalf("activity = %#v, want safe SCAN STARTED then COMPLETED", activity.activities)
	}
}

func TestProductionScanJobRecordsMarketEvidenceUnavailableAndContinues(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	first := productionPool(now.Add(-5 * time.Minute))
	first.MintAddress = "market-unavailable-mint"
	first.PoolAddress = "market-unavailable-pool"
	second := productionPool(now.Add(-4 * time.Minute))
	second.MintAddress = "liquidity-mint"
	second.PoolAddress = "liquidity-pool"
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{first, second}}}
	recorder := &productionReviewSequenceRecorder{}
	options := validProductionScanOptions(now, discovery, &productionEvaluator{})
	options.Evaluator = &marketFailureThenIneligibleEvaluator{}
	options.Reviewed = recorder

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if got, want := len(recorder.candidates), 2; got != want {
		t.Fatalf("reviewed candidate count = %d, want %d", got, want)
	}
	if got, want := recorder.candidates[0].Reason, domain.ReviewedCandidateReason("market_evidence_unavailable"); got != want {
		t.Fatalf("first reason = %q, want %q", got, want)
	}
	if got, want := recorder.candidates[1].Reason, domain.ReviewReasonDeterministicLiquidity; got != want {
		t.Fatalf("second reason = %q, want %q", got, want)
	}
}

func TestProductionScanJobSchedulesTransientMarketFailureForThirtySeconds(t *testing.T) {
	now := time.Date(2026, time.August, 31, 10, 30, 0, 0, time.UTC)
	pool := productionPool(now.Add(-3 * time.Minute))
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{pool}}}
	retries := &marketRetryStoreFake{}
	recorder := &productionReviewRecorder{}
	options := validProductionScanOptions(now, discovery, &productionEvaluator{err: fmt.Errorf("fetch market evidence: %w", marketEvidenceUnavailableError{})})
	options.MarketRetries = retries
	options.Reviewed = recorder

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if retries.scheduleCalls != 1 || retries.scheduled.Identity() != pool.Identity() {
		t.Fatalf("scheduled retry = calls=%d pool=%#v", retries.scheduleCalls, retries.scheduled)
	}
	if !retries.nextAttemptAt.Equal(now.Add(30*time.Second)) || !retries.expiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("retry bounds = next %s expiry %s", retries.nextAttemptAt, retries.expiresAt)
	}
	if recorder.candidate.Reason != domain.ReviewReasonMarketEvidenceUnavailable {
		t.Fatalf("review reason = %q", recorder.candidate.Reason)
	}
}

func TestProductionScanJobDoesNotExtendRetryBeyondPoolAge(t *testing.T) {
	now := time.Date(2026, time.August, 31, 10, 30, 0, 0, time.UTC)
	pool := productionPool(now.Add(-29 * time.Minute))
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{pool}}}
	retries := &marketRetryStoreFake{}
	options := validProductionScanOptions(now, discovery, &productionEvaluator{err: fmt.Errorf("fetch market evidence: %w", marketEvidenceUnavailableError{})})
	options.MarketRetries = retries

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if want := pool.CreatedAt.Add(options.MaxPoolAge); !retries.expiresAt.Equal(want) {
		t.Fatalf("retry expiry = %s, want pool-age bound %s", retries.expiresAt, want)
	}
}

func TestProductionScanJobCompletesRetryAfterUsableMarketEvaluation(t *testing.T) {
	now := time.Date(2026, time.August, 31, 10, 30, 0, 0, time.UTC)
	pool := productionPool(now.Add(-3 * time.Minute))
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{pool}}}
	retries := &marketRetryStoreFake{completeErr: fmt.Errorf("complete retry")}
	social := &productionSocial{analysis: eligibleProductionSocialAnalysis(now)}
	options := validProductionScanOptions(now, discovery, &productionEvaluator{evaluation: eligibleProductionEvaluation()})
	options.MarketRetries = retries
	options.Social = social

	err := application.NewProductionScanJob(options).RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() accepted retry completion failure")
	}
	if retries.completeCalls != 1 || retries.completed.Identity() != pool.Identity() {
		t.Fatalf("completed retry = calls=%d pool=%#v", retries.completeCalls, retries.completed)
	}
	if social.calls != 0 {
		t.Fatalf("social calls = %d, want zero before retry completion", social.calls)
	}
}

func TestProductionScanJobRecordsSocialEvidenceUnavailableWithoutRequestingHermes(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{productionPool(now.Add(-5 * time.Minute))}}}
	verdicts := &productionVerdicts{record: validProductionVerdictRecord()}
	recorder := &productionReviewSequenceRecorder{}
	options := validProductionScanOptions(now, discovery, &productionEvaluator{evaluation: eligibleProductionEvaluation()})
	options.Social = &productionSocial{analysis: application.SocialAnalysis{}}
	options.Verdicts = verdicts
	options.Reviewed = recorder

	if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if verdicts.calls != 0 {
		t.Fatalf("Hermes verdict calls = %d, want 0 for empty social evidence", verdicts.calls)
	}
	if got, want := len(recorder.candidates), 1; got != want {
		t.Fatalf("reviewed candidate count = %d, want %d", got, want)
	}
	if got, want := recorder.candidates[0].Reason, domain.ReviewedCandidateReason("social_evidence_unavailable"); got != want {
		t.Fatalf("reviewed reason = %q, want %q", got, want)
	}
}

func TestProductionScanJobMapsBoldMomentumRulesToFixedReasons(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rule string
		want domain.ReviewedCandidateReason
	}{
		{name: "buy share", rule: domain.RuleFiveMinuteBuyShare, want: domain.ReviewReasonDeterministicBuyShare},
		{name: "turnover", rule: domain.RuleFiveMinuteTurnover, want: domain.ReviewReasonDeterministicTurnover},
		{name: "price change", rule: domain.RuleFiveMinutePriceChange, want: domain.ReviewReasonDeterministicPriceChange},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			discovery := &productionDiscovery{result: application.DiscoveryResult{Pools: []domain.DiscoveredPool{productionPool(now.Add(-5 * time.Minute))}}}
			evaluator := &productionEvaluator{evaluation: domain.CandidateEvaluation{Eligible: false, Rules: []domain.RuleResult{{Code: test.rule, Passed: false}}}}
			recorder := &productionReviewRecorder{}
			options := validProductionScanOptions(now, discovery, evaluator)
			options.Reviewed = recorder
			if err := application.NewProductionScanJob(options).RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if recorder.candidate.Reason != test.want {
				t.Fatalf("review reason = %q, want %q", recorder.candidate.Reason, test.want)
			}
		})
	}
}

type productionReviewRecorder struct {
	calls     int
	candidate domain.ReviewedCandidate
}

func (recorder *productionReviewRecorder) AppendReviewed(_ context.Context, candidate domain.ReviewedCandidate) error {
	recorder.calls++
	recorder.candidate = candidate
	return nil
}

type productionActivityRecorder struct{ activities []domain.AutomationActivity }

func (recorder *productionActivityRecorder) Append(_ context.Context, activity domain.AutomationActivity) error {
	recorder.activities = append(recorder.activities, activity)
	return nil
}

type marketEvidenceUnavailableError struct{}

func (marketEvidenceUnavailableError) Error() string              { return "market evidence unavailable" }
func (marketEvidenceUnavailableError) MarketEvidenceUnavailable() {}

type marketFailureThenIneligibleEvaluator struct{ calls int }

func (evaluator *marketFailureThenIneligibleEvaluator) Evaluate(context.Context, domain.DiscoveredPool) (domain.CandidateEvaluation, error) {
	evaluator.calls++
	if evaluator.calls == 1 {
		return domain.CandidateEvaluation{}, fmt.Errorf("fetch market evidence: %w", marketEvidenceUnavailableError{})
	}
	return domain.CandidateEvaluation{Eligible: false, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: false}}}, nil
}

type productionReviewSequenceRecorder struct{ candidates []domain.ReviewedCandidate }

func (recorder *productionReviewSequenceRecorder) AppendReviewed(_ context.Context, candidate domain.ReviewedCandidate) error {
	recorder.candidates = append(recorder.candidates, candidate)
	return nil
}
