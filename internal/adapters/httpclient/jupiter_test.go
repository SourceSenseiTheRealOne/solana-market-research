package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestJupiterQuoteUsesReadOnlyFullSizeQuoteEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got, want := request.Method, http.MethodGet; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		if got, want := request.URL.Path, "/swap/v1/quote"; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("X-API-Key"), "test-key"; got != want {
			t.Fatalf("X-API-Key = %q, want %q", got, want)
		}
		query := request.URL.Query()
		for key, want := range map[string]string{
			"inputMint":                  "input-mint",
			"outputMint":                 "output-mint",
			"amount":                     "10000000",
			"slippageBps":                "500",
			"restrictIntermediateTokens": "true",
			"instructionVersion":         "V2",
		} {
			if got := query.Get(key); got != want {
				t.Fatalf("query[%q] = %q, want %q", key, got, want)
			}
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"inputMint":"input-mint","outputMint":"output-mint","inAmount":"10000000","outAmount":"25000000","priceImpactPct":"0.0125",
			"routePlan":[{"swapInfo":{"ammKey":"route-one","label":"Raydium","feeAmount":"31","feeMint":"output-mint"},"percent":100}]
		}`))
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Options{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 4096, MaxAttempts: 1, UserAgent: "test-agent"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider := httpclient.NewJupiter(client, "test-key")

	quote, err := provider.Quote(context.Background(), "input-mint", "output-mint", 10_000_000)
	if err != nil {
		t.Fatalf("Quote() error = %v", err)
	}
	if got, want := quote.PriceImpactBPS, int64(125); got != want {
		t.Fatalf("price impact = %d bps, want %d", got, want)
	}
	if got, want := quote.OutAmount, uint64(25_000_000); got != want {
		t.Fatalf("out amount = %d, want %d", got, want)
	}
	if got, want := quote.RoutePlan[0].AMMKey, "route-one"; got != want {
		t.Fatalf("route AMM = %q, want %q", got, want)
	}
	if quote.ObservedAt.IsZero() {
		t.Fatal("quote observation time is zero")
	}
	if got, want := quote.RoutePlan[0].Fee, (domain.QuoteFee{Amount: 31, Mint: "output-mint"}); got != want {
		t.Fatalf("route fee = %#v, want %#v", got, want)
	}
}
