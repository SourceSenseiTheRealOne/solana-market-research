package httpclient_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestGeckoTerminalFetchReadsBoundedPoolMarketEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Path, "/api/v2/networks/solana/pools/pool-address"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":{"type":"pool","attributes":{"address":"pool-address","reserve_in_usd":"10000.500001","transactions":{"m5":{"buys":13,"sells":7}},"volume_usd":{"m5":"1500.075000"},"price_change_percentage":{"m5":"2.345"}}}}`))
	}))
	defer server.Close()

	pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool-address", CreatedAt: time.Date(2026, time.August, 20, 19, 0, 0, 0, time.UTC)}
	snapshot, err := httpclient.NewGeckoTerminal(newBoundedClient(t, server.URL)).Fetch(context.Background(), pool)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got, want := snapshot.LiquidityUSD.Micros, int64(10_000_500_001); got != want {
		t.Fatalf("liquidity micros = %d, want %d", got, want)
	}
	if got, want := snapshot.FiveMinuteTransactions, 20; got != want {
		t.Fatalf("five-minute transactions = %d, want %d", got, want)
	}
	if snapshot.FiveMinuteBuys != 13 || snapshot.FiveMinuteSells != 7 || snapshot.FiveMinuteVolumeUSD.Micros != 1_500_075_000 || snapshot.FiveMinutePriceChangeBPS != 235 {
		t.Fatalf("five-minute market evidence = %#v", snapshot)
	}
	if snapshot.ObservedAt.IsZero() {
		t.Fatal("market observation time is zero")
	}
}

func TestGeckoTerminalRejectsIncompleteFiveMinuteMarketEvidence(t *testing.T) {
	tests := []struct{ name, market string }{
		{name: "missing buys", market: `"transactions":{"m5":{"sells":7}},"volume_usd":{"m5":"750"},"price_change_percentage":{"m5":"2"}`},
		{name: "missing sells", market: `"transactions":{"m5":{"buys":13}},"volume_usd":{"m5":"750"},"price_change_percentage":{"m5":"2"}`},
		{name: "missing volume", market: `"transactions":{"m5":{"buys":13,"sells":7}},"volume_usd":{},"price_change_percentage":{"m5":"2"}`},
		{name: "missing price change", market: `"transactions":{"m5":{"buys":13,"sells":7}},"volume_usd":{"m5":"750"},"price_change_percentage":{}`},
		{name: "negative sells", market: `"transactions":{"m5":{"buys":13,"sells":-1}},"volume_usd":{"m5":"750"},"price_change_percentage":{"m5":"2"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(writer, `{"data":{"type":"pool","attributes":{"address":"pool-address","reserve_in_usd":"5000",%s}}}`, test.market)
			}))
			defer server.Close()
			pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool-address", CreatedAt: time.Now().UTC()}
			if _, err := httpclient.NewGeckoTerminal(newBoundedClient(t, server.URL)).Fetch(context.Background(), pool); err == nil {
				t.Fatal("Fetch() accepted incomplete five-minute evidence")
			}
		})
	}
}
