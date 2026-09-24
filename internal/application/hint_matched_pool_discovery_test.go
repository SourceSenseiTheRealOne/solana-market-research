package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

func TestHintMatchedPoolDiscoveryReturnsGeckoTerminalFreshPoolsWithoutDexScreenerMatch(t *testing.T) {
	calls := make([]string, 0, 2)
	hints := hintDiscoveryFake{calls: &calls, hints: []domain.TokenHint{{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: "matching-mint"}}}
	pools := poolDiscoveryFake{calls: &calls, page: ports.PoolPage{Pools: []domain.DiscoveredPool{
		{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "other-mint", PoolAddress: "other-pool", CreatedAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)},
		{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "matching-mint", PoolAddress: "matching-pool", CreatedAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)},
	}, NextPage: 2}}

	page, err := application.NewHintMatchedPoolDiscovery(hints, pools).FetchNewPools(context.Background(), 1)
	if err != nil {
		t.Fatalf("FetchNewPools() error = %v", err)
	}
	if got, want := calls, []string{"dex", "gecko"}; !sameStrings(got, want) {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
	if len(page.Pools) != 2 || page.Pools[0].MintAddress != "other-mint" || page.Pools[1].MintAddress != "matching-mint" || page.NextPage != 2 {
		t.Fatalf("FetchNewPools() page = %#v, want all bounded GeckoTerminal fresh pools regardless of DexScreener match", page)
	}
}

func TestHintMatchedPoolDiscoveryFetchesGeckoTerminalWhenDexScreenerHasNoHints(t *testing.T) {
	calls := make([]string, 0, 2)
	hints := hintDiscoveryFake{calls: &calls}
	pools := poolDiscoveryFake{calls: &calls}

	page, err := application.NewHintMatchedPoolDiscovery(hints, pools).FetchNewPools(context.Background(), 1)
	if err != nil {
		t.Fatalf("FetchNewPools() error = %v", err)
	}
	if got, want := calls, []string{"dex", "gecko"}; !sameStrings(got, want) {
		t.Fatalf("provider calls = %v, want %v", got, want)
	}
	if len(page.Pools) != 0 || page.NextPage != 0 {
		t.Fatalf("FetchNewPools() page = %#v, want no pool page", page)
	}
}

func TestTwoSourcePoolDiscoveryReturnsDexScreenerOnlyAndGeckoTerminalOnlyPoolsNewestFirst(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	calls := make([]string, 0, 2)
	dex := namedPoolDiscovery{calls: &calls, name: "dex", page: ports.PoolPage{Pools: []domain.DiscoveredPool{
		{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: "dex-only", PoolAddress: "dex-pool", CreatedAt: now},
		{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: "shared", PoolAddress: "shared-pool", CreatedAt: now.Add(-time.Minute)},
	}}}
	gecko := namedPoolDiscovery{calls: &calls, name: "gecko", page: ports.PoolPage{Pools: []domain.DiscoveredPool{
		{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "gecko-only", PoolAddress: "gecko-pool", CreatedAt: now.Add(-2 * time.Minute)},
		{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "shared", PoolAddress: "shared-pool", CreatedAt: now.Add(-time.Minute)},
	}}}

	page, err := application.NewTwoSourcePoolDiscovery(dex, gecko).FetchNewPools(context.Background(), 1)
	if err != nil {
		t.Fatalf("FetchNewPools() error = %v", err)
	}
	if got, want := calls, []string{"dex", "gecko"}; !sameStrings(got, want) {
		t.Fatalf("provider order = %v, want %v", got, want)
	}
	if got, want := len(page.Pools), 3; got != want {
		t.Fatalf("pool count = %d, want %d after source-union deduplication", got, want)
	}
	if got, want := []string{page.Pools[0].MintAddress, page.Pools[1].MintAddress, page.Pools[2].MintAddress}, []string{"dex-only", "shared", "gecko-only"}; !sameStrings(got, want) {
		t.Fatalf("pool order = %v, want newest-first bounded two-source union", got)
	}
}

type hintDiscoveryFake struct {
	calls *[]string
	hints []domain.TokenHint
}

func (fake hintDiscoveryFake) FetchLatestSolanaTokenHints(context.Context) ([]domain.TokenHint, error) {
	*fake.calls = append(*fake.calls, "dex")
	return fake.hints, nil
}

type poolDiscoveryFake struct {
	calls *[]string
	page  ports.PoolPage
}

func (fake poolDiscoveryFake) FetchNewPools(context.Context, int) (ports.PoolPage, error) {
	*fake.calls = append(*fake.calls, "gecko")
	return fake.page, nil
}

type namedPoolDiscovery struct {
	calls *[]string
	name  string
	page  ports.PoolPage
}

func (fake namedPoolDiscovery) FetchNewPools(context.Context, int) (ports.PoolPage, error) {
	*fake.calls = append(*fake.calls, fake.name)
	return fake.page, nil
}
