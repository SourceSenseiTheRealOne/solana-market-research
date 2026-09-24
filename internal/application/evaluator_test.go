package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestEvaluatorCollectsReciprocalEvidenceAndPersistsAllRules(t *testing.T) {
	now := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	market := &marketProvider{snapshot: domain.MarketSnapshot{ObservedAt: now.Add(-time.Minute), LiquidityUSD: domain.USD{Micros: 5_000_000_000}, FiveMinuteTransactions: 20, FiveMinuteBuys: 13, FiveMinuteSells: 7, FiveMinuteVolumeUSD: domain.USD{Micros: 750_000_000}, FiveMinutePriceChangeBPS: 200}}
	quotes := &quoteProvider{quotes: []domain.ExecutableQuote{
		{InputMint: "quote", OutputMint: "mint", InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 100, RoutePlan: []domain.RouteLeg{{AMMKey: "pool"}}},
		{InputMint: "mint", OutputMint: "quote", InAmount: 25_000_000, OutAmount: 9_000_000, PriceImpactBPS: 100, RoutePlan: []domain.RouteLeg{{AMMKey: "pool"}}},
	}}
	repository := &snapshotRepository{}
	evaluator := application.NewEvaluator(application.EvaluatorOptions{
		Now: now,
		Policy: domain.CandidatePolicy{
			MaxPoolAge: 30 * time.Minute, MinLiquidityUSD: domain.USD{Micros: 5_000_000_000}, MinFiveMinuteTransactions: 20,
			MinFiveMinuteBuyShareBPS: 6_500, MinFiveMinuteTurnoverBPS: 1_500,
			MinFiveMinutePriceChangeBPS: 200, MaxFiveMinutePriceChangeBPS: 6_000, MaxEntryPriceImpactBPS: 500,
		},
		QuoteMint: "quote", EntryInputAmount: 10_000_000, Market: market, Token: tokenInspector{}, Quotes: quotes, Snapshots: repository,
	})
	pool := domain.DiscoveredPool{Source: "test", Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: now.Add(-5 * time.Minute)}

	result, err := evaluator.Evaluate(context.Background(), pool)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !result.Eligible || repository.saved != 1 || len(repository.evidence.EntryQuote.RoutePlan) != 1 || len(result.Rules) != 12 {
		t.Fatalf("result = %#v, saved = %#v", result, repository)
	}
	if repository.evidence.FiveMinuteBuys != 13 || repository.evidence.FiveMinuteSells != 7 || repository.evidence.FiveMinuteVolumeUSD.Micros != 750_000_000 || repository.evidence.FiveMinutePriceChangeBPS != 200 {
		t.Fatalf("persisted evaluator evidence = %#v", repository.evidence)
	}
	if got, want := quotes.calls, [][3]string{{"quote", "mint", "10000000"}, {"mint", "quote", "25000000"}}; !equalQuoteCalls(got, want) {
		t.Fatalf("quote calls = %v, want %v", got, want)
	}
}

type marketProvider struct{ snapshot domain.MarketSnapshot }

func (p *marketProvider) Fetch(context.Context, domain.DiscoveredPool) (domain.MarketSnapshot, error) {
	return p.snapshot, nil
}

type tokenInspector struct{}

func (tokenInspector) Inspect(context.Context, string) (domain.TokenRiskSnapshot, error) {
	return domain.TokenRiskSnapshot{Program: domain.TokenProgramLegacy, MintAuthorityRevoked: true, FreezeAuthorityRevoked: true}, nil
}

type quoteProvider struct {
	quotes []domain.ExecutableQuote
	calls  [][3]string
}

func (p *quoteProvider) Quote(_ context.Context, in, out string, amount uint64) (domain.ExecutableQuote, error) {
	p.calls = append(p.calls, [3]string{in, out, itoa(amount)})
	q := p.quotes[0]
	p.quotes = p.quotes[1:]
	return q, nil
}
func (p *quoteProvider) Health(context.Context) error { return nil }

type snapshotRepository struct {
	saved    int
	evidence domain.CandidateEvidence
}

func (r *snapshotRepository) Save(_ context.Context, _ domain.DiscoveredPool, evidence domain.CandidateEvidence, _ domain.CandidateEvaluation) error {
	r.saved++
	r.evidence = evidence
	return nil
}
func equalQuoteCalls(a, b [][3]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func itoa(value uint64) string {
	if value == 10_000_000 {
		return "10000000"
	}
	if value == 25_000_000 {
		return "25000000"
	}
	return "unexpected"
}
