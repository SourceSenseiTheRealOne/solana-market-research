package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/dailyresult"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/paperposition"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/tradedecision"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

const maximumDailyAdmissions = 30

var errDuplicateAdmission = errors.New("duplicate paper admission")

type AdmissionRepositoryOptions struct {
	Now                      func() time.Time
	MaxOpenPositions         int
	MaxDailyAdmissions       int
	MaxSerializationAttempts int
	StrategyVersion          string
}

type AdmissionRepository struct {
	client  *ent.Client
	options AdmissionRepositoryOptions
}

func NewAdmissionRepository(client *ent.Client, options AdmissionRepositoryOptions) (*AdmissionRepository, error) {
	if client == nil {
		return nil, errors.New("admission repository requires an Ent client")
	}
	if options.Now == nil || options.MaxOpenPositions < 1 || options.MaxOpenPositions > 3 || options.MaxDailyAdmissions < 1 || options.MaxDailyAdmissions > maximumDailyAdmissions || options.MaxSerializationAttempts < 1 || strings.TrimSpace(options.StrategyVersion) == "" {
		return nil, errors.New("admission repository options are invalid")
	}
	return &AdmissionRepository{client: client, options: options}, nil
}

func (repository *AdmissionRepository) Admit(ctx context.Context, input application.AdmissionInput) (application.AdmissionResult, error) {
	if repository == nil || repository.client == nil {
		return application.AdmissionResult{}, errors.New("admission repository is not configured")
	}

	for attempt := 0; attempt < repository.options.MaxSerializationAttempts; attempt++ {
		result, err := repository.admitOnce(ctx, input)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, errDuplicateAdmission) {
			return application.AdmissionResult{}, nil
		}
		if !isSerializationFailure(err) {
			return application.AdmissionResult{}, err
		}
	}
	return application.AdmissionResult{}, errors.New("paper admission exceeded serialization retry limit")
}

func (repository *AdmissionRepository) admitOnce(ctx context.Context, input application.AdmissionInput) (application.AdmissionResult, error) {
	tx, err := repository.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return application.AdmissionResult{}, fmt.Errorf("begin serializable paper admission: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := repository.options.Now().UTC()
	if now.IsZero() {
		return application.AdmissionResult{}, errors.New("admission repository clock returned zero time")
	}
	date := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := tx.DailyResult.Create().
		SetUtcDate(date).
		SetStrategyVersion(repository.options.StrategyVersion).
		SetDailyAdmittedCount(0).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		OnConflictColumns(dailyresult.FieldUtcDate, dailyresult.FieldStrategyVersion).
		Ignore().
		Exec(ctx); err != nil {
		return application.AdmissionResult{}, fmt.Errorf("create daily admission counter: %w", err)
	}
	counter, err := tx.DailyResult.Query().Where(
		dailyresult.UtcDateEQ(date),
		dailyresult.StrategyVersionEQ(repository.options.StrategyVersion),
	).Only(ctx)
	if err != nil {
		return application.AdmissionResult{}, fmt.Errorf("load daily admission counter: %w", err)
	}

	// This conditional update takes the daily counter row lock for the remainder
	// of the transaction. Any later rejection rolls it back, so quota is never
	// consumed without a persisted decision and honest PENDING position.
	updated, err := tx.DailyResult.Update().Where(
		dailyresult.IDEQ(counter.ID),
		dailyresult.DailyAdmittedCountLT(repository.options.MaxDailyAdmissions),
	).AddDailyAdmittedCount(1).SetUpdatedAt(now).Save(ctx)
	if err != nil {
		return application.AdmissionResult{}, fmt.Errorf("reserve daily paper admission: %w", err)
	}
	if updated == 0 {
		return application.AdmissionResult{}, nil
	}

	exists, err := tx.TradeDecision.Query().Where(tradedecision.IdempotencyKeyEQ(input.IdempotencyKey)).Exist(ctx)
	if err != nil {
		return application.AdmissionResult{}, fmt.Errorf("check paper admission idempotency: %w", err)
	}
	if exists {
		return application.AdmissionResult{}, errDuplicateAdmission
	}

	candidateExists, err := tx.Candidate.Query().Where(candidate.IDEQ(input.CandidateID)).Exist(ctx)
	if err != nil {
		return application.AdmissionResult{}, fmt.Errorf("check admission candidate: %w", err)
	}
	if !candidateExists {
		return application.AdmissionResult{}, errors.New("admission candidate does not exist")
	}

	active, err := tx.PaperPosition.Query().Where(
		paperposition.StateIn(paperposition.StatePENDING, paperposition.StateOPEN, paperposition.StateCLOSING),
	).Count(ctx)
	if err != nil {
		return application.AdmissionResult{}, fmt.Errorf("count active paper positions: %w", err)
	}
	if active >= repository.options.MaxOpenPositions {
		return application.AdmissionResult{}, nil
	}

	decision, err := tx.TradeDecision.Create().
		SetIdempotencyKey(input.IdempotencyKey).
		SetOutcome(string(domain.VerdictBuy)).
		SetRuleResults(admissionRuleResults(input)).
		SetCandidateID(input.CandidateID).
		SetCreatedAt(now).
		Save(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return application.AdmissionResult{}, errDuplicateAdmission
		}
		return application.AdmissionResult{}, fmt.Errorf("persist paper admission decision: %w", err)
	}
	position, err := tx.PaperPosition.Create().
		SetState(paperposition.StatePENDING).
		SetNotionalMicros(input.NotionalMicros).
		SetStrategyVersion(repository.options.StrategyVersion).
		SetCandidateID(input.CandidateID).
		SetDecisionID(decision.ID).
		SetCreatedAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	if err != nil {
		return application.AdmissionResult{}, fmt.Errorf("persist pending paper position: %w", err)
	}
	if err := tx.PositionEvent.Create().
		SetEventType(string(domain.PositionPending)).
		SetDetails(map[string]any{"idempotency_key": input.IdempotencyKey, "state": string(domain.PositionPending)}).
		SetPositionID(position.ID).
		SetCreatedAt(now).
		Exec(ctx); err != nil {
		return application.AdmissionResult{}, fmt.Errorf("persist pending paper position event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.AdmissionResult{}, fmt.Errorf("commit paper admission: %w", err)
	}
	return application.AdmissionResult{Admitted: true}, nil
}

func admissionRuleResults(input application.AdmissionInput) map[string]any {
	rules := make([]map[string]any, 0, len(input.Evaluation.Rules))
	for _, rule := range input.Evaluation.Rules {
		rules = append(rules, map[string]any{"code": rule.Code, "passed": rule.Passed, "observed": rule.Observed, "limit": rule.Limit})
	}
	return map[string]any{
		"deterministic_rules":  rules,
		"hermes":               map[string]any{"outcome": input.Verdict.Outcome, "confidence": input.Verdict.Confidence},
		"evidence_observed_at": input.EvidenceObservedAt.UTC().Format(time.RFC3339Nano),
		"entry_quote":          quoteEvidence(input.EntryQuote),
	}
}

func isSerializationFailure(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "40001"
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

var _ application.AdmissionRepository = (*AdmissionRepository)(nil)
