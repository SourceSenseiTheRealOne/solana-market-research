package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestAdmissionRejectsInvalidRequestsBeforePersistence(t *testing.T) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*application.AdmissionInput)
	}{
		{name: "failed deterministic evaluation", mutate: func(input *application.AdmissionInput) {
			input.Evaluation.Eligible = false
			input.Evaluation.Rules[0].Passed = false
		}},
		{name: "watch verdict", mutate: func(input *application.AdmissionInput) { input.Verdict.Outcome = domain.VerdictWatch }},
		{name: "low confidence", mutate: func(input *application.AdmissionInput) { input.Verdict.Confidence = 69 }},
		{name: "low hype quality", mutate: func(input *application.AdmissionInput) { input.Verdict.HypeQualityScore = 59 }},
		{name: "high manipulation probability", mutate: func(input *application.AdmissionInput) { input.Verdict.ManipulationProbability = 36 }},
		{name: "stale evidence", mutate: func(input *application.AdmissionInput) {
			input.EvidenceObservedAt = now.Add(-15*time.Minute - time.Nanosecond)
		}},
		{name: "missing second entry quote", mutate: func(input *application.AdmissionInput) { input.EntryQuote.RoutePlan = nil }},
		{name: "excessive second entry price impact", mutate: func(input *application.AdmissionInput) { input.EntryQuote.PriceImpactBPS = 501 }},
		{name: "empty idempotency key", mutate: func(input *application.AdmissionInput) { input.IdempotencyKey = "" }},
		{name: "missing persisted candidate", mutate: func(input *application.AdmissionInput) { input.CandidateID = 0 }},
		{name: "zero paper notional", mutate: func(input *application.AdmissionInput) { input.NotionalMicros = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &recordingAdmissionRepository{}
			service := application.NewAdmission(application.AdmissionOptions{Now: now, VerdictPolicy: domain.VerdictPolicy{MinimumConfidence: 70, MinimumHypeQuality: 60, MaximumManipulationProbability: 35}, MaxEvidenceAge: 15 * time.Minute, MaxEntryPriceImpactBPS: 500, Repository: repository})
			input := validAdmissionInput(now)
			test.mutate(&input)
			if _, err := service.Admit(context.Background(), input); err == nil {
				t.Fatal("Admit() accepted an invalid paper admission")
			}
			if repository.calls != 0 {
				t.Fatalf("repository calls = %d, want 0", repository.calls)
			}
		})
	}
}

func TestAdmissionPersistsValidatedRequest(t *testing.T) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	repository := &recordingAdmissionRepository{result: application.AdmissionResult{Admitted: true}}
	service := application.NewAdmission(application.AdmissionOptions{Now: now, VerdictPolicy: domain.VerdictPolicy{MinimumConfidence: 70, MinimumHypeQuality: 60, MaximumManipulationProbability: 35}, MaxEvidenceAge: 15 * time.Minute, MaxEntryPriceImpactBPS: 500, Repository: repository})
	result, err := service.Admit(context.Background(), validAdmissionInput(now))
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if !result.Admitted || repository.calls != 1 {
		t.Fatalf("result = %#v, calls = %d; want persisted admission", result, repository.calls)
	}
}

type recordingAdmissionRepository struct {
	calls  int
	result application.AdmissionResult
}

func (repository *recordingAdmissionRepository) Admit(_ context.Context, _ application.AdmissionInput) (application.AdmissionResult, error) {
	repository.calls++
	return repository.result, nil
}

func validAdmissionInput(now time.Time) application.AdmissionInput {
	return application.AdmissionInput{
		Evaluation: domain.CandidateEvaluation{Eligible: true, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: true, Observed: "$20000", Limit: ">=$20000"}}},
		Verdict:    domain.Verdict{Outcome: domain.VerdictBuy, Confidence: 70, HypeQualityScore: 60, ManipulationProbability: 35}, EvidenceObservedAt: now.Add(-15 * time.Minute),
		MintAddress: "candidate-mint", QuoteMint: "quote-mint", EntryInputAmount: 10_000_000, IdempotencyKey: "candidate-mint:2026-08-19T12:00:00Z",
		CandidateID: 1, NotionalMicros: 10_000_000,
		EntryQuote: domain.QuoteEvidence{InputMint: "quote-mint", OutputMint: "candidate-mint", InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 500, RoutePlan: []domain.RouteLeg{{AMMKey: "pool"}}},
	}
}
