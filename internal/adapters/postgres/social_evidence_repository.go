package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/apiusage"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const twitterAPIIOProvider = "twitterapiio"

type SocialEvidenceRepository struct {
	client *ent.Client
	mu     sync.Mutex
}

func NewSocialEvidenceRepository(client *ent.Client) (*SocialEvidenceRepository, error) {
	if client == nil {
		return nil, errors.New("social evidence repository requires an Ent client")
	}
	return &SocialEvidenceRepository{client: client}, nil
}

func (repository *SocialEvidenceRepository) Reserve(ctx context.Context, observedAt time.Time, maximum int) (bool, error) {
	if observedAt.IsZero() || maximum < 1 {
		return false, errors.New("social request budget requires time and positive maximum")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	utcDate := utcDay(observedAt)
	usage, err := repository.client.APIUsage.Query().Where(apiusage.ProviderEQ(twitterAPIIOProvider), apiusage.UtcDateEQ(utcDate)).Only(ctx)
	if ent.IsNotFound(err) {
		if err := repository.client.APIUsage.Create().SetProvider(twitterAPIIOProvider).SetUtcDate(utcDate).SetRequestCount(1).SetFailureCount(0).Exec(ctx); err != nil {
			return false, fmt.Errorf("create social usage ledger: %w", err)
		}
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("load social usage ledger: %w", err)
	}
	if usage.RequestCount >= maximum {
		return false, nil
	}
	if err := repository.client.APIUsage.UpdateOne(usage).AddRequestCount(1).Exec(ctx); err != nil {
		return false, fmt.Errorf("reserve social usage: %w", err)
	}
	return true, nil
}

func (repository *SocialEvidenceRepository) Save(ctx context.Context, pool domain.DiscoveredPool, analysis application.SocialAnalysis) error {
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("validate social snapshot candidate: %w", err)
	}
	if analysis.Window.EndsAt.IsZero() || analysis.Window.StartsAt.IsZero() || analysis.Window.MintAddress != pool.MintAddress {
		return errors.New("social snapshot has an invalid evidence window")
	}
	candidateRecord, err := repository.client.Candidate.Query().Where(candidate.NetworkEQ(pool.Network), candidate.MintAddressEQ(pool.MintAddress), candidate.PoolAddressEQ(pool.PoolAddress)).Only(ctx)
	if err != nil {
		return fmt.Errorf("load social snapshot candidate: %w", err)
	}
	if err := repository.client.SocialSnapshot.Create().SetCandidate(candidateRecord).SetScore(analysis.Score).SetObservedAt(analysis.Window.EndsAt.UTC()).SetSocialEvidence(socialEvidence(analysis)).Exec(ctx); err != nil {
		return fmt.Errorf("save social snapshot: %w", err)
	}
	return nil
}

func utcDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func socialEvidence(analysis application.SocialAnalysis) map[string]any {
	posts := make([]map[string]any, 0, len(analysis.Posts))
	for _, post := range analysis.Posts {
		entry := map[string]any{"id": post.ID, "author_id": post.AuthorID, "created_at": post.CreatedAt.UTC().Format(time.RFC3339Nano), "excerpt": post.Text, "likes": post.Likes, "replies": post.Replies, "reposts": post.RepostCount, "quotes": post.QuoteCount, "followers": post.Followers, "is_reply": post.IsReply, "is_repost": post.IsRepost}
		if !post.AuthorCreatedAt.IsZero() {
			entry["author_created_at"] = post.AuthorCreatedAt.UTC().Format(time.RFC3339Nano)
		}
		posts = append(posts, entry)
	}
	metrics := analysis.Metrics
	return map[string]any{"window_starts_at": analysis.Window.StartsAt.UTC().Format(time.RFC3339Nano), "window_ends_at": analysis.Window.EndsAt.UTC().Format(time.RFC3339Nano), "request_count": analysis.RequestCount, "estimated_cost_usd_micros": analysis.EstimatedCostUSD.Micros, "posts": posts, "post_text_hashes": metrics.PostTextHashes, "metrics": map[string]any{"posts": metrics.Posts, "unique_authors": metrics.UniqueAuthors, "exact_mint_mentions": metrics.ExactMintMentions, "original_posts": metrics.OriginalPosts, "reposts": metrics.Reposts, "replies": metrics.Replies, "posts_per_minute_bps": metrics.PostsPerMinuteBPS, "exact_mint_mention_bps": metrics.ExactMintMentionBPS, "repeated_text_hashes": metrics.RepeatedTextHashes, "follower_adjusted_engagement_bps": metrics.FollowerAdjustedEngagementBPS, "warning_posts": metrics.WarningPosts, "simultaneous_posts": metrics.SimultaneousPosts}}
}
