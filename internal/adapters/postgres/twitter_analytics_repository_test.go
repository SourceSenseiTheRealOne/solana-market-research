package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestTwitterAnalyticsRepositoryReturnsNewestSafeAggregateSnapshots(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	repository, err := postgres.NewTwitterAnalyticsRepository(client)
	if err != nil {
		t.Fatalf("NewTwitterAnalyticsRepository() error = %v", err)
	}

	base := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	for index := 0; index < domain.MaxDashboardItems+1; index++ {
		mint := fmt.Sprintf("twitter-analytics-mint-%02d", index)
		candidate, err := client.Candidate.Create().
			SetNetwork("solana").
			SetMintAddress(mint).
			SetPoolAddress(fmt.Sprintf("twitter-analytics-pool-%02d", index)).
			SetDiscoveredAt(base.Add(time.Duration(index) * time.Minute)).
			Save(context.Background())
		if err != nil {
			t.Fatalf("create candidate %d: %v", index, err)
		}
		evidence := map[string]any{
			"posts": []any{map[string]any{"text": "raw-tweet-sentinel", "id": "raw-post-id"}},
			"metrics": map[string]any{
				"posts":               8,
				"unique_authors":      6,
				"exact_mint_mentions": 5,
				"warning_posts":       1,
			},
		}
		if err := client.SocialSnapshot.Create().
			SetCandidate(candidate).
			SetScore(72).
			SetObservedAt(base.Add(time.Duration(index) * time.Minute)).
			SetSocialEvidence(evidence).
			Exec(context.Background()); err != nil {
			t.Fatalf("create social snapshot %d: %v", index, err)
		}
	}

	got, err := repository.ListTwitterAnalytics(context.Background())
	if err != nil {
		t.Fatalf("ListTwitterAnalytics() error = %v", err)
	}
	if got, want := len(got), domain.MaxDashboardItems; got != want {
		t.Fatalf("analytics length = %d, want %d", got, want)
	}
	first := got[0]
	if first.MintAddress != "twitter-analytics-mint-10" || !first.SearchedAt.Equal(base.Add(10*time.Minute)) {
		t.Fatalf("first analytics = %#v, want newest stored snapshot", first)
	}
	if first.SearchCount != 1 || first.Score != 72 || first.Posts != 8 || first.UniqueAuthors != 6 || first.ExactMintMentions != 5 || first.WarningPosts != 1 {
		t.Fatalf("first analytics aggregates = %#v, want stored public aggregates", first)
	}
	if strings.Contains(fmt.Sprintf("%#v", got), "raw-tweet-sentinel") || strings.Contains(fmt.Sprintf("%#v", got), "raw-post-id") {
		t.Fatal("ListTwitterAnalytics() leaked raw Twitter evidence")
	}
}

func TestTwitterAnalyticsRepositoryRejectsMalformedMetricsWithoutRawEvidence(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	repository, err := postgres.NewTwitterAnalyticsRepository(client)
	if err != nil {
		t.Fatalf("NewTwitterAnalyticsRepository() error = %v", err)
	}
	base := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	candidate, err := client.Candidate.Create().
		SetNetwork("solana").
		SetMintAddress("twitter-analytics-malformed-mint").
		SetPoolAddress("twitter-analytics-malformed-pool").
		SetDiscoveredAt(base).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	if err := client.SocialSnapshot.Create().
		SetCandidate(candidate).
		SetScore(20).
		SetObservedAt(base).
		SetSocialEvidence(map[string]any{"metrics": map[string]any{"posts": "raw-twitter-sentinel"}}).
		Exec(context.Background()); err != nil {
		t.Fatalf("create malformed social snapshot: %v", err)
	}

	_, err = repository.ListTwitterAnalytics(context.Background())
	if err == nil {
		t.Fatal("ListTwitterAnalytics() accepted malformed aggregate metrics")
	}
	if strings.Contains(err.Error(), "raw-twitter-sentinel") {
		t.Fatalf("ListTwitterAnalytics() leaked malformed raw evidence in error: %v", err)
	}
}
