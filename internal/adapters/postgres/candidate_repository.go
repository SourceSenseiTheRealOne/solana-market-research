package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/botstate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/ports"
)

const discoveryStatePrefix = "discovery:"

type CandidateRepository struct {
	client *ent.Client
}

func NewCandidateRepository(client *ent.Client) (*CandidateRepository, error) {
	if client == nil {
		return nil, errors.New("candidate repository requires an Ent client")
	}
	return &CandidateRepository{client: client}, nil
}

func (repository *CandidateRepository) InsertDiscovered(ctx context.Context, pools []domain.DiscoveredPool) (int, error) {
	unique, err := validatedUniquePools(pools)
	if err != nil {
		return 0, err
	}
	if len(unique) == 0 {
		return 0, nil
	}

	tx, err := repository.client.Tx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin candidate transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	inserted := 0
	for _, pool := range unique {
		exists, err := tx.Candidate.Query().Where(
			candidate.NetworkEQ(pool.Network),
			candidate.MintAddressEQ(pool.MintAddress),
			candidate.PoolAddressEQ(pool.PoolAddress),
		).Exist(ctx)
		if err != nil {
			return 0, fmt.Errorf("check candidate identity: %w", err)
		}
		if exists {
			continue
		}

		if err := tx.Candidate.Create().
			SetNetwork(pool.Network).
			SetMintAddress(pool.MintAddress).
			SetPoolAddress(pool.PoolAddress).
			SetDiscoveredAt(pool.CreatedAt).
			OnConflictColumns(candidate.FieldNetwork, candidate.FieldMintAddress, candidate.FieldPoolAddress).
			DoNothing().
			Exec(ctx); err != nil {
			return 0, fmt.Errorf("insert discovered candidate: %w", err)
		}
		inserted++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit candidate transaction: %w", err)
	}
	return inserted, nil
}

func (repository *CandidateRepository) LoadWatermark(ctx context.Context, source string) (domain.Watermark, error) {
	key, err := discoveryStateKey(source, "watermark")
	if err != nil {
		return domain.Watermark{}, err
	}
	state, err := repository.client.BotState.Query().Where(botstate.StateKeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return domain.Watermark{}, nil
	}
	if err != nil {
		return domain.Watermark{}, fmt.Errorf("load discovery watermark: %w", err)
	}

	mark, err := watermarkFromState(state.Value)
	if err != nil {
		return domain.Watermark{}, fmt.Errorf("decode discovery watermark: %w", err)
	}
	return mark, nil
}

func (repository *CandidateRepository) SaveWatermark(ctx context.Context, source string, mark domain.Watermark) error {
	key, err := discoveryStateKey(source, "watermark")
	if err != nil {
		return err
	}
	if mark.IsZero() {
		return errors.New("discovery watermark requires pool address and creation time")
	}
	value := map[string]any{
		"created_at":   mark.CreatedAt.UTC().Format(time.RFC3339Nano),
		"pool_address": mark.PoolAddress,
	}
	if err := repository.client.BotState.Create().
		SetStateKey(key).
		SetValue(value).
		OnConflictColumns(botstate.FieldStateKey).
		UpdateNewValues().
		Exec(ctx); err != nil {
		return fmt.Errorf("save discovery watermark: %w", err)
	}
	return nil
}

func (repository *CandidateRepository) RecordCoverageGap(ctx context.Context, source string, page int, reason string) error {
	key, err := discoveryStateKey(source, "coverage-gap")
	if err != nil {
		return err
	}
	if page < 1 || strings.TrimSpace(reason) == "" {
		return errors.New("coverage gap requires a positive page and reason")
	}
	value := map[string]any{
		"page":        page,
		"reason":      strings.TrimSpace(reason),
		"recorded_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := repository.client.BotState.Create().
		SetStateKey(key).
		SetValue(value).
		OnConflictColumns(botstate.FieldStateKey).
		UpdateNewValues().
		Exec(ctx); err != nil {
		return fmt.Errorf("save discovery coverage gap: %w", err)
	}
	return nil
}

func (repository *CandidateRepository) Save(ctx context.Context, pool domain.DiscoveredPool, evidence domain.CandidateEvidence, evaluation domain.CandidateEvaluation) error {
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("validate snapshot candidate: %w", err)
	}
	candidateRecord, err := repository.client.Candidate.Query().Where(
		candidate.NetworkEQ(pool.Network),
		candidate.MintAddressEQ(pool.MintAddress),
		candidate.PoolAddressEQ(pool.PoolAddress),
	).Only(ctx)
	if err != nil {
		return fmt.Errorf("load snapshot candidate: %w", err)
	}
	marketEvidence := candidateMarketEvidence(evidence, evaluation)
	if err := repository.client.CandidateSnapshot.Create().SetCandidateID(candidateRecord.ID).SetObservedAt(evidence.MarketObservedAt.UTC()).SetMarketEvidence(marketEvidence).Exec(ctx); err != nil {
		return fmt.Errorf("save candidate snapshot: %w", err)
	}
	return nil
}

func candidateMarketEvidence(evidence domain.CandidateEvidence, evaluation domain.CandidateEvaluation) map[string]any {
	rules := make([]map[string]any, 0, len(evaluation.Rules))
	for _, rule := range evaluation.Rules {
		rules = append(rules, map[string]any{"code": rule.Code, "passed": rule.Passed, "observed": rule.Observed, "limit": rule.Limit})
	}
	return map[string]any{
		"source_observed_at":            evidence.MarketObservedAt.UTC().Format(time.RFC3339Nano),
		"received_at":                   evidence.ReceivedAt.UTC().Format(time.RFC3339Nano),
		"liquidity_usd_micros":          evidence.LiquidityUSD.Micros,
		"five_minute_transactions":      evidence.FiveMinuteTransactions,
		"five_minute_buys":              evidence.FiveMinuteBuys,
		"five_minute_sells":             evidence.FiveMinuteSells,
		"five_minute_volume_usd_micros": evidence.FiveMinuteVolumeUSD.Micros,
		"five_minute_price_change_bps":  evidence.FiveMinutePriceChangeBPS,
		"token_program":                 string(evidence.Token.Program),
		"token_extensions":              tokenExtensions(evidence.Token.Extensions),
		"mint_authority_revoked":        evidence.Token.MintAuthorityRevoked,
		"freeze_authority_revoked":      evidence.Token.FreezeAuthorityRevoked,
		"entry_quote":                   quoteEvidence(evidence.EntryQuote),
		"exit_quote":                    quoteEvidence(evidence.ExitQuote),
		"eligible":                      evaluation.Eligible,
		"rules":                         rules,
	}
}

func (repository *CandidateRepository) FindID(ctx context.Context, pool domain.DiscoveredPool) (int, error) {
	if err := pool.Validate(); err != nil {
		return 0, fmt.Errorf("validate candidate identity: %w", err)
	}
	candidateRecord, err := repository.client.Candidate.Query().Where(
		candidate.NetworkEQ(pool.Network),
		candidate.MintAddressEQ(pool.MintAddress),
		candidate.PoolAddressEQ(pool.PoolAddress),
	).Only(ctx)
	if err != nil {
		return 0, fmt.Errorf("load candidate identity: %w", err)
	}
	return candidateRecord.ID, nil
}

func tokenExtensions(extensions []domain.TokenExtension) []string {
	values := make([]string, len(extensions))
	for index, extension := range extensions {
		values[index] = string(extension)
	}
	return values
}

func quoteEvidence(quote domain.QuoteEvidence) map[string]any {
	route := make([]map[string]any, 0, len(quote.RoutePlan))
	for _, leg := range quote.RoutePlan {
		route = append(route, map[string]any{"amm_key": leg.AMMKey, "label": leg.Label})
	}
	return map[string]any{
		"input_mint":       quote.InputMint,
		"output_mint":      quote.OutputMint,
		"in_amount":        quote.InAmount,
		"out_amount":       quote.OutAmount,
		"price_impact_bps": quote.PriceImpactBPS,
		"route":            route,
	}
}

func validatedUniquePools(pools []domain.DiscoveredPool) ([]domain.DiscoveredPool, error) {
	unique := make([]domain.DiscoveredPool, 0, len(pools))
	seen := make(map[string]struct{}, len(pools))
	for _, pool := range pools {
		if err := pool.Validate(); err != nil {
			return nil, fmt.Errorf("validate discovered pool: %w", err)
		}
		identity := pool.Identity()
		if _, exists := seen[identity]; exists {
			continue
		}
		seen[identity] = struct{}{}
		unique = append(unique, pool)
	}
	return unique, nil
}

func discoveryStateKey(source, suffix string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" || strings.ContainsAny(source, ":\t\n\r") {
		return "", errors.New("discovery source must be a non-empty key segment")
	}
	return discoveryStatePrefix + source + ":" + suffix, nil
}

func watermarkFromState(value map[string]any) (domain.Watermark, error) {
	createdAtRaw, ok := value["created_at"].(string)
	if !ok {
		return domain.Watermark{}, errors.New("watermark created_at is missing")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return domain.Watermark{}, fmt.Errorf("parse watermark created_at: %w", err)
	}
	poolAddress, ok := value["pool_address"].(string)
	if !ok || strings.TrimSpace(poolAddress) == "" {
		return domain.Watermark{}, errors.New("watermark pool_address is missing")
	}
	return domain.Watermark{CreatedAt: createdAt.UTC(), PoolAddress: poolAddress}, nil
}

var _ ports.CandidateRepository = (*CandidateRepository)(nil)
