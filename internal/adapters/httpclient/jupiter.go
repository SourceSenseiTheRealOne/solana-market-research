package httpclient

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

type Jupiter struct {
	client *Client
	apiKey string
}

func NewJupiter(client *Client, apiKey string) *Jupiter {
	return &Jupiter{client: client, apiKey: apiKey}
}

func (provider *Jupiter) Health(context.Context) error {
	if provider.client == nil || strings.TrimSpace(provider.apiKey) == "" {
		return errors.New("Jupiter quote provider is not configured")
	}
	return nil
}

func (provider *Jupiter) Quote(ctx context.Context, inMint, outMint string, amount uint64) (domain.QuoteEvidence, error) {
	if provider.client == nil || strings.TrimSpace(provider.apiKey) == "" {
		return domain.QuoteEvidence{}, errors.New("Jupiter quote provider is not configured")
	}
	if strings.TrimSpace(inMint) == "" || strings.TrimSpace(outMint) == "" || amount == 0 {
		return domain.QuoteEvidence{}, errors.New("Jupiter quote requires non-empty mints and a positive amount")
	}

	query := url.Values{
		"inputMint":                  {inMint},
		"outputMint":                 {outMint},
		"amount":                     {strconv.FormatUint(amount, 10)},
		"slippageBps":                {"500"},
		"restrictIntermediateTokens": {"true"},
		"instructionVersion":         {"V2"},
	}
	var response jupiterQuoteResponse
	if err := provider.client.GetJSONWithHeaders(ctx, "/swap/v1/quote", query, http.Header{"X-API-Key": []string{provider.apiKey}}, &response); err != nil {
		if jupiterNoRouteStatus(err) {
			return domain.QuoteEvidence{}, fmt.Errorf("%w: Jupiter quote request was not executable", domain.ErrNoExecutableRoute)
		}
		return domain.QuoteEvidence{}, fmt.Errorf("fetch Jupiter quote: %w", err)
	}
	quote, err := response.normalize(inMint, outMint, amount)
	if err != nil {
		return domain.QuoteEvidence{}, err
	}
	quote.ObservedAt = time.Now().UTC()
	if err := quote.Validate(); err != nil {
		return domain.QuoteEvidence{}, fmt.Errorf("validate normalized Jupiter quote: %w", err)
	}
	return quote, nil
}

type jupiterQuoteResponse struct {
	InputMint      string `json:"inputMint"`
	OutputMint     string `json:"outputMint"`
	InAmount       string `json:"inAmount"`
	OutAmount      string `json:"outAmount"`
	PriceImpactPct string `json:"priceImpactPct"`
	PlatformFee    *struct {
		Amount string `json:"amount"`
	} `json:"platformFee"`
	RoutePlan []struct {
		SwapInfo struct {
			AMMKey    string `json:"ammKey"`
			Label     string `json:"label"`
			FeeAmount string `json:"feeAmount"`
			FeeMint   string `json:"feeMint"`
		} `json:"swapInfo"`
	} `json:"routePlan"`
}

func (response jupiterQuoteResponse) normalize(inMint, outMint string, amount uint64) (domain.QuoteEvidence, error) {
	if response.InputMint != inMint || response.OutputMint != outMint {
		return domain.QuoteEvidence{}, errors.New("Jupiter quote returned mismatched mints")
	}
	inAmount, err := strconv.ParseUint(response.InAmount, 10, 64)
	if err != nil || inAmount != amount {
		return domain.QuoteEvidence{}, errors.New("Jupiter quote returned mismatched input amount")
	}
	outAmount, err := strconv.ParseUint(response.OutAmount, 10, 64)
	if err != nil {
		return domain.QuoteEvidence{}, errors.New("Jupiter quote returned invalid output amount")
	}
	if outAmount == 0 {
		return domain.QuoteEvidence{}, fmt.Errorf("%w: Jupiter quote returned zero output", domain.ErrNoExecutableRoute)
	}
	impactBPS, err := parseFractionalBPS(response.PriceImpactPct)
	if err != nil {
		return domain.QuoteEvidence{}, fmt.Errorf("parse Jupiter price impact: %w", err)
	}
	platformFee := domain.QuoteFee{}
	if response.PlatformFee != nil {
		platformFee, err = parseQuoteFee(response.PlatformFee.Amount, outMint)
		if err != nil {
			return domain.QuoteEvidence{}, fmt.Errorf("parse Jupiter platform fee: %w", err)
		}
	}
	route := make([]domain.RouteLeg, 0, len(response.RoutePlan))
	for _, leg := range response.RoutePlan {
		if strings.TrimSpace(leg.SwapInfo.AMMKey) == "" {
			return domain.QuoteEvidence{}, fmt.Errorf("%w: Jupiter quote route is missing an AMM key", domain.ErrNoExecutableRoute)
		}
		fee, err := parseQuoteFee(leg.SwapInfo.FeeAmount, leg.SwapInfo.FeeMint)
		if err != nil {
			return domain.QuoteEvidence{}, fmt.Errorf("parse Jupiter route fee: %w", err)
		}
		route = append(route, domain.RouteLeg{AMMKey: leg.SwapInfo.AMMKey, Label: leg.SwapInfo.Label, Fee: fee})
	}
	if len(route) == 0 {
		return domain.QuoteEvidence{}, fmt.Errorf("%w: Jupiter quote route is empty", domain.ErrNoExecutableRoute)
	}
	return domain.QuoteEvidence{InputMint: inMint, OutputMint: outMint, InAmount: inAmount, OutAmount: outAmount, PriceImpactBPS: impactBPS, PlatformFee: platformFee, RoutePlan: route}, nil
}

func jupiterNoRouteStatus(err error) bool {
	var statusError StatusError
	return errors.As(err, &statusError) && (statusError.StatusCode == http.StatusBadRequest || statusError.StatusCode == http.StatusNotFound || statusError.StatusCode == http.StatusUnprocessableEntity)
}

func parseQuoteFee(amount, mint string) (domain.QuoteFee, error) {
	if strings.TrimSpace(amount) == "" {
		return domain.QuoteFee{}, nil
	}
	parsed, err := strconv.ParseUint(amount, 10, 64)
	if err != nil {
		return domain.QuoteFee{}, errors.New("fee amount must be an unsigned integer")
	}
	fee := domain.QuoteFee{Amount: parsed, Mint: mint}
	if fee.Amount > 0 && strings.TrimSpace(fee.Mint) == "" {
		return domain.QuoteFee{}, errors.New("positive fee is missing its mint")
	}
	return fee, nil
}

func parseFractionalBPS(value string) (int64, error) {
	ratio, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok || ratio.Sign() < 0 {
		return 0, errors.New("price impact must be a non-negative decimal")
	}
	ratio.Mul(ratio, big.NewRat(10_000, 1))
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(ratio.Num(), ratio.Denom(), remainder)
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, errors.New("price impact is out of range")
	}
	return quotient.Int64(), nil
}

var _ ports.QuoteProvider = (*Jupiter)(nil)
