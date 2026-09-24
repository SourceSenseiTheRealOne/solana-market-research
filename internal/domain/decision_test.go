package domain_test

import (
	"testing"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestVerdictValidationAcceptsBoundedKnownEvidence(t *testing.T) {
	verdict := domain.Verdict{Outcome: domain.VerdictBuy, Confidence: 80, HypeQualityScore: 75, ManipulationProbability: 20, Reasons: []string{"organic author diversity"}, RiskFlags: []string{"new pool"}, InvalidationConditions: []string{"liquidity falls"}, EvidencePostIDs: []string{"post-1"}}
	if err := verdict.Validate(map[string]struct{}{"post-1": {}}); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestVerdictValidationRejectsUnsafeOrUnverifiableFields(t *testing.T) {
	valid := domain.Verdict{Outcome: domain.VerdictWatch, Confidence: 50, HypeQualityScore: 50, ManipulationProbability: 50}
	tests := []struct {
		name   string
		mutate func(*domain.Verdict)
	}{
		{"unknown outcome", func(verdict *domain.Verdict) { verdict.Outcome = "EXECUTE" }},
		{"confidence above range", func(verdict *domain.Verdict) { verdict.Confidence = 101 }},
		{"unknown evidence post", func(verdict *domain.Verdict) { verdict.EvidencePostIDs = []string{"missing"} }},
		{"oversized reasons", func(verdict *domain.Verdict) { verdict.Reasons = make([]string, 9) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verdict := valid
			test.mutate(&verdict)
			if err := verdict.Validate(map[string]struct{}{}); err == nil {
				t.Fatal("Validate() accepted unsafe or unverifiable verdict")
			}
		})
	}
}
