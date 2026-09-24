package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestSourceRoutedMarketUsesOriginProviderForIndependentPools(t *testing.T) {
	dex := &routedMarketProvider{snapshot: domain.MarketSnapshot{LiquidityUSD: domain.USD{Micros: 1}}}
	gecko := &routedMarketProvider{snapshot: domain.MarketSnapshot{LiquidityUSD: domain.USD{Micros: 2}}}
	market := application.NewSourceRoutedMarket(dex, gecko)
	now := time.Date(2026, time.August, 20, 19, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		source string
		want   int64
	}{
		{source: domain.SourceDexScreener, want: 1},
		{source: domain.SourceGeckoTerminal, want: 2},
	} {
		snapshot, err := market.Fetch(context.Background(), domain.DiscoveredPool{Source: test.source, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: now})
		if err != nil {
			t.Fatalf("Fetch(%s) error = %v", test.source, err)
		}
		if got := snapshot.LiquidityUSD.Micros; got != test.want {
			t.Fatalf("Fetch(%s) liquidity = %d, want %d", test.source, got, test.want)
		}
	}
	if dex.calls != 1 || gecko.calls != 1 {
		t.Fatalf("provider calls dex/gecko = %d/%d, want 1/1", dex.calls, gecko.calls)
	}
}

type routedMarketProvider struct {
	calls    int
	snapshot domain.MarketSnapshot
}

func (provider *routedMarketProvider) Fetch(context.Context, domain.DiscoveredPool) (domain.MarketSnapshot, error) {
	provider.calls++
	return provider.snapshot, nil
}
