package domain_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestAnalyzeSocialComputesDeterministicQualityAndManipulationMetrics(t *testing.T) {
	windowStart := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	posts := []domain.SocialPost{
		{ID: "one", AuthorID: "alice", CreatedAt: windowStart.Add(time.Minute), Text: "MintX is moving", Followers: 100, Likes: 10},
		{ID: "two", AuthorID: "bob", CreatedAt: windowStart.Add(2 * time.Minute), Text: "MintX is moving", Followers: 10, Likes: 2, IsRepost: true},
		{ID: "three", AuthorID: "alice", CreatedAt: windowStart.Add(2 * time.Minute), Text: "scam rug MintX", Followers: 100, Replies: 3, IsReply: true},
	}

	metrics := domain.AnalyzeSocial(posts, domain.SocialWindow{MintAddress: "MintX", StartsAt: windowStart, EndsAt: windowStart.Add(15 * time.Minute)})
	if got, want := metrics.UniqueAuthors, 2; got != want {
		t.Fatalf("unique authors = %d, want %d", got, want)
	}
	if got, want := metrics.ExactMintMentions, 3; got != want {
		t.Fatalf("exact mint mentions = %d, want %d", got, want)
	}
	if got, want := metrics.Reposts, 1; got != want {
		t.Fatalf("reposts = %d, want %d", got, want)
	}
	if got, want := metrics.Replies, 1; got != want {
		t.Fatalf("replies = %d, want %d", got, want)
	}
	if got, want := metrics.RepeatedTextHashes, 1; got != want {
		t.Fatalf("repeated text hashes = %d, want %d", got, want)
	}
	if got, want := len(metrics.PostTextHashes), 3; got != want {
		t.Fatalf("post text hashes = %d, want %d", got, want)
	}
	if metrics.PostTextHashes["one"] != metrics.PostTextHashes["two"] {
		t.Fatalf("equivalent normalized texts received different hashes: %#v", metrics.PostTextHashes)
	}
	if got, want := metrics.WarningPosts, 1; got != want {
		t.Fatalf("warning posts = %d, want %d", got, want)
	}
	if got, want := metrics.SimultaneousPosts, 2; got != want {
		t.Fatalf("simultaneous posts = %d, want %d", got, want)
	}
	if metrics.FollowerAdjustedEngagementBPS <= 0 {
		t.Fatalf("follower adjusted engagement bps = %d, want positive", metrics.FollowerAdjustedEngagementBPS)
	}
	if got, want := domain.ScoreSocial(metrics), 2; got != want {
		t.Fatalf("social score = %d, want %d", got, want)
	}
}

func TestAnalyzeSocialReturnsZeroMetricsForEmptyEvidence(t *testing.T) {
	windowStart := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	metrics := domain.AnalyzeSocial(nil, domain.SocialWindow{MintAddress: "MintX", StartsAt: windowStart, EndsAt: windowStart.Add(15 * time.Minute)})
	if metrics.Posts != 0 || metrics.UniqueAuthors != 0 || metrics.WarningPosts != 0 || len(metrics.PostTextHashes) != 0 {
		t.Fatalf("empty evidence metrics = %#v, want zero metrics", metrics)
	}
	if got, want := domain.ScoreSocial(metrics), 0; got != want {
		t.Fatalf("empty evidence score = %d, want %d", got, want)
	}
}
