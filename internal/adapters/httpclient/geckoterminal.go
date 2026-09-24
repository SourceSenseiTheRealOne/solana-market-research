package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

type GeckoTerminal struct {
	client *Client
}

func NewGeckoTerminal(client *Client) *GeckoTerminal {
	return &GeckoTerminal{client: client}
}

func (provider *GeckoTerminal) FetchNewPools(ctx context.Context, page int) (ports.PoolPage, error) {
	if page < 1 {
		return ports.PoolPage{}, errors.New("GeckoTerminal page must be at least one")
	}
	var response geckoPoolsResponse
	if err := provider.client.GetJSON(ctx, "/api/v2/networks/solana/new_pools", url.Values{"page": []string{strconv.Itoa(page)}}, &response); err != nil {
		return ports.PoolPage{}, fmt.Errorf("fetch GeckoTerminal new pools: %w", err)
	}

	tokens, err := geckoTokenAddresses(response)
	if err != nil {
		return ports.PoolPage{}, err
	}

	seen := make(map[string]struct{}, len(response.Data))
	pools := make([]domain.DiscoveredPool, 0, len(response.Data))
	for _, item := range response.Data {
		pool, err := parseGeckoPool(item, tokens)
		if err != nil {
			return ports.PoolPage{}, err
		}
		if _, exists := seen[pool.PoolAddress]; exists {
			continue
		}
		seen[pool.PoolAddress] = struct{}{}
		pools = append(pools, pool)
	}
	sort.Slice(pools, func(left, right int) bool {
		if pools[left].CreatedAt.Equal(pools[right].CreatedAt) {
			return pools[left].PoolAddress < pools[right].PoolAddress
		}
		return pools[left].CreatedAt.After(pools[right].CreatedAt)
	})

	nextPage, err := parseNextPage(response.Links.Next)
	if err != nil {
		return ports.PoolPage{}, err
	}
	return ports.PoolPage{Pools: pools, NextPage: nextPage}, nil
}

// Fetch returns bounded market evidence for a GeckoTerminal-discovered pool.
func (provider *GeckoTerminal) Fetch(ctx context.Context, pool domain.DiscoveredPool) (domain.MarketSnapshot, error) {
	if provider == nil || provider.client == nil {
		return domain.MarketSnapshot{}, errors.New("GeckoTerminal market provider is not configured")
	}
	if err := pool.Validate(); err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("validate GeckoTerminal pool: %w", err)
	}
	if pool.Source != domain.SourceGeckoTerminal || pool.Network != domain.NetworkSolana {
		return domain.MarketSnapshot{}, errors.New("GeckoTerminal market provider only supports GeckoTerminal Solana pools")
	}

	var response geckoPoolResponse
	path := "/api/v2/networks/" + domain.NetworkSolana + "/pools/" + url.PathEscape(pool.PoolAddress)
	if err := provider.client.GetJSON(ctx, path, nil, &response); err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("fetch GeckoTerminal pool: %w", err)
	}
	if response.Data.Type != "pool" || response.Data.Attributes.Address != pool.PoolAddress {
		return domain.MarketSnapshot{}, errors.New("GeckoTerminal response did not contain the requested pool")
	}
	liquidity, err := domain.ParseUSD(response.Data.Attributes.ReserveInUSD)
	if err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("parse GeckoTerminal liquidity: %w", err)
	}
	transactions := response.Data.Attributes.Transactions.M5
	if transactions.Buys == nil || transactions.Sells == nil {
		return domain.MarketSnapshot{}, errors.New("GeckoTerminal pool is missing five-minute transaction counts")
	}
	if *transactions.Buys < 0 || *transactions.Sells < 0 {
		return domain.MarketSnapshot{}, errors.New("GeckoTerminal pool has negative transaction counts")
	}
	volume, err := domain.ParseUSD(response.Data.Attributes.VolumeUSD.M5)
	if err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("parse GeckoTerminal five-minute volume: %w", err)
	}
	priceChangeBPS, err := domain.ParsePercentageBPS(response.Data.Attributes.PriceChangePercentage.M5)
	if err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("parse GeckoTerminal five-minute price change: %w", err)
	}
	return domain.MarketSnapshot{
		ObservedAt: time.Now().UTC(), LiquidityUSD: liquidity,
		FiveMinuteTransactions: *transactions.Buys + *transactions.Sells,
		FiveMinuteBuys:         *transactions.Buys, FiveMinuteSells: *transactions.Sells,
		FiveMinuteVolumeUSD: volume, FiveMinutePriceChangeBPS: priceChangeBPS,
	}, nil
}

func geckoTokenAddresses(response geckoPoolsResponse) (map[string]string, error) {
	tokens := make(map[string]string, len(response.Included))
	for _, token := range response.Included {
		if token.Type != "token" || strings.TrimSpace(token.ID) == "" || strings.TrimSpace(token.Attributes.Address) == "" {
			continue
		}
		tokens[token.ID] = token.Attributes.Address
	}
	if len(response.Included) > 0 {
		return tokens, nil
	}

	for _, pool := range response.Data {
		relationshipID := pool.Relationships.BaseToken.Data.ID
		mint, err := geckoRelationshipMint(relationshipID)
		if err != nil {
			return nil, err
		}
		tokens[relationshipID] = mint
	}
	return tokens, nil
}

func geckoRelationshipMint(relationshipID string) (string, error) {
	network, mint, ok := strings.Cut(strings.TrimSpace(relationshipID), "_")
	if !ok || network != domain.NetworkSolana || strings.TrimSpace(mint) == "" || strings.Contains(mint, "_") {
		return "", errors.New("GeckoTerminal pool base-token relationship is not a canonical Solana token ID")
	}
	return mint, nil
}

func parseGeckoPool(item geckoPool, tokens map[string]string) (domain.DiscoveredPool, error) {
	if item.Type != "pool" {
		return domain.DiscoveredPool{}, errors.New("GeckoTerminal response contains a non-pool data item")
	}
	mint, ok := tokens[item.Relationships.BaseToken.Data.ID]
	if !ok {
		return domain.DiscoveredPool{}, errors.New("GeckoTerminal pool base-token relationship was not included")
	}
	createdAt, err := time.Parse(time.RFC3339, item.Attributes.PoolCreatedAt)
	if err != nil {
		return domain.DiscoveredPool{}, fmt.Errorf("parse GeckoTerminal pool creation time: %w", err)
	}
	pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: mint, PoolAddress: item.Attributes.Address, CreatedAt: createdAt.UTC()}
	if err := pool.Validate(); err != nil {
		return domain.DiscoveredPool{}, fmt.Errorf("validate GeckoTerminal pool: %w", err)
	}
	return pool, nil
}

func parseNextPage(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return 0, fmt.Errorf("parse GeckoTerminal next link: %w", err)
	}
	page, err := strconv.Atoi(parsed.Query().Get("page"))
	if err != nil || page < 1 {
		return 0, errors.New("GeckoTerminal next link has no valid page")
	}
	return page, nil
}

type geckoPoolsResponse struct {
	Data     []geckoPool    `json:"data"`
	Included []geckoToken   `json:"included"`
	Links    geckoPageLinks `json:"links"`
}

type geckoPool struct {
	Type       string `json:"type"`
	Attributes struct {
		Address       string `json:"address"`
		PoolCreatedAt string `json:"pool_created_at"`
		ReserveInUSD  string `json:"reserve_in_usd"`
		Transactions  struct {
			M5 struct {
				Buys  *int `json:"buys"`
				Sells *int `json:"sells"`
			} `json:"m5"`
		} `json:"transactions"`
		VolumeUSD struct {
			M5 string `json:"m5"`
		} `json:"volume_usd"`
		PriceChangePercentage struct {
			M5 string `json:"m5"`
		} `json:"price_change_percentage"`
	} `json:"attributes"`
	Relationships struct {
		BaseToken struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"base_token"`
	} `json:"relationships"`
}

type geckoPoolResponse struct {
	Data geckoPool `json:"data"`
}

type geckoToken struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Attributes struct {
		Address string `json:"address"`
	} `json:"attributes"`
}

type geckoPageLinks struct {
	Next string `json:"next"`
}

var _ ports.PoolDiscovery = (*GeckoTerminal)(nil)
