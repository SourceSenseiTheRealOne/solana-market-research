package httpclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
)

func TestGeckoTerminalFetchNewPoolsParsesPageAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.URL.Path, "/api/v2/networks/solana/new_pools"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := request.URL.Query().Get("page"), "2"; got != want {
			t.Errorf("page = %q, want %q", got, want)
		}
		_, _ = writer.Write([]byte(`{
			"data": [
				{"type":"pool","id":"solana_pool-one","attributes":{"address":"pool-one","pool_created_at":"2026-08-19T10:00:00Z"},"relationships":{"base_token":{"data":{"type":"token","id":"solana_mint-one"}}}},
				{"type":"pool","id":"solana_pool-one-duplicate","attributes":{"address":"pool-one","pool_created_at":"2026-08-19T10:00:00Z"},"relationships":{"base_token":{"data":{"type":"token","id":"solana_mint-one"}}}}
			],
			"included": [{"type":"token","id":"solana_mint-one","attributes":{"address":"mint-one"}}],
			"links": {"next":"https://api.geckoterminal.com/api/v2/networks/solana/new_pools?page=3"}
		}`))
	}))
	defer server.Close()

	client := newBoundedClient(t, server.URL)
	page, err := httpclient.NewGeckoTerminal(client).FetchNewPools(context.Background(), 2)
	if err != nil {
		t.Fatalf("FetchNewPools() error = %v", err)
	}
	if got, want := len(page.Pools), 1; got != want {
		t.Fatalf("pool count = %d, want %d", got, want)
	}
	pool := page.Pools[0]
	if got, want := pool.Network, "solana"; got != want {
		t.Fatalf("network = %q, want %q", got, want)
	}
	if got, want := pool.MintAddress, "mint-one"; got != want {
		t.Fatalf("mint = %q, want %q", got, want)
	}
	if got, want := pool.PoolAddress, "pool-one"; got != want {
		t.Fatalf("pool = %q, want %q", got, want)
	}
	if got, want := page.NextPage, 3; got != want {
		t.Fatalf("next page = %d, want %d", got, want)
	}
}

func TestGeckoTerminalFetchNewPoolsUsesRelationshipMintWhenIncludedIsAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{
			"data": [{
				"type":"pool",
				"attributes":{"address":"pool-current-schema","pool_created_at":"2026-08-20T10:00:00Z"},
				"relationships":{"base_token":{"data":{"type":"token","id":"solana_mint-current-schema"}}}
			}],
			"links": {}
		}`))
	}))
	defer server.Close()

	page, err := httpclient.NewGeckoTerminal(newBoundedClient(t, server.URL)).FetchNewPools(context.Background(), 1)
	if err != nil {
		t.Fatalf("FetchNewPools() error = %v", err)
	}
	if got, want := len(page.Pools), 1; got != want {
		t.Fatalf("pool count = %d, want %d", got, want)
	}
	if got, want := page.Pools[0].MintAddress, "mint-current-schema"; got != want {
		t.Fatalf("mint = %q, want %q", got, want)
	}
}

func TestGeckoTerminalRejectsInvalidPoolPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing pool address",
			body: `{"data":[{"type":"pool","attributes":{"pool_created_at":"2026-08-19T10:00:00Z"},"relationships":{"base_token":{"data":{"type":"token","id":"solana_mint"}}}}],"included":[{"type":"token","id":"solana_mint","attributes":{"address":"mint"}}]}`,
		},
		{
			name: "malformed timestamp",
			body: `{"data":[{"type":"pool","attributes":{"address":"pool","pool_created_at":"not-a-time"},"relationships":{"base_token":{"data":{"type":"token","id":"solana_mint"}}}}],"included":[{"type":"token","id":"solana_mint","attributes":{"address":"mint"}}]}`,
		},
		{
			name: "malformed relationship mint fallback",
			body: `{"data":[{"type":"pool","attributes":{"address":"pool","pool_created_at":"2026-08-19T10:00:00Z"},"relationships":{"base_token":{"data":{"type":"token","id":"solana_mint_extra"}}}}]}`,
		},
		{
			name: "unknown included relationship",
			body: `{"data":[{"type":"pool","attributes":{"address":"pool","pool_created_at":"2026-08-19T10:00:00Z"},"relationships":{"base_token":{"data":{"type":"token","id":"solana_missing"}}}}],"included":[{"type":"token","id":"solana_other","attributes":{"address":"mint"}}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, err := httpclient.NewGeckoTerminal(newBoundedClient(t, server.URL)).FetchNewPools(context.Background(), 1)
			if err == nil {
				t.Fatal("FetchNewPools() accepted invalid payload")
			}
		})
	}
}

func TestGeckoTerminalClassifiesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "1")
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := httpclient.NewGeckoTerminal(newBoundedClient(t, server.URL)).FetchNewPools(context.Background(), 1)
	if !errors.Is(err, httpclient.ErrRateLimited) {
		t.Fatalf("FetchNewPools() error = %v, want ErrRateLimited", err)
	}
}

func newBoundedClient(t *testing.T, baseURL string) *httpclient.Client {
	t.Helper()
	client, err := httpclient.New(httpclient.Options{
		BaseURL:      baseURL,
		Timeout:      time.Second,
		MaxBodyBytes: 32 * 1024,
		MaxAttempts:  1,
		UserAgent:    "solana-hype-paper-bot/test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}
