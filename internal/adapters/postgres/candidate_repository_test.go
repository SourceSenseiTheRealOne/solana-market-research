package postgres_test

import (
	"context"
	"fmt"

	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/apiusage"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/botstate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidatesnapshot"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/socialsnapshot"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/verdict"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/testsupport"
)

func TestCandidateRepositoryPersistsIdempotentlyAndRoundTripsDiscoveryState(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	repository, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}

	unique := fmt.Sprintf("candidate-repository-%d", time.Now().UTC().UnixNano())
	pool := domain.DiscoveredPool{
		Source:      domain.SourceGeckoTerminal,
		Network:     domain.NetworkSolana,
		MintAddress: unique + "-mint",
		PoolAddress: unique + "-pool",
		CreatedAt:   time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
	}

	inserted, err := repository.InsertDiscovered(context.Background(), []domain.DiscoveredPool{pool})
	if err != nil {
		t.Fatalf("first InsertDiscovered() error = %v", err)
	}
	if got, want := inserted, 1; got != want {
		t.Fatalf("first inserted = %d, want %d", got, want)
	}

	inserted, err = repository.InsertDiscovered(context.Background(), []domain.DiscoveredPool{pool})
	if err != nil {
		t.Fatalf("second InsertDiscovered() error = %v", err)
	}
	if got, want := inserted, 0; got != want {
		t.Fatalf("second inserted = %d, want %d", got, want)
	}

	stored, err := client.Candidate.Query().Where(candidate.PoolAddressEQ(pool.PoolAddress)).Only(context.Background())
	if err != nil {
		t.Fatalf("query stored candidate: %v", err)
	}
	if got, want := stored.DiscoveredAt, pool.CreatedAt; !got.Equal(want) {
		t.Fatalf("stored discovered_at = %s, want %s", got, want)
	}

	mark := domain.Watermark{CreatedAt: pool.CreatedAt, PoolAddress: pool.PoolAddress}
	if err := repository.SaveWatermark(context.Background(), unique, mark); err != nil {
		t.Fatalf("SaveWatermark() error = %v", err)
	}
	loaded, err := repository.LoadWatermark(context.Background(), unique)
	if err != nil {
		t.Fatalf("LoadWatermark() error = %v", err)
	}
	if loaded != mark {
		t.Fatalf("loaded watermark = %#v, want %#v", loaded, mark)
	}

	if err := repository.RecordCoverageGap(context.Background(), unique, 3, "page budget exhausted"); err != nil {
		t.Fatalf("RecordCoverageGap() error = %v", err)
	}
	coverage, err := client.BotState.Query().Where(botstate.StateKeyEQ("discovery:" + unique + ":coverage-gap")).Only(context.Background())
	if err != nil {
		t.Fatalf("query coverage gap: %v", err)
	}
	if got, want := coverage.Value["page"], float64(3); got != want {
		t.Fatalf("coverage page = %#v, want %#v", got, want)
	}
	if got, want := coverage.Value["reason"], "page budget exhausted"; got != want {
		t.Fatalf("coverage reason = %#v, want %#v", got, want)
	}
}

func TestCandidateRepositoryRejectsMixedInvalidBatchWithoutPersisting(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	repository, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}

	unique := fmt.Sprintf("candidate-repository-invalid-%d", time.Now().UTC().UnixNano())
	valid := domain.DiscoveredPool{
		Source:      domain.SourceGeckoTerminal,
		Network:     domain.NetworkSolana,
		MintAddress: unique + "-mint",
		PoolAddress: unique + "-pool",
		CreatedAt:   time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
	}
	invalid := valid
	invalid.MintAddress = ""

	if _, err := repository.InsertDiscovered(context.Background(), []domain.DiscoveredPool{valid, invalid}); err == nil {
		t.Fatal("InsertDiscovered() accepted a mixed invalid batch")
	}
	count, err := client.Candidate.Query().Where(candidate.PoolAddressEQ(valid.PoolAddress)).Count(context.Background())
	if err != nil {
		t.Fatalf("count valid candidate after rejected batch: %v", err)
	}
	if got, want := count, 0; got != want {
		t.Fatalf("stored candidates after rejected batch = %d, want %d", got, want)
	}
}

func TestPendingPositionDoesNotPersistInventedFillValues(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	unique := fmt.Sprintf("pending-position-%d", time.Now().UTC().UnixNano())
	storedCandidate, err := client.Candidate.Create().SetNetwork(string(domain.NetworkSolana)).SetMintAddress(unique + "-mint").SetPoolAddress(unique + "-pool").SetDiscoveredAt(time.Now().UTC()).Save(context.Background())
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	decision, err := client.TradeDecision.Create().SetIdempotencyKey(unique + "-decision").SetOutcome(string(domain.VerdictBuy)).SetRuleResults(map[string]any{"source": "test"}).SetCandidate(storedCandidate).Save(context.Background())
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	position, err := client.PaperPosition.Create().SetState(paperposition.StatePENDING).SetNotionalMicros(10_000_000).SetCandidate(storedCandidate).SetDecision(decision).Save(context.Background())
	if err != nil {
		t.Fatalf("create pending position: %v", err)
	}
	if position.EntryPrice != nil || position.TokenQuantity != nil {
		t.Fatalf("pending fill fields = %#v, %#v; want nil", position.EntryPrice, position.TokenQuantity)
	}
}

func TestCandidateRepositoryPersistsAuditableEvaluationSnapshot(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()
	repository, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	unique := fmt.Sprintf("evaluation-snapshot-%d", time.Now().UTC().UnixNano())
	pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: unique + "-mint", PoolAddress: unique + "-pool", CreatedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	if _, err := repository.InsertDiscovered(context.Background(), []domain.DiscoveredPool{pool}); err != nil {
		t.Fatalf("InsertDiscovered() error = %v", err)
	}
	evidence := domain.CandidateEvidence{MintAddress: pool.MintAddress, QuoteMint: "quote", PoolCreatedAt: pool.CreatedAt, MarketObservedAt: pool.CreatedAt.Add(5 * time.Minute), ReceivedAt: pool.CreatedAt.Add(5*time.Minute + time.Second), LiquidityUSD: domain.USD{Micros: 20_000_000_000}, FiveMinuteTransactions: 20, EntryInputAmount: 10_000_000, Token: domain.TokenSafety{Program: domain.TokenProgram2022, MintAuthorityRevoked: true, FreezeAuthorityRevoked: true, Extensions: []domain.TokenExtension{"TransferFeeConfig"}}, EntryQuote: domain.QuoteEvidence{InputMint: "quote", OutputMint: pool.MintAddress, InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 100, RoutePlan: []domain.RouteLeg{{AMMKey: pool.PoolAddress}}}, ExitQuote: domain.QuoteEvidence{InputMint: pool.MintAddress, OutputMint: "quote", InAmount: 25_000_000, OutAmount: 9_000_000, PriceImpactBPS: 100, RoutePlan: []domain.RouteLeg{{AMMKey: pool.PoolAddress}}}}
	evaluation := domain.CandidateEvaluation{Eligible: true, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: true, Observed: "$20000.00", Limit: ">=$20000.00"}, {Code: domain.RuleEntryRoute, Passed: true, Observed: "executable", Limit: "full-size executable route"}}}
	if err := repository.Save(context.Background(), pool, evidence, evaluation); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	snapshot, err := client.CandidateSnapshot.Query().Where(candidatesnapshot.HasCandidateWith(candidate.PoolAddressEQ(pool.PoolAddress))).Only(context.Background())
	if err != nil {
		t.Fatalf("query snapshot: %v", err)
	}
	if !snapshot.ObservedAt.Equal(evidence.MarketObservedAt) {
		t.Fatalf("observed_at = %s, want %s", snapshot.ObservedAt, evidence.MarketObservedAt)
	}
	if got, want := snapshot.MarketEvidence["source_observed_at"], evidence.MarketObservedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("source observed at = %#v, want %#v", got, want)
	}
	if got, want := snapshot.MarketEvidence["received_at"], evidence.ReceivedAt.Format(time.RFC3339Nano); got != want {
		t.Fatalf("received at = %#v, want %#v", got, want)
	}
	extensions, ok := snapshot.MarketEvidence["token_extensions"].([]any)
	if !ok || len(extensions) != 1 || extensions[0] != "TransferFeeConfig" {
		t.Fatalf("token extensions = %#v, want [TransferFeeConfig]", snapshot.MarketEvidence["token_extensions"])
	}
	if got := snapshot.MarketEvidence["entry_quote"].(map[string]any)["route"].([]any); len(got) != 1 {
		t.Fatalf("entry route = %#v, want one route leg", got)
	}
	if got := snapshot.MarketEvidence["rules"].([]any); len(got) != len(evaluation.Rules) {
		t.Fatalf("rules = %#v, want %d", got, len(evaluation.Rules))
	}
}

func TestSocialEvidenceRepositoryBoundsDailyUsageAndPersistsNormalizedSnapshot(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	candidates, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	repository, err := postgres.NewSocialEvidenceRepository(client)
	if err != nil {
		t.Fatalf("NewSocialEvidenceRepository() error = %v", err)
	}
	unique := fmt.Sprintf("social-evidence-%d", time.Now().UTC().UnixNano())
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: unique + "-mint", PoolAddress: unique + "-pool", CreatedAt: now.Add(-time.Hour)}
	if _, err := candidates.InsertDiscovered(context.Background(), []domain.DiscoveredPool{pool}); err != nil {
		t.Fatalf("InsertDiscovered() error = %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		reserved, err := repository.Reserve(context.Background(), now, 2)
		if err != nil || !reserved {
			t.Fatalf("Reserve() attempt %d = %t, %v; want true, nil", attempt+1, reserved, err)
		}
	}
	reserved, err := repository.Reserve(context.Background(), now, 2)
	if err != nil {
		t.Fatalf("third Reserve() error = %v", err)
	}
	if reserved {
		t.Fatal("third Reserve() = true, want false after daily limit")
	}

	analysis := application.SocialAnalysis{Window: domain.SocialWindow{MintAddress: pool.MintAddress, StartsAt: now.Add(-15 * time.Minute), EndsAt: now}, Posts: []domain.SocialPost{{ID: "post", AuthorID: "author", CreatedAt: now.Add(-time.Minute), Text: "normalized excerpt", Followers: 100, Likes: 2}}, Metrics: domain.SocialMetrics{Posts: 1, UniqueAuthors: 1}, Score: 17, RequestCount: 1, EstimatedCostUSD: domain.USD{Micros: 150}}
	analysis.Metrics.PostTextHashes = map[string]string{"post": "hash"}
	if err := repository.Save(context.Background(), pool, analysis); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	usageDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	usage, err := client.APIUsage.Query().Where(apiusage.ProviderEQ("twitterapiio"), apiusage.UtcDateEQ(usageDate)).Only(context.Background())
	if err != nil {
		t.Fatalf("query social usage: %v", err)
	}
	if got, want := usage.RequestCount, 2; got != want {
		t.Fatalf("request_count = %d, want %d", got, want)
	}
	snapshot, err := client.SocialSnapshot.Query().Where(socialsnapshot.HasCandidateWith(candidate.PoolAddressEQ(pool.PoolAddress))).Only(context.Background())
	if err != nil {
		t.Fatalf("query social snapshot: %v", err)
	}
	if got, want := snapshot.Score, 17; got != want {
		t.Fatalf("score = %d, want %d", got, want)
	}
	if got, want := snapshot.SocialEvidence["estimated_cost_usd_micros"], float64(150); got != want {
		t.Fatalf("estimated cost = %#v, want %#v", got, want)
	}
	posts, ok := snapshot.SocialEvidence["posts"].([]any)
	if !ok || len(posts) != 1 {
		t.Fatalf("normalized posts = %#v, want one post", snapshot.SocialEvidence["posts"])
	}
	hashes, ok := snapshot.SocialEvidence["post_text_hashes"].(map[string]any)
	if !ok || hashes["post"] != "hash" {
		t.Fatalf("post text hashes = %#v, want post hash", snapshot.SocialEvidence["post_text_hashes"])
	}
}

func TestVerdictRepositoryPersistsSanitizedAuditMetadata(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()
	candidates, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	repository, err := postgres.NewVerdictRepository(client)
	if err != nil {
		t.Fatalf("NewVerdictRepository() error = %v", err)
	}
	unique := fmt.Sprintf("verdict-%d", time.Now().UTC().UnixNano())
	pool := domain.DiscoveredPool{Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana, MintAddress: unique + "-mint", PoolAddress: unique + "-pool", CreatedAt: time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)}
	if _, err := candidates.InsertDiscovered(context.Background(), []domain.DiscoveredPool{pool}); err != nil {
		t.Fatalf("InsertDiscovered() error = %v", err)
	}
	record := postgres.VerdictRecord{Provider: "openai-codex", Model: "gpt-5.6-sol", PromptVersion: "v1", InputSHA256: strings.Repeat("a", 64), Latency: 24 * time.Millisecond, InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Verdict: domain.Verdict{Outcome: domain.VerdictBuy, Confidence: 80, HypeQualityScore: 70, ManipulationProbability: 20, Reasons: []string{"organic activity"}, RiskFlags: []string{"concentration"}, InvalidationConditions: []string{"liquidity falls"}, EvidencePostIDs: []string{"post-1"}}}
	if err := repository.Save(context.Background(), pool, record); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	stored, err := client.Verdict.Query().Where(verdict.HasCandidateWith(candidate.PoolAddressEQ(pool.PoolAddress))).Only(context.Background())
	if err != nil {
		t.Fatalf("query persisted verdict: %v", err)
	}
	if got, want := stored.Outcome, string(domain.VerdictBuy); got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if got, want := stored.Evidence["input_sha256"], record.InputSHA256; got != want {
		t.Fatalf("input hash = %#v, want %#v", got, want)
	}
	if got, want := stored.Evidence["latency_ms"], float64(24); got != want {
		t.Fatalf("latency = %#v, want %#v", got, want)
	}
	if got, want := stored.Evidence["usage"].(map[string]any)["total_tokens"], float64(15); got != want {
		t.Fatalf("usage total = %#v, want %#v", got, want)
	}
	response := stored.Evidence["response"].(map[string]any)
	if got, want := response["evidence_post_ids"].([]any)[0], "post-1"; got != want {
		t.Fatalf("evidence post ID = %#v, want %#v", got, want)
	}
	if _, leaked := stored.Evidence["input"]; leaked {
		t.Fatal("raw Hermes input was persisted")
	}
}

func openTestEntClient(t *testing.T) *ent.Client {
	t.Helper()

	databaseURL := testsupport.DatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := postgres.OpenEnt(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenEnt() error = %v", err)
	}
	return client
}
