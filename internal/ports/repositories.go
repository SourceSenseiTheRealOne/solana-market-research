package ports

import (
	"context"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type CandidateRepository interface {
	InsertDiscovered(ctx context.Context, pools []domain.DiscoveredPool) (int, error)
	LoadWatermark(ctx context.Context, source string) (domain.Watermark, error)
	SaveWatermark(ctx context.Context, source string, mark domain.Watermark) error
	RecordCoverageGap(ctx context.Context, source string, page int, reason string) error
}
