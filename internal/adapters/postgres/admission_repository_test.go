package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/dailyresult"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/positionevent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/tradedecision"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestAdmissionRepositoryIsIdempotentAndPersistsAnHonestPendingPosition(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	strategyVersion := admissionTestStrategyVersion("idempotent")
	repository := newAdmissionRepository(t, client, now, strategyVersion)
	storedCandidate := createAdmissionCandidate(t, client, "idempotent", now)
	service := newAdmissionService(repository, now)
	input := validRepositoryAdmissionInput(storedCandidate.ID, storedCandidate.MintAddress, now, "idempotent-key")

	first, err := service.Admit(context.Background(), input)
	if err != nil {
		t.Fatalf("first Admit() error = %v", err)
	}
	if !first.Admitted {
		t.Fatal("first Admit() = not admitted, want admitted")
	}
	second, err := service.Admit(context.Background(), input)
	if err != nil {
		t.Fatalf("second Admit() error = %v", err)
	}
	if second.Admitted {
		t.Fatal("second Admit() admitted a duplicate idempotency key")
	}

	if got, err := client.TradeDecision.Query().Where(tradedecision.IdempotencyKeyEQ(input.IdempotencyKey)).Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("stored decisions = %d, %v; want 1, nil", got, err)
	}
	position, err := client.PaperPosition.Query().Where(paperposition.HasCandidateWith(candidate.IDEQ(storedCandidate.ID))).Only(context.Background())
	if err != nil {
		t.Fatalf("query pending position: %v", err)
	}
	if position.State != paperposition.StatePENDING || position.StrategyVersion != strategyVersion || position.EntryPrice != nil || position.TokenQuantity != nil {
		t.Fatalf("position = %#v; want honest PENDING position without fill values", position)
	}
	if got, err := client.PositionEvent.Query().Where(positionevent.HasPositionWith(paperposition.IDEQ(position.ID)), positionevent.EventTypeEQ(string(domain.PositionPending))).Count(context.Background()); err != nil || got != 1 {
		t.Fatalf("PENDING position events = %d, %v; want 1, nil", got, err)
	}
	assertAdmissionDailyCount(t, client, now, strategyVersion, 1)
}

func TestAdmissionRepositoryAtomicallyCapsConcurrentAdmissions(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	strategyVersion := admissionTestStrategyVersion("concurrent")
	repository := newAdmissionRepository(t, client, now, strategyVersion)
	service := newAdmissionService(repository, now)
	initialActive, err := client.PaperPosition.Query().Where(paperposition.StateIn(paperposition.StatePENDING, paperposition.StateOPEN, paperposition.StateCLOSING)).Count(context.Background())
	if err != nil {
		t.Fatalf("count initial active paper positions: %v", err)
	}
	if initialActive > 3 {
		t.Fatalf("initial active paper positions = %d, want <= 3", initialActive)
	}

	const attempts = 40
	inputs := make([]application.AdmissionInput, attempts)
	for index := range inputs {
		storedCandidate := createAdmissionCandidate(t, client, fmt.Sprintf("concurrent-%d", index), now)
		inputs[index] = validRepositoryAdmissionInput(storedCandidate.ID, storedCandidate.MintAddress, now, fmt.Sprintf("concurrent-key-%d", index))
	}

	start := make(chan struct{})
	results := make(chan application.AdmissionResult, attempts)
	errors := make(chan error, attempts)
	var workers sync.WaitGroup
	for _, input := range inputs {
		workers.Add(1)
		go func(input application.AdmissionInput) {
			defer workers.Done()
			<-start
			result, err := service.Admit(context.Background(), input)
			if err != nil {
				errors <- err
				return
			}
			results <- result
		}(input)
	}
	close(start)
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent Admit() error = %v", err)
	}

	admitted := 0
	for result := range results {
		if result.Admitted {
			admitted++
		}
	}
	if got, want := admitted, 3-initialActive; got != want {
		t.Fatalf("admitted = %d, want %d remaining active-position slots", got, want)
	}
	active, err := client.PaperPosition.Query().Where(paperposition.StateIn(paperposition.StatePENDING, paperposition.StateOPEN, paperposition.StateCLOSING)).Count(context.Background())
	if err != nil {
		t.Fatalf("count active paper positions: %v", err)
	}
	if active != 3 {
		t.Fatalf("active paper positions = %d, want cap of 3", active)
	}
	assertAdmissionDailyCount(t, client, now, strategyVersion, admitted)
}

func TestAdmissionRepositoryRejectsDailyQuotaWithoutPersistingDecisionOrPosition(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()

	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	strategyVersion := admissionTestStrategyVersion("quota")
	if err := client.DailyResult.Create().SetUtcDate(date).SetStrategyVersion(strategyVersion).SetDailyAdmittedCount(30).Exec(context.Background()); err != nil {
		t.Fatalf("seed daily quota: %v", err)
	}

	repository := newAdmissionRepository(t, client, now, strategyVersion)
	storedCandidate := createAdmissionCandidate(t, client, "quota", now)
	input := validRepositoryAdmissionInput(storedCandidate.ID, storedCandidate.MintAddress, now, "quota-key")
	result, err := newAdmissionService(repository, now).Admit(context.Background(), input)
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if result.Admitted {
		t.Fatal("Admit() exceeded the daily quota")
	}
	if got, err := client.TradeDecision.Query().Where(tradedecision.IdempotencyKeyEQ(input.IdempotencyKey)).Count(context.Background()); err != nil || got != 0 {
		t.Fatalf("stored decisions = %d, %v; want 0, nil", got, err)
	}
	if got, err := client.PaperPosition.Query().Where(paperposition.HasCandidateWith(candidate.IDEQ(storedCandidate.ID))).Count(context.Background()); err != nil || got != 0 {
		t.Fatalf("stored positions = %d, %v; want 0, nil", got, err)
	}
	assertAdmissionDailyCount(t, client, now, strategyVersion, 30)
}

func newAdmissionRepository(t *testing.T, client *ent.Client, now time.Time, strategyVersion string) application.AdmissionRepository {
	t.Helper()

	repository, err := postgres.NewAdmissionRepository(client, postgres.AdmissionRepositoryOptions{
		Now:                      func() time.Time { return now },
		MaxOpenPositions:         3,
		MaxDailyAdmissions:       30,
		MaxSerializationAttempts: 5,
		StrategyVersion:          strategyVersion,
	})
	if err != nil {
		t.Fatalf("NewAdmissionRepository() error = %v", err)
	}
	return repository
}

func newAdmissionService(repository application.AdmissionRepository, now time.Time) *application.Admission {
	return application.NewAdmission(application.AdmissionOptions{
		Now:                    now,
		VerdictPolicy:          domain.VerdictPolicy{MinimumConfidence: 70, MinimumHypeQuality: 60, MaximumManipulationProbability: 35},
		MaxEvidenceAge:         15 * time.Minute,
		MaxEntryPriceImpactBPS: 500,
		Repository:             repository,
	})
}

func createAdmissionCandidate(t *testing.T, client *ent.Client, suffix string, now time.Time) *ent.Candidate {
	t.Helper()

	storedCandidate, err := client.Candidate.Create().
		SetNetwork(string(domain.NetworkSolana)).
		SetMintAddress("admission-" + suffix + "-mint").
		SetPoolAddress("admission-" + suffix + "-pool").
		SetDiscoveredAt(now.Add(-time.Hour)).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create admission candidate %q: %v", suffix, err)
	}
	return storedCandidate
}

func validRepositoryAdmissionInput(candidateID int, mintAddress string, now time.Time, idempotencyKey string) application.AdmissionInput {
	return application.AdmissionInput{
		Evaluation:         domain.CandidateEvaluation{Eligible: true, Rules: []domain.RuleResult{{Code: domain.RuleLiquidity, Passed: true, Observed: "$20000", Limit: ">=$20000"}}},
		Verdict:            domain.Verdict{Outcome: domain.VerdictBuy, Confidence: 70, HypeQualityScore: 60, ManipulationProbability: 35},
		EvidenceObservedAt: now.Add(-time.Minute),
		MintAddress:        mintAddress,
		QuoteMint:          "quote-mint",
		EntryInputAmount:   10_000_000,
		EntryQuote:         domain.QuoteEvidence{InputMint: "quote-mint", OutputMint: mintAddress, InAmount: 10_000_000, OutAmount: 25_000_000, PriceImpactBPS: 100, RoutePlan: []domain.RouteLeg{{AMMKey: "route"}}},
		IdempotencyKey:     idempotencyKey,
		CandidateID:        candidateID,
		NotionalMicros:     10_000_000,
	}
}

func assertAdmissionDailyCount(t *testing.T, client *ent.Client, now time.Time, strategyVersion string, want int) {
	t.Helper()

	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	stored, err := client.DailyResult.Query().Where(dailyresult.UtcDateEQ(date), dailyresult.StrategyVersionEQ(strategyVersion)).Only(context.Background())
	if err != nil {
		t.Fatalf("query admission daily counter: %v", err)
	}
	if stored.DailyAdmittedCount != want || stored.DailyAdmittedCount > 30 {
		t.Fatalf("daily admitted count = %d, want %d and <= 30", stored.DailyAdmittedCount, want)
	}
}

func admissionTestStrategyVersion(name string) string {
	return fmt.Sprintf("admission-%s-%d", name, time.Now().UTC().UnixNano())
}
