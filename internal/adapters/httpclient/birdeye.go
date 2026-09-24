package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

const (
	birdeyeNewListingPath = "/defi/v2/tokens/new_listing"
	birdeyeTokenSecurity  = "/defi/token_security"
	maxBirdeyeTokenHints  = 20
)

type Birdeye struct {
	client *Client
	apiKey string
}

func NewBirdeye(client *Client, apiKey string) *Birdeye {
	return &Birdeye{client: client, apiKey: apiKey}
}

func (provider *Birdeye) FetchLatestSolanaTokenHints(ctx context.Context) ([]domain.TokenHint, error) {
	if err := provider.validate(); err != nil {
		return nil, err
	}

	var response birdeyeNewListingResponse
	if err := provider.client.GetJSONWithHeaders(
		ctx,
		birdeyeNewListingPath,
		url.Values{"limit": {fmt.Sprintf("%d", maxBirdeyeTokenHints)}},
		birdeyeHeaders(provider.apiKey),
		&response,
	); err != nil {
		return nil, fmt.Errorf("fetch Birdeye Solana new listings: %w", err)
	}
	if !response.Success {
		return nil, errors.New("Birdeye new listing response was unsuccessful")
	}

	seen := make(map[string]struct{}, len(response.Data.Items))
	hints := make([]domain.TokenHint, 0, min(len(response.Data.Items), maxBirdeyeTokenHints))
	for _, item := range response.Data.Items {
		if len(hints) == maxBirdeyeTokenHints {
			break
		}
		hint := domain.TokenHint{Source: domain.SourceBirdeye, Network: domain.NetworkSolana, MintAddress: item.Address}
		if err := hint.Validate(); err != nil {
			return nil, fmt.Errorf("validate Birdeye new listing: %w", err)
		}
		if _, exists := seen[hint.MintAddress]; exists {
			continue
		}
		seen[hint.MintAddress] = struct{}{}
		hints = append(hints, hint)
	}
	sort.Slice(hints, func(left, right int) bool { return hints[left].MintAddress < hints[right].MintAddress })
	return hints, nil
}

// ConfirmSecurity only verifies that Birdeye returned structured public security
// context for the requested mint. Admission continues to use the authoritative
// Solana RPC mint inspection; this method neither interprets provider risk flags
// nor persists raw provider data.
func (provider *Birdeye) ConfirmSecurity(ctx context.Context, mintAddress string) error {
	if err := provider.validate(); err != nil {
		return err
	}
	if strings.TrimSpace(mintAddress) == "" || strings.ContainsAny(mintAddress, "\"\t\r\n ") {
		return errors.New("Birdeye security confirmation requires a mint address without query syntax")
	}

	var response birdeyeSecurityResponse
	if err := provider.client.GetJSONWithHeaders(
		ctx,
		birdeyeTokenSecurity,
		url.Values{"address": {mintAddress}},
		birdeyeHeaders(provider.apiKey),
		&response,
	); err != nil {
		return fmt.Errorf("fetch Birdeye token security: %w", err)
	}
	if !response.Success || len(response.Data) == 0 || string(response.Data) == "null" {
		return errors.New("Birdeye token security response was empty or unsuccessful")
	}

	var report map[string]json.RawMessage
	if err := json.Unmarshal(response.Data, &report); err != nil || len(report) == 0 {
		return errors.New("Birdeye token security response was not a structured report")
	}
	return nil
}

func (provider *Birdeye) validate() error {
	if provider == nil || provider.client == nil || strings.TrimSpace(provider.apiKey) == "" {
		return errors.New("Birdeye provider is not configured")
	}
	return nil
}

func birdeyeHeaders(apiKey string) http.Header {
	return http.Header{
		"X-API-KEY": {apiKey},
		"x-chain":   {domain.NetworkSolana},
	}
}

type birdeyeNewListingResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []struct {
			Address string `json:"address"`
		} `json:"items"`
	} `json:"data"`
}

type birdeyeSecurityResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

var _ ports.TokenHintDiscovery = (*Birdeye)(nil)
