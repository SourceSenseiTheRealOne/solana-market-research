package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

const (
	maxHintMatchedHints = 20
	maxHintMatchedPools = 5
)

// HintMatchedPoolDiscovery fetches bounded public DexScreener hints and
// GeckoTerminal fresh pools independently. DexScreener-only pool resolution is
// added by the caller-facing pool source; GeckoTerminal pools are never
// discarded solely because DexScreener did not report the mint.
type HintMatchedPoolDiscovery struct {
	hints ports.TokenHintDiscovery
	pools ports.PoolDiscovery
}

func NewHintMatchedPoolDiscovery(hints ports.TokenHintDiscovery, pools ports.PoolDiscovery) *HintMatchedPoolDiscovery {
	return &HintMatchedPoolDiscovery{hints: hints, pools: pools}
}

func (discovery *HintMatchedPoolDiscovery) FetchNewPools(ctx context.Context, page int) (ports.PoolPage, error) {
	if discovery == nil || discovery.hints == nil || discovery.pools == nil || page < 1 {
		return ports.PoolPage{}, errors.New("hint-matched pool discovery is not completely configured")
	}
	hints, err := discovery.hints.FetchLatestSolanaTokenHints(ctx)
	if err != nil {
		return ports.PoolPage{}, fmt.Errorf("fetch DexScreener token hints: %w", err)
	}

	for index, hint := range hints {
		if index == maxHintMatchedHints {
			break
		}
		if err := hint.Validate(); err != nil {
			return ports.PoolPage{}, fmt.Errorf("validate DexScreener token hint: %w", err)
		}
	}

	poolPage, err := discovery.pools.FetchNewPools(ctx, page)
	if err != nil {
		return ports.PoolPage{}, fmt.Errorf("fetch GeckoTerminal new pools: %w", err)
	}
	matched := make([]domain.DiscoveredPool, 0, min(len(poolPage.Pools), maxHintMatchedPools))
	for _, pool := range poolPage.Pools {
		if len(matched) == maxHintMatchedPools {
			break
		}
		if err := pool.Validate(); err != nil {
			return ports.PoolPage{}, fmt.Errorf("validate GeckoTerminal pool: %w", err)
		}
		matched = append(matched, pool)
	}
	return ports.PoolPage{Pools: matched, NextPage: poolPage.NextPage}, nil
}

var _ ports.PoolDiscovery = (*HintMatchedPoolDiscovery)(nil)
