package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const socialEvidenceWindow = 15 * time.Minute

type SocialProvider interface {
	Fetch(context.Context, domain.SocialWindow) ([]domain.SocialPost, error)
}

type SocialBudget interface {
	Reserve(context.Context, time.Time, int) (bool, error)
}

type SocialSnapshotRepository interface {
	Save(context.Context, domain.DiscoveredPool, SocialAnalysis) error
}

type SocialAnalyzerOptions struct {
	Now                  time.Time
	Provider             SocialProvider
	Budget               SocialBudget
	Snapshots            SocialSnapshotRepository
	MaxDailyRequests     int
	EstimatedCostPerPost domain.USD
}

type SocialAnalysis struct {
	Window           domain.SocialWindow
	Posts            []domain.SocialPost
	Metrics          domain.SocialMetrics
	Score            int
	RequestCount     int
	EstimatedCostUSD domain.USD
}

type SocialAnalyzer struct{ options SocialAnalyzerOptions }

func NewSocialAnalyzer(options SocialAnalyzerOptions) *SocialAnalyzer {
	return &SocialAnalyzer{options: options}
}

func (analyzer *SocialAnalyzer) Analyze(ctx context.Context, pool domain.DiscoveredPool, evaluation domain.CandidateEvaluation) (SocialAnalysis, error) {
	if !evaluation.Eligible {
		return SocialAnalysis{}, nil
	}
	if analyzer == nil || analyzer.options.Provider == nil || analyzer.options.Budget == nil || analyzer.options.Snapshots == nil {
		return SocialAnalysis{}, errors.New("social analyzer is not configured")
	}
	if analyzer.options.Now.IsZero() || analyzer.options.MaxDailyRequests < 1 || analyzer.options.EstimatedCostPerPost.Micros < 0 {
		return SocialAnalysis{}, errors.New("social analyzer configuration is invalid")
	}
	reserved, err := analyzer.options.Budget.Reserve(ctx, analyzer.options.Now.UTC(), analyzer.options.MaxDailyRequests)
	if err != nil {
		return SocialAnalysis{}, fmt.Errorf("reserve social request budget: %w", err)
	}
	if !reserved {
		return SocialAnalysis{}, errors.New("social request budget exhausted")
	}
	window := domain.SocialWindow{MintAddress: pool.MintAddress, StartsAt: analyzer.options.Now.UTC().Add(-socialEvidenceWindow), EndsAt: analyzer.options.Now.UTC()}
	posts, err := analyzer.options.Provider.Fetch(ctx, window)
	if err != nil {
		return SocialAnalysis{}, fmt.Errorf("fetch social evidence: %w", err)
	}
	analysis := SocialAnalysis{Window: window, Posts: posts, Metrics: domain.AnalyzeSocial(posts, window), RequestCount: 1, EstimatedCostUSD: domain.USD{Micros: int64(len(posts)) * analyzer.options.EstimatedCostPerPost.Micros}}
	analysis.Score = domain.ScoreSocial(analysis.Metrics)
	if err := analyzer.options.Snapshots.Save(ctx, pool, analysis); err != nil {
		return SocialAnalysis{}, fmt.Errorf("persist social evidence: %w", err)
	}
	return analysis, nil
}
