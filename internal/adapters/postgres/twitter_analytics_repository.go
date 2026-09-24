package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"entgo.io/ent/dialect/sql"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/socialsnapshot"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type TwitterAnalyticsRepository struct {
	client *ent.Client
}

func NewTwitterAnalyticsRepository(client *ent.Client) (*TwitterAnalyticsRepository, error) {
	if client == nil {
		return nil, errors.New("Twitter analytics repository requires an Ent client")
	}
	return &TwitterAnalyticsRepository{client: client}, nil
}

func (repository *TwitterAnalyticsRepository) ListTwitterAnalytics(ctx context.Context) ([]domain.TwitterAnalytics, error) {
	if repository == nil || repository.client == nil {
		return nil, errors.New("Twitter analytics repository is not configured")
	}
	snapshots, err := repository.client.SocialSnapshot.Query().
		Order(socialsnapshot.ByObservedAt(sql.OrderDesc())).
		Limit(domain.MaxDashboardItems).
		WithCandidate().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Twitter analytics snapshots: %w", err)
	}
	analytics := make([]domain.TwitterAnalytics, 0, len(snapshots))
	for _, snapshot := range snapshots {
		candidate := snapshot.Edges.Candidate
		if candidate == nil {
			return nil, errors.New("Twitter analytics snapshot candidate is missing")
		}
		metrics, err := twitterMetrics(snapshot.SocialEvidence)
		if err != nil {
			return nil, err
		}
		item := domain.TwitterAnalytics{
			MintAddress:       candidate.MintAddress,
			SearchedAt:        snapshot.ObservedAt.UTC(),
			SearchCount:       1,
			Score:             snapshot.Score,
			Posts:             metrics.posts,
			UniqueAuthors:     metrics.uniqueAuthors,
			ExactMintMentions: metrics.exactMintMentions,
			WarningPosts:      metrics.warningPosts,
		}
		if err := item.Validate(); err != nil {
			return nil, errors.New("Twitter analytics snapshot is invalid")
		}
		analytics = append(analytics, item)
	}
	if err := domain.ValidateTwitterAnalytics(analytics); err != nil {
		return nil, errors.New("Twitter analytics list is invalid")
	}
	return analytics, nil
}

type twitterMetricValues struct {
	posts             int
	uniqueAuthors     int
	exactMintMentions int
	warningPosts      int
}

func twitterMetrics(evidence map[string]any) (twitterMetricValues, error) {
	metrics, ok := evidence["metrics"].(map[string]any)
	if !ok {
		return twitterMetricValues{}, errors.New("Twitter analytics metrics are missing")
	}
	posts, err := twitterMetric(metrics, "posts")
	if err != nil {
		return twitterMetricValues{}, err
	}
	uniqueAuthors, err := twitterMetric(metrics, "unique_authors")
	if err != nil {
		return twitterMetricValues{}, err
	}
	exactMintMentions, err := twitterMetric(metrics, "exact_mint_mentions")
	if err != nil {
		return twitterMetricValues{}, err
	}
	warningPosts, err := twitterMetric(metrics, "warning_posts")
	if err != nil {
		return twitterMetricValues{}, err
	}
	return twitterMetricValues{posts: posts, uniqueAuthors: uniqueAuthors, exactMintMentions: exactMintMentions, warningPosts: warningPosts}, nil
}

func twitterMetric(metrics map[string]any, key string) (int, error) {
	value, ok := metrics[key].(float64)
	if !ok || value < 0 || math.Trunc(value) != value || value > float64(math.MaxInt) {
		return 0, errors.New("Twitter analytics metric is invalid")
	}
	return int(value), nil
}
