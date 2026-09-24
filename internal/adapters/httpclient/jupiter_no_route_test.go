package httpclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestJupiterQuoteClassifiesNoRouteResponseWithoutExposingBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
		_, _ = response.Write([]byte(`{"internal":"must never escape"}`))
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Options{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 4096, MaxAttempts: 1, UserAgent: "test-agent"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = httpclient.NewJupiter(client, "test-key").Quote(context.Background(), "input-mint", "output-mint", 10_000_000)
	if !errors.Is(err, domain.ErrNoExecutableRoute) {
		t.Fatalf("Quote() error = %v, want ErrNoExecutableRoute", err)
	}
}
