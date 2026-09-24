package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

const (
	maxDexScreenerTokenHints    = 20
	maxDexScreenerResolvedPools = 5
)

type DexScreener struct {
	client *Client
}

func NewDexScreener(client *Client) *DexScreener {
	return &DexScreener{client: client}
}

func (provider *DexScreener) FetchLatestSolanaTokenHints(ctx context.Context) ([]domain.TokenHint, error) {
	var profiles []dexScreenerTokenProfile
	if err := provider.client.GetJSON(ctx, "/token-profiles/latest/v1", nil, &profiles); err != nil {
		return nil, fmt.Errorf("fetch DexScreener token profiles: %w", err)
	}

	seen := make(map[string]struct{}, len(profiles))
	hints := make([]domain.TokenHint, 0, min(len(profiles), maxDexScreenerTokenHints))
	for _, profile := range profiles {
		if len(hints) == maxDexScreenerTokenHints {
			break
		}
		if profile.ChainID != domain.NetworkSolana {
			continue
		}
		hint := domain.TokenHint{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: profile.TokenAddress, URL: profile.URL}
		if err := hint.Validate(); err != nil {
			return nil, fmt.Errorf("validate DexScreener token profile: %w", err)
		}
		if _, exists := seen[hint.MintAddress]; exists {
			continue
		}
		seen[hint.MintAddress] = struct{}{}
		hints = append(hints, hint)
	}
	return hints, nil
}

// FetchNewPools resolves at most five latest DexScreener Solana profiles into
// validated pool identities. DexScreener provides no page cursor for profile
// resolution, so pages after the first are intentionally empty.
func (provider *DexScreener) FetchNewPools(ctx context.Context, page int) (ports.PoolPage, error) {
	if provider == nil || provider.client == nil || page < 1 {
		return ports.PoolPage{}, errors.New("DexScreener pool discovery is not completely configured")
	}
	if page > 1 {
		return ports.PoolPage{}, nil
	}
	hints, err := provider.FetchLatestSolanaTokenHints(ctx)
	if err != nil {
		return ports.PoolPage{}, err
	}
	pools := make([]domain.DiscoveredPool, 0, min(len(hints), maxDexScreenerResolvedPools))
	for _, hint := range hints {
		if len(pools) == maxDexScreenerResolvedPools {
			break
		}
		pool, found, err := provider.newestSolanaPoolForHint(ctx, hint)
		if err != nil {
			return ports.PoolPage{}, err
		}
		if found {
			pools = append(pools, pool)
		}
	}
	sort.Slice(pools, func(left, right int) bool {
		if pools[left].CreatedAt.Equal(pools[right].CreatedAt) {
			return pools[left].PoolAddress < pools[right].PoolAddress
		}
		return pools[left].CreatedAt.After(pools[right].CreatedAt)
	})
	return ports.PoolPage{Pools: pools}, nil
}

func (provider *DexScreener) newestSolanaPoolForHint(ctx context.Context, hint domain.TokenHint) (domain.DiscoveredPool, bool, error) {
	if err := hint.Validate(); err != nil || hint.Source != domain.SourceDexScreener || hint.Network != domain.NetworkSolana {
		return domain.DiscoveredPool{}, false, errors.New("DexScreener token hint is invalid")
	}
	var pairs []dexScreenerPair
	path := "/token-pairs/v1/" + domain.NetworkSolana + "/" + url.PathEscape(hint.MintAddress)
	if err := provider.client.GetJSON(ctx, path, nil, &pairs); err != nil {
		return domain.DiscoveredPool{}, false, fmt.Errorf("fetch DexScreener token pairs: %w", err)
	}
	var newest domain.DiscoveredPool
	for _, pair := range pairs {
		if !isSolana(pair.ChainID) || strings.TrimSpace(pair.PairAddress) == "" || pair.PairCreatedAt <= 0 {
			continue
		}
		candidate := domain.DiscoveredPool{
			Source:      domain.SourceDexScreener,
			Network:     domain.NetworkSolana,
			MintAddress: hint.MintAddress,
			PoolAddress: pair.PairAddress,
			CreatedAt:   time.UnixMilli(pair.PairCreatedAt).UTC(),
		}
		if err := candidate.Validate(); err != nil {
			continue
		}
		if newest.CreatedAt.IsZero() || candidate.CreatedAt.After(newest.CreatedAt) || (candidate.CreatedAt.Equal(newest.CreatedAt) && candidate.PoolAddress < newest.PoolAddress) {
			newest = candidate
		}
	}
	return newest, !newest.CreatedAt.IsZero(), nil
}

func (provider *DexScreener) Fetch(ctx context.Context, pool domain.DiscoveredPool) (domain.MarketSnapshot, error) {
	if provider.client == nil {
		return domain.MarketSnapshot{}, errors.New("DexScreener market provider is not configured")
	}
	if err := pool.Validate(); err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("validate DexScreener pool: %w", err)
	}
	if !isSolana(pool.Network) {
		return domain.MarketSnapshot{}, errors.New("DexScreener market provider only supports Solana pools")
	}
	response, err := provider.fetchPairResponse(ctx, pool.PoolAddress)
	if err != nil {
		return domain.MarketSnapshot{}, err
	}
	snapshot, marketErr := dexScreenerMarketSnapshot(response, pool.PoolAddress)
	if marketErr == nil {
		return snapshot, nil
	}
	hint := domain.TokenHint{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: pool.MintAddress}
	resolved, found, err := provider.newestSolanaPoolForHint(ctx, hint)
	if err != nil {
		return domain.MarketSnapshot{}, errors.Join(marketErr, fmt.Errorf("resolve newest DexScreener pool after unavailable evidence: %w", err))
	}
	if !found || resolved.PoolAddress == pool.PoolAddress || !resolved.CreatedAt.After(pool.CreatedAt) {
		return domain.MarketSnapshot{}, marketErr
	}
	response, err = provider.fetchPairResponse(ctx, resolved.PoolAddress)
	if err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("fetch resolved DexScreener pair: %w", err)
	}
	snapshot, err = dexScreenerMarketSnapshot(response, resolved.PoolAddress)
	if err != nil {
		return domain.MarketSnapshot{}, fmt.Errorf("normalize resolved DexScreener pair: %w", err)
	}
	return snapshot, nil
}

func (provider *DexScreener) fetchPairResponse(ctx context.Context, poolAddress string) (dexScreenerPairsResponse, error) {
	var response dexScreenerPairsResponse
	path := "/latest/dex/pairs/" + domain.NetworkSolana + "/" + url.PathEscape(poolAddress)
	if err := provider.client.GetJSON(ctx, path, nil, &response); err != nil {
		return dexScreenerPairsResponse{}, fmt.Errorf("fetch DexScreener pair: %w", err)
	}
	return response, nil
}

func dexScreenerMarketSnapshot(response dexScreenerPairsResponse, poolAddress string) (domain.MarketSnapshot, error) {
	for _, pair := range response.Pairs {
		if !isSolana(pair.ChainID) || pair.PairAddress != poolAddress {
			continue
		}
		liquidity, err := domain.ParseUSD(pair.Liquidity.USD.String())
		if err != nil {
			return domain.MarketSnapshot{}, fmt.Errorf("parse DexScreener liquidity: %w", err)
		}
		if strings.EqualFold(pair.DexID, "pumpfun") && liquidity.Micros == 0 {
			return domain.MarketSnapshot{}, errors.New("DexScreener Pump.fun curve has no migrated pool liquidity")
		}
		if pair.Transactions.M5.Buys == nil || pair.Transactions.M5.Sells == nil {
			return domain.MarketSnapshot{}, errors.New("DexScreener pair is missing five-minute transaction counts")
		}
		if *pair.Transactions.M5.Buys < 0 || *pair.Transactions.M5.Sells < 0 {
			return domain.MarketSnapshot{}, errors.New("DexScreener pair has negative transaction counts")
		}
		volume, err := domain.ParseUSD(pair.Volume.M5.String())
		if err != nil {
			return domain.MarketSnapshot{}, fmt.Errorf("parse DexScreener five-minute volume: %w", err)
		}
		priceChangeBPS, err := domain.ParsePercentageBPS(pair.PriceChange.M5.String())
		if err != nil {
			return domain.MarketSnapshot{}, fmt.Errorf("parse DexScreener five-minute price change: %w", err)
		}
		return domain.MarketSnapshot{
			ObservedAt: time.Now().UTC(), LiquidityUSD: liquidity,
			FiveMinuteTransactions: *pair.Transactions.M5.Buys + *pair.Transactions.M5.Sells,
			FiveMinuteBuys:         *pair.Transactions.M5.Buys, FiveMinuteSells: *pair.Transactions.M5.Sells,
			FiveMinuteVolumeUSD: volume, FiveMinutePriceChangeBPS: priceChangeBPS,
		}, nil
	}
	return domain.MarketSnapshot{}, errors.New("DexScreener response did not contain the requested Solana pool")
}

type dexScreenerTokenProfile struct {
	ChainID      string `json:"chainId"`
	TokenAddress string `json:"tokenAddress"`
	URL          string `json:"url"`
}

type dexScreenerPairsResponse struct {
	Pairs []dexScreenerPair `json:"pairs"`
}

type dexScreenerPair struct {
	ChainID       string `json:"chainId"`
	DexID         string `json:"dexId"`
	PairAddress   string `json:"pairAddress"`
	PairCreatedAt int64  `json:"pairCreatedAt"`
	Liquidity     struct {
		USD json.Number `json:"usd"`
	} `json:"liquidity"`
	Transactions struct {
		M5 struct {
			Buys  *int `json:"buys"`
			Sells *int `json:"sells"`
		} `json:"m5"`
	} `json:"txns"`
	Volume struct {
		M5 json.Number `json:"m5"`
	} `json:"volume"`
	PriceChange struct {
		M5 json.Number `json:"m5"`
	} `json:"priceChange"`
}

var _ ports.TokenHintDiscovery = (*DexScreener)(nil)
var _ ports.PoolDiscovery = (*DexScreener)(nil)

func isSolana(chainID string) bool {
	return strings.EqualFold(strings.TrimSpace(chainID), domain.NetworkSolana)
}
