package application

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

const twoSourcePoolCap = 5

// TwoSourcePoolDiscovery returns the bounded union of independent fresh-pool
// feeds. An exact cross-source pool identity is corroborating evidence, not a
// prerequisite for consideration.
type TwoSourcePoolDiscovery struct {
	dexScreener   ports.PoolDiscovery
	geckoTerminal ports.PoolDiscovery
}

func NewTwoSourcePoolDiscovery(dexScreener, geckoTerminal ports.PoolDiscovery) *TwoSourcePoolDiscovery {
	return &TwoSourcePoolDiscovery{dexScreener: dexScreener, geckoTerminal: geckoTerminal}
}

func (discovery *TwoSourcePoolDiscovery) FetchNewPools(ctx context.Context, page int) (ports.PoolPage, error) {
	if discovery == nil || discovery.dexScreener == nil || discovery.geckoTerminal == nil || page < 1 {
		return ports.PoolPage{}, errors.New("two-source pool discovery is not completely configured")
	}
	dexPage, err := discovery.dexScreener.FetchNewPools(ctx, page)
	if err != nil {
		return ports.PoolPage{}, fmt.Errorf("fetch DexScreener new pools: %w", err)
	}
	geckoPage, err := discovery.geckoTerminal.FetchNewPools(ctx, page)
	if err != nil {
		return ports.PoolPage{}, fmt.Errorf("fetch GeckoTerminal new pools: %w", err)
	}

	pools, err := mergeNewestPools(dexPage.Pools, geckoPage.Pools)
	if err != nil {
		return ports.PoolPage{}, err
	}
	return ports.PoolPage{Pools: pools, NextPage: geckoPage.NextPage}, nil
}

func mergeNewestPools(sources ...[]domain.DiscoveredPool) ([]domain.DiscoveredPool, error) {
	unique := make(map[string]domain.DiscoveredPool, twoSourcePoolCap)
	for _, pools := range sources {
		for _, pool := range pools {
			if err := pool.Validate(); err != nil {
				return nil, fmt.Errorf("validate fresh source pool: %w", err)
			}
			identity := pool.Identity()
			if _, exists := unique[identity]; !exists {
				unique[identity] = pool
			}
		}
	}
	merged := make([]domain.DiscoveredPool, 0, len(unique))
	for _, pool := range unique {
		merged = append(merged, pool)
	}
	sort.Slice(merged, func(left, right int) bool {
		if merged[left].CreatedAt.Equal(merged[right].CreatedAt) {
			return merged[left].Identity() < merged[right].Identity()
		}
		return merged[left].CreatedAt.After(merged[right].CreatedAt)
	})
	if len(merged) > twoSourcePoolCap {
		merged = merged[:twoSourcePoolCap]
	}
	return merged, nil
}

var _ ports.PoolDiscovery = (*TwoSourcePoolDiscovery)(nil)
