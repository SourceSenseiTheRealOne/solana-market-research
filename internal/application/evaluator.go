package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type MarketProvider interface {
	Fetch(context.Context, domain.DiscoveredPool) (domain.MarketSnapshot, error)
}
type SnapshotRepository interface {
	Save(context.Context, domain.DiscoveredPool, domain.CandidateEvidence, domain.CandidateEvaluation) error
}
type QuoteProvider interface {
	Quote(context.Context, string, string, uint64) (domain.ExecutableQuote, error)
}
type TokenInspector interface {
	Inspect(context.Context, string) (domain.TokenRiskSnapshot, error)
}

type EvaluatorOptions struct {
	Now              time.Time
	Policy           domain.CandidatePolicy
	QuoteMint        string
	EntryInputAmount uint64
	Market           MarketProvider
	Token            TokenInspector
	Quotes           QuoteProvider
	Snapshots        SnapshotRepository
}

type Evaluator struct{ options EvaluatorOptions }

func NewEvaluator(options EvaluatorOptions) *Evaluator { return &Evaluator{options: options} }
func (e *Evaluator) Evaluate(ctx context.Context, pool domain.DiscoveredPool) (domain.CandidateEvaluation, error) {
	if e.options.Market == nil || e.options.Token == nil || e.options.Quotes == nil || e.options.Snapshots == nil || e.options.Now.IsZero() || e.options.QuoteMint == "" || e.options.EntryInputAmount == 0 {
		return domain.CandidateEvaluation{}, errors.New("evaluator is not completely configured")
	}
	if err := pool.Validate(); err != nil {
		return domain.CandidateEvaluation{}, fmt.Errorf("validate candidate: %w", err)
	}
	market, err := e.options.Market.Fetch(ctx, pool)
	if err != nil {
		return domain.CandidateEvaluation{}, fmt.Errorf("fetch market evidence: %w", newMarketEvidenceUnavailableError(err))
	}
	token, err := e.options.Token.Inspect(ctx, pool.MintAddress)
	if err != nil {
		return domain.CandidateEvaluation{}, fmt.Errorf("inspect token: %w", err)
	}
	entry, err := e.options.Quotes.Quote(ctx, e.options.QuoteMint, pool.MintAddress, e.options.EntryInputAmount)
	if err != nil {
		if errors.Is(err, domain.ErrNoExecutableRoute) {
			return e.evaluateAndSave(ctx, pool, market, token, domain.QuoteEvidence{}, domain.QuoteEvidence{})
		}
		return domain.CandidateEvaluation{}, fmt.Errorf("quote entry: %w", err)
	}
	exit, err := e.options.Quotes.Quote(ctx, pool.MintAddress, e.options.QuoteMint, entry.OutAmount)
	if err != nil {
		if errors.Is(err, domain.ErrNoExecutableRoute) {
			return e.evaluateAndSave(ctx, pool, market, token, entry, domain.QuoteEvidence{})
		}
		return domain.CandidateEvaluation{}, fmt.Errorf("quote exit: %w", err)
	}
	return e.evaluateAndSave(ctx, pool, market, token, entry, exit)
}

func (e *Evaluator) evaluateAndSave(ctx context.Context, pool domain.DiscoveredPool, market domain.MarketSnapshot, token domain.TokenRiskSnapshot, entry, exit domain.QuoteEvidence) (domain.CandidateEvaluation, error) {
	evidence := domain.CandidateEvidence{
		MintAddress: pool.MintAddress, QuoteMint: e.options.QuoteMint, PoolCreatedAt: pool.CreatedAt,
		MarketObservedAt: market.ObservedAt, ReceivedAt: e.options.Now, LiquidityUSD: market.LiquidityUSD,
		FiveMinuteTransactions: market.FiveMinuteTransactions, FiveMinuteBuys: market.FiveMinuteBuys,
		FiveMinuteSells: market.FiveMinuteSells, FiveMinuteVolumeUSD: market.FiveMinuteVolumeUSD,
		FiveMinutePriceChangeBPS: market.FiveMinutePriceChangeBPS, EntryInputAmount: e.options.EntryInputAmount,
		Token: token, EntryQuote: entry, ExitQuote: exit,
	}
	result := e.options.Policy.Evaluate(e.options.Now, evidence)
	if err := e.options.Snapshots.Save(ctx, pool, evidence, result); err != nil {
		return domain.CandidateEvaluation{}, fmt.Errorf("save candidate snapshot: %w", err)
	}
	return result, nil
}
