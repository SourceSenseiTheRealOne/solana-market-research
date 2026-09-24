package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestDashboardActivityRepositoryRetainsBoundedSafeProjections(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	repository, err := postgres.NewDashboardActivityRepository(client)
	if err != nil {
		t.Fatalf("NewDashboardActivityRepository() error = %v", err)
	}
	base := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	for index := 0; index < domain.MaxDashboardItems+1; index++ {
		if err := repository.Append(context.Background(), domain.AutomationActivity{OccurredAt: base.Add(time.Duration(index) * time.Minute), Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeCompleted}); err != nil {
			t.Fatalf("Append() activity %d error = %v", index, err)
		}
		if err := repository.AppendReviewed(context.Background(), domain.ReviewedCandidate{MintAddress: "reviewed-mint-" + string(rune('a'+index)), CheckedAt: base.Add(time.Duration(index) * time.Minute), Outcome: domain.ReviewedCandidateNotTraded, Reason: domain.ReviewReasonDeterministicLiquidity}); err != nil {
			t.Fatalf("AppendReviewed() candidate %d error = %v", index, err)
		}
	}

	activities, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if got, want := len(activities), domain.MaxDashboardItems; got != want {
		t.Fatalf("activities length = %d, want %d", got, want)
	}
	if !activities[0].OccurredAt.Equal(base.Add(10*time.Minute)) || activities[0].Job != domain.AutomationJobScan || activities[0].Outcome != domain.AutomationOutcomeCompleted {
		t.Fatalf("newest activity = %#v, want public newest completed scan", activities[0])
	}

	reviewed, err := repository.ListReviewed(context.Background())
	if err != nil {
		t.Fatalf("ListReviewed() error = %v", err)
	}
	if got, want := len(reviewed), domain.MaxDashboardItems; got != want {
		t.Fatalf("reviewed length = %d, want %d", got, want)
	}
	if reviewed[0].MintAddress != "reviewed-mint-k" || !reviewed[0].CheckedAt.Equal(base.Add(10*time.Minute)) || reviewed[0].Outcome != domain.ReviewedCandidateNotTraded || reviewed[0].Reason != domain.ReviewReasonDeterministicLiquidity {
		t.Fatalf("newest reviewed candidate = %#v, want public newest non-admission", reviewed[0])
	}
}
