package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

type DiscoveryOptions struct {
	Source   string
	MaxPages int
}

type DiscoveryResult struct {
	Inserted    int
	Overlapped  bool
	CoverageGap bool
	Pools       []domain.DiscoveredPool
}

type Discovery struct {
	pools      ports.PoolDiscovery
	candidates ports.CandidateRepository
	options    DiscoveryOptions
}

func NewDiscovery(pools ports.PoolDiscovery, candidates ports.CandidateRepository, options DiscoveryOptions) *Discovery {
	return &Discovery{pools: pools, candidates: candidates, options: options}
}

func (discovery *Discovery) Run(ctx context.Context) (DiscoveryResult, error) {
	if discovery.pools == nil || discovery.candidates == nil {
		return DiscoveryResult{}, errors.New("discovery requires pool provider and candidate repository")
	}
	if discovery.options.Source == "" || discovery.options.MaxPages < 1 {
		return DiscoveryResult{}, errors.New("discovery requires source and positive page limit")
	}

	watermark, err := discovery.candidates.LoadWatermark(ctx, discovery.options.Source)
	if err != nil {
		return DiscoveryResult{}, fmt.Errorf("load discovery watermark: %w", err)
	}

	result := DiscoveryResult{}
	seen := make(map[string]struct{})
	pageNumber := 1
	lastFetchedPage := 0
	var newest domain.DiscoveredPool

	for pagesFetched := 0; pagesFetched < discovery.options.MaxPages; pagesFetched++ {
		page, err := discovery.pools.FetchNewPools(ctx, pageNumber)
		if err != nil {
			return result, fmt.Errorf("fetch discovery page %d: %w", pageNumber, err)
		}
		lastFetchedPage = pageNumber

		unseen, overlap, currentNewest, err := normalizePage(page.Pools, watermark, seen)
		if err != nil {
			return result, err
		}
		if !currentNewest.CreatedAt.IsZero() && newerThan(currentNewest, newest) {
			newest = currentNewest
		}
		if len(unseen) > 0 {
			inserted, err := discovery.candidates.InsertDiscovered(ctx, unseen)
			if err != nil {
				return result, fmt.Errorf("persist discovery page %d: %w", pageNumber, err)
			}
			result.Inserted += inserted
			result.Pools = append(result.Pools, unseen...)
		}

		if overlap {
			result.Overlapped = true
			return result, discovery.saveWatermark(ctx, newest)
		}
		if page.NextPage == 0 {
			if watermark.IsZero() {
				return result, discovery.saveWatermark(ctx, newest)
			}
			return discovery.coverageGap(ctx, result, pageNumber, "provider ended before previous watermark")
		}
		if page.NextPage == pageNumber {
			return discovery.coverageGap(ctx, result, pageNumber, "provider repeated page")
		}
		pageNumber = page.NextPage
	}
	return discovery.coverageGap(ctx, result, lastFetchedPage, "page budget exhausted before previous watermark")
}

func (discovery *Discovery) saveWatermark(ctx context.Context, watermark domain.DiscoveredPool) error {
	if watermark.CreatedAt.IsZero() {
		return nil
	}
	mark := domain.Watermark{CreatedAt: watermark.CreatedAt, PoolAddress: watermark.PoolAddress}
	if err := discovery.candidates.SaveWatermark(ctx, discovery.options.Source, mark); err != nil {
		return fmt.Errorf("save discovery watermark: %w", err)
	}
	return nil
}

func (discovery *Discovery) coverageGap(ctx context.Context, result DiscoveryResult, page int, reason string) (DiscoveryResult, error) {
	result.CoverageGap = true
	if err := discovery.candidates.RecordCoverageGap(ctx, discovery.options.Source, page, reason); err != nil {
		return result, fmt.Errorf("record discovery coverage gap: %w", err)
	}
	return result, nil
}

func normalizePage(pools []domain.DiscoveredPool, watermark domain.Watermark, seen map[string]struct{}) ([]domain.DiscoveredPool, bool, domain.DiscoveredPool, error) {
	unseen := make([]domain.DiscoveredPool, 0, len(pools))
	var newest domain.DiscoveredPool
	overlap := false
	for _, pool := range pools {
		if err := pool.Validate(); err != nil {
			return nil, false, domain.DiscoveredPool{}, fmt.Errorf("validate discovered pool: %w", err)
		}
		if !watermark.IsZero() && watermark.Matches(pool) {
			overlap = true
		}
		if newest.CreatedAt.IsZero() || newerThan(pool, newest) {
			newest = pool
		}
		identity := pool.Identity()
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		unseen = append(unseen, pool)
	}
	return unseen, overlap, newest, nil
}

func newerThan(left, right domain.DiscoveredPool) bool {
	if right.CreatedAt.IsZero() || left.CreatedAt.After(right.CreatedAt) {
		return true
	}
	return left.CreatedAt.Equal(right.CreatedAt) && left.PoolAddress < right.PoolAddress
}
