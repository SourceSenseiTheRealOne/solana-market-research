package ports

import (
	"context"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type PoolPage struct {
	Pools    []domain.DiscoveredPool
	NextPage int
}

// PoolDiscovery returns public pools ordered newest-first by the provider.
type PoolDiscovery interface {
	FetchNewPools(ctx context.Context, page int) (PoolPage, error)
}

// TokenHintDiscovery returns bounded public token hints, not executable trade data.
type TokenHintDiscovery interface {
	FetchLatestSolanaTokenHints(ctx context.Context) ([]domain.TokenHint, error)
}

type QuoteProvider interface {
	Quote(ctx context.Context, inMint, outMint string, amount uint64) (domain.ExecutableQuote, error)
	Health(ctx context.Context) error
}

type TokenInspector interface {
	Inspect(ctx context.Context, mint string) (domain.TokenRiskSnapshot, error)
}
