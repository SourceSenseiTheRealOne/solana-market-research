package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/botstate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	dashboardActivityStateKey = "dashboard:automation-activity:v1"
	reviewedCandidatesStateKey = "dashboard:reviewed-candidates:v1"
)

type DashboardActivityRepository struct {
	client *ent.Client
	mu     sync.Mutex
}

func NewDashboardActivityRepository(client *ent.Client) (*DashboardActivityRepository, error) {
	if client == nil {
		return nil, errors.New("dashboard activity repository requires an Ent client")
	}
	return &DashboardActivityRepository{client: client}, nil
}

func (repository *DashboardActivityRepository) Append(ctx context.Context, activity domain.AutomationActivity) error {
	if err := activity.Validate(); err != nil {
		return fmt.Errorf("validate dashboard activity: %w", err)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()

	activities, err := repository.listActivities(ctx)
	if err != nil {
		return err
	}
	activities = prependBounded(activities, activity)
	if err := domain.ValidateDashboardActivity(activities); err != nil {
		return errors.New("dashboard activity projection is invalid")
	}
	return repository.save(ctx, dashboardActivityStateKey, activityState(activities))
}

func (repository *DashboardActivityRepository) List(ctx context.Context) ([]domain.AutomationActivity, error) {
	if repository == nil || repository.client == nil {
		return nil, errors.New("dashboard activity repository is not configured")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listActivities(ctx)
}

func (repository *DashboardActivityRepository) AppendReviewed(ctx context.Context, candidate domain.ReviewedCandidate) error {
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validate reviewed candidate: %w", err)
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()

	candidates, err := repository.listReviewed(ctx)
	if err != nil {
		return err
	}
	candidates = prependBounded(candidates, candidate)
	if err := domain.ValidateReviewedCandidates(candidates); err != nil {
		return errors.New("reviewed candidate projection is invalid")
	}
	return repository.save(ctx, reviewedCandidatesStateKey, reviewedState(candidates))
}

func (repository *DashboardActivityRepository) ListReviewed(ctx context.Context) ([]domain.ReviewedCandidate, error) {
	if repository == nil || repository.client == nil {
		return nil, errors.New("dashboard activity repository is not configured")
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.listReviewed(ctx)
}

func (repository *DashboardActivityRepository) listActivities(ctx context.Context) ([]domain.AutomationActivity, error) {
	value, found, err := repository.load(ctx, dashboardActivityStateKey)
	if err != nil || !found {
		return nil, err
	}
	items, err := stateItems(value)
	if err != nil {
		return nil, errors.New("dashboard activity projection is malformed")
	}
	activities := make([]domain.AutomationActivity, 0, len(items))
	for _, item := range items {
		activity, err := activityFromState(item)
		if err != nil {
			return nil, errors.New("dashboard activity projection is malformed")
		}
		activities = append(activities, activity)
	}
	if err := domain.ValidateDashboardActivity(activities); err != nil {
		return nil, errors.New("dashboard activity projection is malformed")
	}
	return activities, nil
}

func (repository *DashboardActivityRepository) listReviewed(ctx context.Context) ([]domain.ReviewedCandidate, error) {
	value, found, err := repository.load(ctx, reviewedCandidatesStateKey)
	if err != nil || !found {
		return nil, err
	}
	items, err := stateItems(value)
	if err != nil {
		return nil, errors.New("reviewed candidate projection is malformed")
	}
	candidates := make([]domain.ReviewedCandidate, 0, len(items))
	for _, item := range items {
		candidate, err := reviewedFromState(item)
		if err != nil {
			return nil, errors.New("reviewed candidate projection is malformed")
		}
		candidates = append(candidates, candidate)
	}
	if err := domain.ValidateReviewedCandidates(candidates); err != nil {
		return nil, errors.New("reviewed candidate projection is malformed")
	}
	return candidates, nil
}

func (repository *DashboardActivityRepository) load(ctx context.Context, key string) (map[string]any, bool, error) {
	state, err := repository.client.BotState.Query().Where(botstate.StateKeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load dashboard projection: %w", err)
	}
	return state.Value, true, nil
}

func (repository *DashboardActivityRepository) save(ctx context.Context, key string, value map[string]any) error {
	if err := repository.client.BotState.Create().
		SetStateKey(key).
		SetValue(value).
		OnConflictColumns(botstate.FieldStateKey).
		UpdateNewValues().
		Exec(ctx); err != nil {
		return fmt.Errorf("save dashboard projection: %w", err)
	}
	return nil
}

func prependBounded[T any](items []T, item T) []T {
	items = append([]T{item}, items...)
	if len(items) > domain.MaxDashboardItems {
		return items[:domain.MaxDashboardItems]
	}
	return items
}

func activityState(activities []domain.AutomationActivity) map[string]any {
	items := make([]map[string]any, 0, len(activities))
	for _, activity := range activities {
		item := map[string]any{
			"occurred_at": activity.OccurredAt.UTC().Format(time.RFC3339Nano),
			"job":         string(activity.Job),
			"outcome":     string(activity.Outcome),
		}
		if activity.Category != "" {
			item["category"] = activity.Category
			item["stage"] = activity.Stage
		}
		items = append(items, item)
	}
	return map[string]any{"items": items}
}

func reviewedState(candidates []domain.ReviewedCandidate) map[string]any {
	items := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		items = append(items, map[string]any{
			"mint_address": candidate.MintAddress,
			"checked_at":   candidate.CheckedAt.UTC().Format(time.RFC3339Nano),
			"outcome":      string(candidate.Outcome),
			"reason":       string(candidate.Reason),
		})
	}
	return map[string]any{"items": items}
}

func stateItems(value map[string]any) ([]map[string]any, error) {
	raw, ok := value["items"].([]any)
	if !ok {
		return nil, errors.New("items are missing")
	}
	if len(raw) > domain.MaxDashboardItems {
		return nil, errors.New("items exceed limit")
	}
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		record, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("item is invalid")
		}
		items = append(items, record)
	}
	return items, nil
}

func activityFromState(value map[string]any) (domain.AutomationActivity, error) {
	occurredAt, err := stateTime(value, "occurred_at")
	if err != nil {
		return domain.AutomationActivity{}, err
	}
	job, ok := value["job"].(string)
	if !ok {
		return domain.AutomationActivity{}, errors.New("job is invalid")
	}
	outcome, ok := value["outcome"].(string)
	if !ok {
		return domain.AutomationActivity{}, errors.New("outcome is invalid")
	}
	activity := domain.AutomationActivity{OccurredAt: occurredAt, Job: domain.AutomationJob(job), Outcome: domain.AutomationOutcome(outcome)}
	if category, exists := value["category"]; exists {
		categoryText, ok := category.(string)
		stage, stageOK := value["stage"].(string)
		if !ok || !stageOK {
			return domain.AutomationActivity{}, errors.New("failure fields are invalid")
		}
		activity.Category = categoryText
		activity.Stage = stage
	}
	return activity, nil
}

func reviewedFromState(value map[string]any) (domain.ReviewedCandidate, error) {
	checkedAt, err := stateTime(value, "checked_at")
	if err != nil {
		return domain.ReviewedCandidate{}, err
	}
	mintAddress, mintOK := value["mint_address"].(string)
	outcome, outcomeOK := value["outcome"].(string)
	reason, hasReason := value["reason"]
	if !mintOK || !outcomeOK {
		return domain.ReviewedCandidate{}, errors.New("reviewed fields are invalid")
	}
	if !hasReason {
		return domain.ReviewedCandidate{MintAddress: mintAddress, CheckedAt: checkedAt, Outcome: domain.ReviewedCandidateOutcome(outcome), Reason: domain.ReviewReasonLegacyUnavailable}, nil
	}
	reasonText, reasonOK := reason.(string)
	if !reasonOK {
		return domain.ReviewedCandidate{}, errors.New("reviewed reason is invalid")
	}
	return domain.ReviewedCandidate{MintAddress: mintAddress, CheckedAt: checkedAt, Outcome: domain.ReviewedCandidateOutcome(outcome), Reason: domain.ReviewedCandidateReason(reasonText)}, nil
}

func stateTime(value map[string]any, key string) (time.Time, error) {
	raw, ok := value[key].(string)
	if !ok {
		return time.Time{}, errors.New("timestamp is invalid")
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || parsed.Location() != time.UTC {
		return time.Time{}, errors.New("timestamp is invalid")
	}
	return parsed.UTC(), nil
}

var _ application.AutomationActivityStore = (*DashboardActivityRepository)(nil)
var _ application.AutomationActivityReader = (*DashboardActivityRepository)(nil)
var _ application.ReviewedCandidateStore = (*DashboardActivityRepository)(nil)
var _ application.ReviewedCandidatesReader = (*DashboardActivityRepository)(nil)
