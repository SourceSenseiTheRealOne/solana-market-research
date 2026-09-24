package httpclient_test

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestSolanaRPCInspectsLegacyMintAuthoritiesReadOnly(t *testing.T) {
	mintData := make([]byte, 82) // Both COption authority tags are zero: revoked.
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got, want := request.Method, http.MethodPost; got != want {
			t.Fatalf("method = %q, want %q", got, want)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"value":{"owner":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","data":["` + base64.StdEncoding.EncodeToString(mintData) + `","base64"]}}}`))
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Options{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 4096, MaxAttempts: 1, UserAgent: "test-agent"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	inspector := httpclient.NewSolanaRPC(client)

	snapshot, err := inspector.Inspect(context.Background(), "mint-address")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if got, want := snapshot.Program, domain.TokenProgramLegacy; got != want {
		t.Fatalf("program = %q, want %q", got, want)
	}
	if !snapshot.MintAuthorityRevoked || !snapshot.FreezeAuthorityRevoked {
		t.Fatalf("authority revocation = %+v, want both revoked", snapshot)
	}
}

func TestSolanaRPCNormalizesKnownToken2022Extensions(t *testing.T) {
	mintData := make([]byte, 86)
	binary.LittleEndian.PutUint16(mintData[82:84], 1) // TransferFeeConfig.
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"value":{"owner":"TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb","data":["` + base64.StdEncoding.EncodeToString(mintData) + `","base64"]}}}`))
	}))
	defer server.Close()

	client, err := httpclient.New(httpclient.Options{BaseURL: server.URL, Timeout: time.Second, MaxBodyBytes: 4096, MaxAttempts: 1, UserAgent: "test-agent"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	snapshot, err := httpclient.NewSolanaRPC(client).Inspect(context.Background(), "mint-address")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if got, want := snapshot.Extensions, []domain.TokenExtension{"TransferFeeConfig"}; !equalExtensions(got, want) {
		t.Fatalf("extensions = %v, want %v", got, want)
	}
}

func equalExtensions(left, right []domain.TokenExtension) bool {
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
