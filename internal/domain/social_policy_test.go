package domain_test

import (
	"testing"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestSocialPolicyEvaluatesEveryBoldMomentumBoundary(t *testing.T) {
	policy := domain.SocialPolicy{MinScore: 30, MinUniqueAuthors: 3, MinOriginalPosts: 2, MinExactMintMentions: 2, MaxWarningPosts: 1}
	valid := domain.SocialMetrics{Posts: 4, UniqueAuthors: 3, OriginalPosts: 2, Reposts: 1, Replies: 1, ExactMintMentions: 2, WarningPosts: 1}
	tests := []struct {
		name   string
		score  int
		mutate func(*domain.SocialMetrics)
	}{
		{name: "accepts exact boundaries", score: 30, mutate: func(*domain.SocialMetrics) {}},
		{name: "rejects low score", score: 29, mutate: func(*domain.SocialMetrics) {}},
		{name: "rejects too few authors", score: 30, mutate: func(m *domain.SocialMetrics) { m.UniqueAuthors = 2 }},
		{name: "rejects too few original posts", score: 30, mutate: func(m *domain.SocialMetrics) { m.OriginalPosts = 1; m.Reposts = 2 }},
		{name: "rejects too few mint mentions", score: 30, mutate: func(m *domain.SocialMetrics) { m.ExactMintMentions = 1 }},
		{name: "rejects too many warnings", score: 30, mutate: func(m *domain.SocialMetrics) { m.WarningPosts = 2 }},
		{name: "rejects impossible aggregates", score: 30, mutate: func(m *domain.SocialMetrics) { m.UniqueAuthors = 5 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics := valid
			test.mutate(&metrics)
			got := policy.Evaluate(metrics, test.score).Eligible
			if got != (test.name == "accepts exact boundaries") {
				t.Fatalf("eligible = %t for metrics %#v", got, metrics)
			}
		})
	}
}

func TestSocialPolicyRejectsInvalidConfiguration(t *testing.T) {
	valid := domain.SocialPolicy{MinScore: 30, MinUniqueAuthors: 3, MinOriginalPosts: 2, MinExactMintMentions: 2, MaxWarningPosts: 1}
	tests := []func(*domain.SocialPolicy){
		func(p *domain.SocialPolicy) { p.MinScore = 0 },
		func(p *domain.SocialPolicy) { p.MinScore = 101 },
		func(p *domain.SocialPolicy) { p.MinUniqueAuthors = 0 },
		func(p *domain.SocialPolicy) { p.MinOriginalPosts = 0 },
		func(p *domain.SocialPolicy) { p.MinExactMintMentions = 0 },
		func(p *domain.SocialPolicy) { p.MaxWarningPosts = -1 },
	}
	for index, mutate := range tests {
		policy := valid
		mutate(&policy)
		if err := policy.Validate(); err == nil {
			t.Fatalf("invalid social policy %d was accepted", index)
		}
	}
}

func TestVerdictPolicyAcceptsOnlyBuyAtEveryBoldMomentumBoundary(t *testing.T) {
	policy := domain.VerdictPolicy{MinimumConfidence: 70, MinimumHypeQuality: 60, MaximumManipulationProbability: 35}
	valid := domain.Verdict{Outcome: domain.VerdictBuy, Confidence: 70, HypeQualityScore: 60, ManipulationProbability: 35}
	tests := []struct {
		name   string
		mutate func(*domain.Verdict)
		want   bool
	}{
		{name: "accepts exact boundaries", mutate: func(*domain.Verdict) {}, want: true},
		{name: "rejects watch", mutate: func(v *domain.Verdict) { v.Outcome = domain.VerdictWatch }},
		{name: "rejects low confidence", mutate: func(v *domain.Verdict) { v.Confidence = 69 }},
		{name: "rejects low hype quality", mutate: func(v *domain.Verdict) { v.HypeQualityScore = 59 }},
		{name: "rejects high manipulation probability", mutate: func(v *domain.Verdict) { v.ManipulationProbability = 36 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verdict := valid
			test.mutate(&verdict)
			if got := policy.Accepts(verdict); got != test.want {
				t.Fatalf("Accepts(%#v) = %t, want %t", verdict, got, test.want)
			}
		})
	}
}

func TestVerdictPolicyRejectsInvalidConfiguration(t *testing.T) {
	valid := domain.VerdictPolicy{MinimumConfidence: 70, MinimumHypeQuality: 60, MaximumManipulationProbability: 35}
	tests := []func(*domain.VerdictPolicy){
		func(p *domain.VerdictPolicy) { p.MinimumConfidence = 0 },
		func(p *domain.VerdictPolicy) { p.MinimumConfidence = 101 },
		func(p *domain.VerdictPolicy) { p.MinimumHypeQuality = 0 },
		func(p *domain.VerdictPolicy) { p.MinimumHypeQuality = 101 },
		func(p *domain.VerdictPolicy) { p.MaximumManipulationProbability = -1 },
		func(p *domain.VerdictPolicy) { p.MaximumManipulationProbability = 101 },
	}
	for index, mutate := range tests {
		policy := valid
		mutate(&policy)
		if err := policy.Validate(); err == nil {
			t.Fatalf("invalid verdict policy %d was accepted", index)
		}
	}
}
