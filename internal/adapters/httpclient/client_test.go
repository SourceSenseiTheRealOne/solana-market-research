package httpclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
)

func TestClientRejectsResponseLargerThanConfiguredLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("{}" + strings.Repeat(" ", 32)))
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Options{
		BaseURL:      server.URL,
		Timeout:      time.Second,
		MaxBodyBytes: 2,
		MaxAttempts:  1,
		UserAgent:    "solana-hype-paper-bot/test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var response map[string]any
	err = client.GetJSON(context.Background(), "/response", nil, &response)
	if !errors.Is(err, httpclient.ErrResponseTooLarge) {
		t.Fatalf("GetJSON() error = %v, want ErrResponseTooLarge", err)
	}
}

func TestClientPreservesFixedBaseQueryForJSONRPCRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/" || request.URL.Query().Get("api-key") != "test-helius-key" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"ok"}`))
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Options{
		BaseURL:      server.URL + "?api-key=test-helius-key",
		Timeout:      time.Second,
		MaxBodyBytes: 1024,
		MaxAttempts:  1,
		UserAgent:    "solana-hype-paper-bot/test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var response struct {
		Result string `json:"result"`
	}
	if err := client.PostJSON(context.Background(), "/", map[string]string{"method": "getHealth"}, &response); err != nil {
		t.Fatalf("PostJSON() error = %v, want a request retaining the fixed Helius query", err)
	}
	if response.Result != "ok" {
		t.Fatalf("PostJSON() result = %q, want ok", response.Result)
	}
}
