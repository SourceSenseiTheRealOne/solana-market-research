package domain_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestAutomationActivityValidatesSafeLifecycleRecords(t *testing.T) {
	occurredAt := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	completed := domain.AutomationActivity{OccurredAt: occurredAt, Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeCompleted}
	failed := domain.AutomationActivity{OccurredAt: occurredAt.Add(-time.Minute), Job: domain.AutomationJobMonitor, Outcome: domain.AutomationOutcomeFailed, Category: "provider_rate_limited", Stage: "discovery"}

	if err := completed.Validate(); err != nil {
		t.Fatalf("completed activity Validate() error = %v", err)
	}
	if err := failed.Validate(); err != nil {
		t.Fatalf("failed activity Validate() error = %v", err)
	}
	if err := domain.ValidateDashboardActivity([]domain.AutomationActivity{completed, failed}); err != nil {
		t.Fatalf("ValidateDashboardActivity() error = %v", err)
	}
}

func TestAutomationActivityRejectsUnsafeOrInvalidLifecycleRecords(t *testing.T) {
	occurredAt := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	valid := domain.AutomationActivity{OccurredAt: occurredAt, Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeCompleted}

	cases := []domain.AutomationActivity{
		{OccurredAt: occurredAt, Job: "UNKNOWN", Outcome: domain.AutomationOutcomeCompleted},
		{OccurredAt: occurredAt, Job: domain.AutomationJobScan, Outcome: "UNKNOWN"},
		{Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeCompleted},
		{OccurredAt: occurredAt, Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeFailed},
		{OccurredAt: occurredAt, Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeFailed, Category: "provider_rate_limited", Stage: "https://provider.invalid/?api-key=must-not-pass"},
		{OccurredAt: occurredAt, Job: domain.AutomationJobScan, Outcome: domain.AutomationOutcomeCompleted, Category: "provider_rate_limited", Stage: "discovery"},
	}
	for _, activity := range cases {
		if err := activity.Validate(); err == nil {
			t.Fatalf("Validate() accepted unsafe activity %#v", activity)
		}
	}

	oversized := make([]domain.AutomationActivity, domain.MaxDashboardItems+1)
	for index := range oversized {
		oversized[index] = valid
		oversized[index].OccurredAt = occurredAt.Add(-time.Duration(index) * time.Minute)
	}
	if err := domain.ValidateDashboardActivity(oversized); err == nil {
		t.Fatal("ValidateDashboardActivity() accepted more than the fixed dashboard limit")
	}
	if err := domain.ValidateDashboardActivity([]domain.AutomationActivity{oversized[1], oversized[0]}); err == nil {
		t.Fatal("ValidateDashboardActivity() accepted non-descending activity timestamps")
	}
}

func TestReviewedCandidateValidatesOnlyPublicSummaryFields(t *testing.T) {
	checkedAt := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	candidate := domain.ReviewedCandidate{MintAddress: "public-token-mint", CheckedAt: checkedAt, Outcome: domain.ReviewedCandidateNotTraded, Reason: domain.ReviewReasonDeterministicLiquidity}
	if err := candidate.Validate(); err != nil {
		t.Fatalf("ReviewedCandidate.Validate() error = %v", err)
	}
	if err := (domain.ReviewedCandidate{}).Validate(); err == nil {
		t.Fatal("ReviewedCandidate.Validate() accepted missing public summary fields")
	}
	unsafe := candidate
	unsafe.Reason = "https://provider.invalid/raw-response"
	if err := unsafe.Validate(); err == nil {
		t.Fatal("ReviewedCandidate.Validate() accepted an unsafe rejection reason")
	}

	marketUnavailable := candidate
	marketUnavailable.Reason = "market_evidence_unavailable"
	if err := marketUnavailable.Validate(); err != nil {
		t.Fatalf("ReviewedCandidate.Validate() error = %v, want safe market-evidence reason", err)
	}

	socialUnavailable := candidate
	socialUnavailable.Reason = "social_evidence_unavailable"
	if err := socialUnavailable.Validate(); err != nil {
		t.Fatalf("ReviewedCandidate.Validate() error = %v, want safe social-evidence reason", err)
	}

	for _, reason := range []domain.ReviewedCandidateReason{
		domain.ReviewReasonDeterministicBuyShare,
		domain.ReviewReasonDeterministicTurnover,
		domain.ReviewReasonDeterministicPriceChange,
		domain.ReviewReasonDeterministicSocialQuality,
		domain.ReviewReasonHermesVerdictThreshold,
	} {
		candidate.Reason = reason
		if err := candidate.Validate(); err != nil {
			t.Fatalf("ReviewedCandidate.Validate() rejected fixed reason %q: %v", reason, err)
		}
	}
}

func TestTwitterAnalyticsValidatesBoundedPublicAggregates(t *testing.T) {
	searchedAt := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	analytics := domain.TwitterAnalytics{
		MintAddress:       "public-token-mint",
		SearchedAt:        searchedAt,
		SearchCount:       1,
		Score:             72,
		Posts:             8,
		UniqueAuthors:     6,
		ExactMintMentions: 5,
		WarningPosts:      1,
	}
	if err := analytics.Validate(); err != nil {
		t.Fatalf("TwitterAnalytics.Validate() error = %v", err)
	}
	if err := domain.ValidateTwitterAnalytics([]domain.TwitterAnalytics{analytics}); err != nil {
		t.Fatalf("ValidateTwitterAnalytics() error = %v", err)
	}

	unsafe := analytics
	unsafe.Score = 101
	if err := unsafe.Validate(); err == nil {
		t.Fatal("TwitterAnalytics.Validate() accepted an out-of-range score")
	}
	unsafe = analytics
	unsafe.WarningPosts = -1
	if err := unsafe.Validate(); err == nil {
		t.Fatal("TwitterAnalytics.Validate() accepted a negative aggregate")
	}
	if err := domain.ValidateTwitterAnalytics(make([]domain.TwitterAnalytics, domain.MaxDashboardItems+1)); err == nil {
		t.Fatal("ValidateTwitterAnalytics() accepted more than the fixed dashboard limit")
	}
}
