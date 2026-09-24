package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestBirdeyeFetchesBoundedDeduplicatedSolanaNewListingHints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.Method, http.MethodGet; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := request.URL.Path, "/defi/v2/tokens/new_listing"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := request.URL.Query().Get("limit"), "20"; got != want {
			t.Errorf("limit = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("X-API-KEY"), "test-key"; got != want {
			t.Errorf("X-API-KEY = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("x-chain"), domain.NetworkSolana; got != want {
			t.Errorf("x-chain = %q, want %q", got, want)
		}
		_, _ = writer.Write([]byte(`{"success":true,"data":{"items":[{"address":"mint-b"},{"address":"mint-a"},{"address":"mint-b"}]}}`))
	}))
	defer server.Close()

	hints, err := httpclient.NewBirdeye(newBoundedClient(t, server.URL), "test-key").FetchLatestSolanaTokenHints(context.Background())
	if err != nil {
		t.Fatalf("FetchLatestSolanaTokenHints() error = %v", err)
	}
	if got, want := len(hints), 2; got != want {
		t.Fatalf("hint count = %d, want %d", got, want)
	}
	if got, want := hints[0], (domain.TokenHint{Source: domain.SourceBirdeye, Network: domain.NetworkSolana, MintAddress: "mint-a"}); got != want {
		t.Fatalf("first hint = %#v, want %#v", got, want)
	}
	if got, want := hints[1], (domain.TokenHint{Source: domain.SourceBirdeye, Network: domain.NetworkSolana, MintAddress: "mint-b"}); got != want {
		t.Fatalf("second hint = %#v, want %#v", got, want)
	}
}

func TestBirdeyeRejectsNewListingWithoutMintAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"success":true,"data":{"items":[{}]}}`))
	}))
	defer server.Close()

	_, err := httpclient.NewBirdeye(newBoundedClient(t, server.URL), "test-key").FetchLatestSolanaTokenHints(context.Background())
	if err == nil {
		t.Fatal("FetchLatestSolanaTokenHints() accepted a new-listing item without an address")
	}
}

func TestBirdeyeConfirmsStructuredSecurityForRequestedMint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.Method, http.MethodGet; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := request.URL.Path, "/defi/token_security"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		if got, want := request.URL.Query().Get("address"), "mint-address"; got != want {
			t.Errorf("address = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("X-API-KEY"), "test-key"; got != want {
			t.Errorf("X-API-KEY = %q, want %q", got, want)
		}
		if got, want := request.Header.Get("x-chain"), domain.NetworkSolana; got != want {
			t.Errorf("x-chain = %q, want %q", got, want)
		}
		_, _ = writer.Write([]byte(`{"success":true,"data":{"ownerAddress":"public-owner"}}`))
	}))
	defer server.Close()

	if err := httpclient.NewBirdeye(newBoundedClient(t, server.URL), "test-key").ConfirmSecurity(context.Background(), "mint-address"); err != nil {
		t.Fatalf("ConfirmSecurity() error = %v", err)
	}
}

func TestBirdeyeRejectsEmptySecurityResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"success":true,"data":null}`))
	}))
	defer server.Close()

	if err := httpclient.NewBirdeye(newBoundedClient(t, server.URL), "test-key").ConfirmSecurity(context.Background(), "mint-address"); err == nil {
		t.Fatal("ConfirmSecurity() accepted an empty security response")
	}
}
