package application_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

func TestDiscoveryPersistsThroughWatermarkOverlapThenAdvances(t *testing.T) {
	oldest := pool("pool-oldest", "mint-oldest", "2026-08-19T10:00:00Z")
	provider := scriptedPoolDiscovery{pages: map[int]ports.PoolPage{
		1: {Pools: []domain.DiscoveredPool{pool("pool-newest", "mint-newest", "2026-08-19T12:00:00Z"), pool("pool-middle", "mint-middle", "2026-08-19T11:00:00Z")}, NextPage: 2},
		2: {Pools: []domain.DiscoveredPool{oldest}, NextPage: 3},
	}}
	repository := &memoryCandidateRepository{watermark: domain.Watermark{CreatedAt: oldest.CreatedAt, PoolAddress: oldest.PoolAddress}}

	result, err := application.NewDiscovery(&provider, repository, application.DiscoveryOptions{Source: domain.SourceGeckoTerminal, MaxPages: 3}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Overlapped {
		t.Fatal("Run() did not report watermark overlap")
	}
	if got, want := provider.calls, []int{1, 2}; !samePages(got, want) {
		t.Fatalf("pages fetched = %v, want %v", got, want)
	}
	if got, want := repository.persistedPools, []string{"pool-newest", "pool-middle", "pool-oldest"}; !sameStrings(got, want) {
		t.Fatalf("persisted pools = %v, want %v", got, want)
	}
	pools := reflect.ValueOf(result).FieldByName("Pools")
	if !pools.IsValid() {
		t.Fatal("Run() result does not expose bounded newly observed pools")
	}
	observed, ok := pools.Interface().([]domain.DiscoveredPool)
	if !ok || len(observed) != 3 || observed[0].PoolAddress != "pool-newest" || observed[1].PoolAddress != "pool-middle" || observed[2].PoolAddress != "pool-oldest" {
		t.Fatalf("Run() pools = %#v, want the persisted bounded pools in provider order", pools.Interface())
	}
	if got, want := repository.saved, (domain.Watermark{CreatedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC), PoolAddress: "pool-newest"}); got != want {
		t.Fatalf("saved watermark = %#v, want %#v", got, want)
	}
}

func TestDiscoveryDoesNotAdvanceWatermarkAfterPartialFailure(t *testing.T) {
	oldest := pool("pool-oldest", "mint-oldest", "2026-08-19T10:00:00Z")
	provider := scriptedPoolDiscovery{
		pages: map[int]ports.PoolPage{1: {Pools: []domain.DiscoveredPool{pool("pool-new", "mint-new", "2026-08-19T12:00:00Z")}, NextPage: 2}},
		errs:  map[int]error{2: errors.New("upstream timeout")},
	}
	repository := &memoryCandidateRepository{watermark: domain.Watermark{CreatedAt: oldest.CreatedAt, PoolAddress: oldest.PoolAddress}}

	_, err := application.NewDiscovery(&provider, repository, application.DiscoveryOptions{Source: domain.SourceGeckoTerminal, MaxPages: 3}).Run(context.Background())
	if err == nil {
		t.Fatal("Run() accepted a partial provider failure")
	}
	if !repository.saved.IsZero() {
		t.Fatalf("watermark advanced after partial failure: %#v", repository.saved)
	}
}

func TestDiscoveryRecordsCoverageGapWhenPageBudgetIsExhausted(t *testing.T) {
	oldest := pool("pool-oldest", "mint-oldest", "2026-08-19T10:00:00Z")
	provider := scriptedPoolDiscovery{pages: map[int]ports.PoolPage{
		1: {Pools: []domain.DiscoveredPool{pool("pool-new", "mint-new", "2026-08-19T12:00:00Z")}, NextPage: 2},
	}}
	repository := &memoryCandidateRepository{watermark: domain.Watermark{CreatedAt: oldest.CreatedAt, PoolAddress: oldest.PoolAddress}}

	result, err := application.NewDiscovery(&provider, repository, application.DiscoveryOptions{Source: domain.SourceGeckoTerminal, MaxPages: 1}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.CoverageGap || repository.coverageGapPage != 1 {
		t.Fatalf("coverage gap = %#v on page %d, want page 1", result, repository.coverageGapPage)
	}
	if !repository.saved.IsZero() {
		t.Fatalf("watermark advanced after coverage gap: %#v", repository.saved)
	}
}

func TestDiscoveryIsIdempotentAcrossRepeatedPools(t *testing.T) {
	oldest := pool("pool-oldest", "mint-oldest", "2026-08-19T10:00:00Z")
	newest := pool("pool-new", "mint-new", "2026-08-19T12:00:00Z")
	provider := scriptedPoolDiscovery{pages: map[int]ports.PoolPage{
		1: {Pools: []domain.DiscoveredPool{newest}, NextPage: 2},
		2: {Pools: []domain.DiscoveredPool{newest, oldest}},
	}}
	repository := &memoryCandidateRepository{watermark: domain.Watermark{CreatedAt: oldest.CreatedAt, PoolAddress: oldest.PoolAddress}}

	_, err := application.NewDiscovery(&provider, repository, application.DiscoveryOptions{Source: domain.SourceGeckoTerminal, MaxPages: 2}).Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := repository.persistedPools, []string{"pool-new", "pool-oldest"}; !sameStrings(got, want) {
		t.Fatalf("persisted pools = %v, want unique %v", got, want)
	}
}

type scriptedPoolDiscovery struct {
	pages map[int]ports.PoolPage
	errs  map[int]error
	calls []int
}

func (discovery *scriptedPoolDiscovery) FetchNewPools(_ context.Context, page int) (ports.PoolPage, error) {
	discovery.calls = append(discovery.calls, page)
	if err := discovery.errs[page]; err != nil {
		return ports.PoolPage{}, err
	}
	return discovery.pages[page], nil
}

type memoryCandidateRepository struct {
	watermark       domain.Watermark
	saved           domain.Watermark
	persistedPools  []string
	coverageGapPage int
}

func (repository *memoryCandidateRepository) InsertDiscovered(_ context.Context, pools []domain.DiscoveredPool) (int, error) {
	for _, pool := range pools {
		repository.persistedPools = append(repository.persistedPools, pool.PoolAddress)
	}
	return len(pools), nil
}
func (repository *memoryCandidateRepository) LoadWatermark(_ context.Context, _ string) (domain.Watermark, error) {
	return repository.watermark, nil
}
func (repository *memoryCandidateRepository) SaveWatermark(_ context.Context, _ string, mark domain.Watermark) error {
	repository.saved = mark
	return nil
}
func (repository *memoryCandidateRepository) RecordCoverageGap(_ context.Context, _ string, page int, _ string) error {
	repository.coverageGapPage = page
	return nil
}

func pool(poolAddress, mintAddress, createdAt string) domain.DiscoveredPool {
	parsed, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		panic(err)
	}
	return domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: mintAddress, PoolAddress: poolAddress, CreatedAt: parsed}
}

func samePages(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
