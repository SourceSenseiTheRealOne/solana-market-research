package application

import (
	"context"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type AutomationActivityStore interface {
	Append(context.Context, domain.AutomationActivity) error
}

type AutomationActivityReader interface {
	List(context.Context) ([]domain.AutomationActivity, error)
}

type ReviewedCandidateStore interface {
	AppendReviewed(context.Context, domain.ReviewedCandidate) error
}

type ReviewedCandidatesReader interface {
	ListReviewed(context.Context) ([]domain.ReviewedCandidate, error)
}
