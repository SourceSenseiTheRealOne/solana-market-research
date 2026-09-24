package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestSocialAnalyzerReservesBudgetAndPersistsEligibleCandidateEvidence(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 15, 0, 0, time.UTC)
	provider := &socialProvider{posts: []domain.SocialPost{{ID: "post", AuthorID: "author", CreatedAt: now.Add(-time.Minute), Text: "mint", Followers: 100, Likes: 10}}}
	budget := &socialBudget{}
	repository := &socialRepository{}
	analyzer := application.NewSocialAnalyzer(application.SocialAnalyzerOptions{Now: now, Provider: provider, Budget: budget, Snapshots: repository, MaxDailyRequests: 30, EstimatedCostPerPost: domain.USD{Micros: 150}})
	pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: now.Add(-5 * time.Minute)}

	analysis, err := analyzer.Analyze(context.Background(), pool, domain.CandidateEvaluation{Eligible: true})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if provider.calls != 1 || budget.calls != 1 || repository.calls != 1 {
		t.Fatalf("calls provider/budget/repository = %d/%d/%d, want 1/1/1", provider.calls, budget.calls, repository.calls)
	}
	if analysis.RequestCount != 1 || analysis.EstimatedCostUSD.Micros != 150 || analysis.Score <= 0 {
		t.Fatalf("analysis = %#v, want one request, cost 150 micros, positive score", analysis)
	}
	if repository.analysis.Metrics.Posts != 1 {
		t.Fatalf("persisted metrics = %#v, want one post", repository.analysis.Metrics)
	}
}

func TestSocialAnalyzerSkipsIneligibleCandidateWithoutConsumingBudget(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 15, 0, 0, time.UTC)
	provider := &socialProvider{}
	budget := &socialBudget{}
	repository := &socialRepository{}
	analyzer := application.NewSocialAnalyzer(application.SocialAnalyzerOptions{Now: now, Provider: provider, Budget: budget, Snapshots: repository, MaxDailyRequests: 30, EstimatedCostPerPost: domain.USD{Micros: 150}})
	pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: now.Add(-5 * time.Minute)}

	analysis, err := analyzer.Analyze(context.Background(), pool, domain.CandidateEvaluation{Eligible: false})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if analysis.RequestCount != 0 || analysis.Score != 0 || len(analysis.Posts) != 0 {
		t.Fatalf("analysis = %#v, want zero result", analysis)
	}
	if provider.calls != 0 || budget.calls != 0 || repository.calls != 0 {
		t.Fatalf("calls provider/budget/repository = %d/%d/%d, want 0/0/0", provider.calls, budget.calls, repository.calls)
	}
}

type socialProvider struct {
	posts []domain.SocialPost
	calls int
}

func (p *socialProvider) Fetch(context.Context, domain.SocialWindow) ([]domain.SocialPost, error) {
	p.calls++
	return p.posts, nil
}

type socialBudget struct{ calls int }

func (b *socialBudget) Reserve(context.Context, time.Time, int) (bool, error) {
	b.calls++
	return true, nil
}

type socialRepository struct {
	calls    int
	analysis application.SocialAnalysis
}

func (r *socialRepository) Save(_ context.Context, _ domain.DiscoveredPool, analysis application.SocialAnalysis) error {
	r.calls++
	r.analysis = analysis
	return nil
}
