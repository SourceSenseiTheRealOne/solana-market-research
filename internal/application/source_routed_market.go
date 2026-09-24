package application

import (
	"context"
	"errors"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

// SourceRoutedMarket keeps independent discovery sources independent during
// market evidence collection.
type SourceRoutedMarket struct {
	dexScreener   MarketProvider
	geckoTerminal MarketProvider
}

func NewSourceRoutedMarket(dexScreener, geckoTerminal MarketProvider) *SourceRoutedMarket {
	return &SourceRoutedMarket{dexScreener: dexScreener, geckoTerminal: geckoTerminal}
}

func (market *SourceRoutedMarket) Fetch(ctx context.Context, pool domain.DiscoveredPool) (domain.MarketSnapshot, error) {
	if market == nil {
		return domain.MarketSnapshot{}, errors.New("source-routed market provider is not configured")
	}
	switch pool.Source {
	case domain.SourceDexScreener:
		if market.dexScreener == nil {
			return domain.MarketSnapshot{}, errors.New("DexScreener market provider is not configured")
		}
		return market.dexScreener.Fetch(ctx, pool)
	case domain.SourceGeckoTerminal:
		if market.geckoTerminal == nil {
			return domain.MarketSnapshot{}, errors.New("GeckoTerminal market provider is not configured")
		}
		return market.geckoTerminal.Fetch(ctx, pool)
	default:
		return domain.MarketSnapshot{}, errors.New("market provider does not support pool source")
	}
}

var _ MarketProvider = (*SourceRoutedMarket)(nil)
