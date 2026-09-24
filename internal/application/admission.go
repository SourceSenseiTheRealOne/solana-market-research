package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type AdmissionInput struct {
	Evaluation         domain.CandidateEvaluation
	Verdict            domain.Verdict
	EvidenceObservedAt time.Time
	MintAddress        string
	QuoteMint          string
	EntryInputAmount   uint64
	EntryQuote         domain.QuoteEvidence
	IdempotencyKey     string
	CandidateID        int
	NotionalMicros     int64
}

type AdmissionResult struct {
	Admitted bool
}

type AdmissionRepository interface {
	Admit(context.Context, AdmissionInput) (AdmissionResult, error)
}

type AdmissionOptions struct {
	Now                    time.Time
	VerdictPolicy          domain.VerdictPolicy
	MaxEvidenceAge         time.Duration
	MaxEntryPriceImpactBPS int64
	Repository             AdmissionRepository
}

type Admission struct{ options AdmissionOptions }

func NewAdmission(options AdmissionOptions) *Admission { return &Admission{options: options} }

func (admission *Admission) Admit(ctx context.Context, input AdmissionInput) (AdmissionResult, error) {
	if admission == nil || admission.options.Repository == nil || admission.options.Now.IsZero() || admission.options.VerdictPolicy.Validate() != nil || admission.options.MaxEvidenceAge <= 0 || admission.options.MaxEntryPriceImpactBPS < 0 {
		return AdmissionResult{}, errors.New("admission is not completely configured")
	}
	if err := validateAdmissionInput(admission.options, input); err != nil {
		return AdmissionResult{}, err
	}
	result, err := admission.options.Repository.Admit(ctx, input)
	if err != nil {
		return AdmissionResult{}, fmt.Errorf("persist paper admission: %w", err)
	}
	return result, nil
}

func validateAdmissionInput(options AdmissionOptions, input AdmissionInput) error {
	if !input.Evaluation.Eligible || len(input.Evaluation.Rules) == 0 {
		return errors.New("candidate did not pass deterministic evaluation")
	}
	for _, rule := range input.Evaluation.Rules {
		if !rule.Passed {
			return fmt.Errorf("candidate failed deterministic rule %q", rule.Code)
		}
	}
	if !options.VerdictPolicy.Accepts(input.Verdict) {
		return errors.New("Hermes verdict does not meet admission policy")
	}
	if input.EvidenceObservedAt.IsZero() || input.EvidenceObservedAt.After(options.Now) || options.Now.Sub(input.EvidenceObservedAt) > options.MaxEvidenceAge {
		return errors.New("admission evidence is stale")
	}
	if strings.TrimSpace(input.MintAddress) == "" || strings.TrimSpace(input.QuoteMint) == "" || input.EntryInputAmount == 0 || strings.TrimSpace(input.IdempotencyKey) == "" || input.CandidateID <= 0 || input.NotionalMicros <= 0 {
		return errors.New("admission input is incomplete")
	}
	quote := input.EntryQuote
	if quote.InputMint != input.QuoteMint || quote.OutputMint != input.MintAddress || quote.InAmount != input.EntryInputAmount || quote.OutAmount == 0 || quote.PriceImpactBPS < 0 || quote.PriceImpactBPS > options.MaxEntryPriceImpactBPS || len(quote.RoutePlan) == 0 {
		return errors.New("second entry quote is not executable within admission limits")
	}
	for _, leg := range quote.RoutePlan {
		if strings.TrimSpace(leg.AMMKey) == "" {
			return errors.New("second entry quote is missing a route leg")
		}
	}
	return nil
}
