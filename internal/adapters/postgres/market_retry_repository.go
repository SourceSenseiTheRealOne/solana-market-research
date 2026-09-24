package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/botstate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	marketRetryStatePrefix = "market-retry:"
	marketRetryCap         = application.MarketRetryCap
)

func (repository *CandidateRepository) Schedule(ctx context.Context, pool domain.DiscoveredPool, nextAttemptAt, expiresAt time.Time) error {
	if repository == nil || repository.client == nil {
		return errors.New("market retry repository is not configured")
	}
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("validate market retry pool: %w", err)
	}
	nextAttemptAt = nextAttemptAt.UTC()
	expiresAt = expiresAt.UTC()
	if nextAttemptAt.IsZero() || !nextAttemptAt.After(pool.CreatedAt.UTC()) || expiresAt.IsZero() || !expiresAt.After(nextAttemptAt) {
		return errors.New("market retry schedule is invalid")
	}

	tx, err := repository.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin market retry transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	key := marketRetryKey(pool)
	states, err := tx.BotState.Query().Where(botstate.StateKeyHasPrefix(marketRetryStatePrefix)).Limit(marketRetryCap).All(ctx)
	if err != nil {
		return fmt.Errorf("load market retries: %w", err)
	}
	var existing *ent.BotState
	var reusable *ent.BotState
	for _, state := range states {
		if state.StateKey == key {
			existing = state
			continue
		}
		if reusable != nil {
			continue
		}
		if isMarketRetryTombstone(state.Value) {
			reusable = state
			continue
		}
		_, _, stateExpiry, decodeErr := marketRetryFromState(state.Value)
		if decodeErr != nil || !stateExpiry.After(nextAttemptAt) {
			reusable = state
		}
	}
	if existing != nil {
		_, _, firstExpiry, decodeErr := marketRetryFromState(existing.Value)
		if decodeErr == nil && firstExpiry.Before(expiresAt) {
			expiresAt = firstExpiry
		}
	}
	if existing != nil && !expiresAt.After(nextAttemptAt) {
		if err := tx.BotState.UpdateOneID(existing.ID).SetValue(marketRetryTombstoneValue()).Exec(ctx); err != nil {
			return fmt.Errorf("clear exhausted market retry: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit exhausted market retry: %w", err)
		}
		return nil
	}
	value := marketRetryValue(pool, nextAttemptAt, expiresAt)
	switch {
	case existing != nil:
		if err := tx.BotState.UpdateOneID(existing.ID).SetValue(value).Exec(ctx); err != nil {
			return fmt.Errorf("refresh market retry: %w", err)
		}
	case reusable != nil:
		if err := tx.BotState.UpdateOneID(reusable.ID).SetStateKey(key).SetValue(value).Exec(ctx); err != nil {
			return fmt.Errorf("reuse cleared market retry: %w", err)
		}
	case len(states) == marketRetryCap:
		oldest, err := oldestMarketRetry(states)
		if err != nil {
			return err
		}
		if err := tx.BotState.UpdateOneID(oldest.ID).SetStateKey(key).SetValue(value).Exec(ctx); err != nil {
			return fmt.Errorf("replace oldest market retry: %w", err)
		}
	default:
		if err := tx.BotState.Create().SetStateKey(key).SetValue(value).Exec(ctx); err != nil {
			return fmt.Errorf("save market retry: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit market retry: %w", err)
	}
	return nil
}

func (repository *CandidateRepository) ReserveDue(ctx context.Context, now, leaseUntil time.Time, limit int) ([]domain.DiscoveredPool, error) {
	if repository == nil || repository.client == nil {
		return nil, errors.New("market retry repository is not configured")
	}
	now = now.UTC()
	leaseUntil = leaseUntil.UTC()
	if now.IsZero() || leaseUntil.IsZero() || !leaseUntil.After(now) || limit < 1 || limit > marketRetryCap {
		return nil, errors.New("market retry reservation is invalid")
	}
	tx, err := repository.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin market retry reservation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	states, err := tx.BotState.Query().Where(botstate.StateKeyHasPrefix(marketRetryStatePrefix)).Limit(marketRetryCap).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load market retries: %w", err)
	}
	type dueRetry struct {
		state         *ent.BotState
		pool          domain.DiscoveredPool
		nextAttemptAt time.Time
		expiresAt     time.Time
	}
	due := make([]dueRetry, 0, len(states))
	for _, state := range states {
		if isMarketRetryTombstone(state.Value) {
			continue
		}
		pool, nextAttemptAt, expiresAt, decodeErr := marketRetryFromState(state.Value)
		if decodeErr != nil || !now.Before(expiresAt) {
			if err := tx.BotState.UpdateOneID(state.ID).SetValue(marketRetryTombstoneValue()).Exec(ctx); err != nil {
				return nil, fmt.Errorf("clear invalid or expired market retry: %w", err)
			}
			continue
		}
		if !nextAttemptAt.After(now) {
			due = append(due, dueRetry{state: state, pool: pool, nextAttemptAt: nextAttemptAt, expiresAt: expiresAt})
		}
	}
	sort.Slice(due, func(left, right int) bool {
		if !due[left].nextAttemptAt.Equal(due[right].nextAttemptAt) {
			return due[left].nextAttemptAt.Before(due[right].nextAttemptAt)
		}
		if !due[left].pool.CreatedAt.Equal(due[right].pool.CreatedAt) {
			return due[left].pool.CreatedAt.Before(due[right].pool.CreatedAt)
		}
		return due[left].pool.Identity() < due[right].pool.Identity()
	})
	reserved := make([]domain.DiscoveredPool, 0, limit)
	for _, retry := range due {
		if len(reserved) == limit {
			break
		}
		if !retry.expiresAt.After(leaseUntil) {
			if err := tx.BotState.UpdateOneID(retry.state.ID).SetValue(marketRetryTombstoneValue()).Exec(ctx); err != nil {
				return nil, fmt.Errorf("clear exhausted market retry: %w", err)
			}
			continue
		}
		value := marketRetryValue(retry.pool, leaseUntil, retry.expiresAt)
		if err := tx.BotState.UpdateOneID(retry.state.ID).SetValue(value).Exec(ctx); err != nil {
			return nil, fmt.Errorf("lease market retry: %w", err)
		}
		reserved = append(reserved, retry.pool)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit market retry reservation: %w", err)
	}
	return reserved, nil
}

func (repository *CandidateRepository) Complete(ctx context.Context, pool domain.DiscoveredPool) error {
	if repository == nil || repository.client == nil {
		return errors.New("market retry repository is not configured")
	}
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("validate completed market retry: %w", err)
	}
	state, err := repository.client.BotState.Query().Where(botstate.StateKeyEQ(marketRetryKey(pool))).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load completed market retry: %w", err)
	}
	if err := repository.client.BotState.UpdateOneID(state.ID).SetValue(marketRetryTombstoneValue()).Exec(ctx); err != nil {
		return fmt.Errorf("clear completed market retry: %w", err)
	}
	return nil
}

func marketRetryKey(pool domain.DiscoveredPool) string {
	digest := sha256.Sum256([]byte(pool.Identity()))
	return marketRetryStatePrefix + hex.EncodeToString(digest[:])
}

func marketRetryValue(pool domain.DiscoveredPool, nextAttemptAt, expiresAt time.Time) map[string]any {
	return map[string]any{
		"state":           "active",
		"source":          pool.Source,
		"network":         pool.Network,
		"mint_address":    pool.MintAddress,
		"pool_address":    pool.PoolAddress,
		"created_at":      pool.CreatedAt.UTC().Format(time.RFC3339Nano),
		"next_attempt_at": nextAttemptAt.UTC().Format(time.RFC3339Nano),
		"expires_at":      expiresAt.UTC().Format(time.RFC3339Nano),
	}
}

func marketRetryTombstoneValue() map[string]any {
	return map[string]any{"state": "empty"}
}

func isMarketRetryTombstone(value map[string]any) bool {
	state, _ := value["state"].(string)
	return state == "empty"
}

func marketRetryFromState(value map[string]any) (domain.DiscoveredPool, time.Time, time.Time, error) {
	if isMarketRetryTombstone(value) {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, errors.New("market retry state is empty")
	}
	read := func(name string) (string, error) {
		raw, ok := value[name].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("market retry %s is missing", name)
		}
		return raw, nil
	}
	source, err := read("source")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, err
	}
	network, err := read("network")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, err
	}
	mint, err := read("mint_address")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, err
	}
	address, err := read("pool_address")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, err
	}
	createdAtRaw, err := read("created_at")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, err
	}
	nextAttemptAtRaw, err := read("next_attempt_at")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, err
	}
	expiresAtRaw, err := read("expires_at")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, errors.New("market retry created_at is invalid")
	}
	nextAttemptAt, err := time.Parse(time.RFC3339Nano, nextAttemptAtRaw)
	if err != nil || !nextAttemptAt.After(createdAt) {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, errors.New("market retry next_attempt_at is invalid")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil || !expiresAt.After(nextAttemptAt) {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, errors.New("market retry expires_at is invalid")
	}
	pool := domain.DiscoveredPool{Source: source, Network: network, MintAddress: mint, PoolAddress: address, CreatedAt: createdAt.UTC()}
	if err := pool.Validate(); err != nil {
		return domain.DiscoveredPool{}, time.Time{}, time.Time{}, fmt.Errorf("validate market retry pool: %w", err)
	}
	return pool, nextAttemptAt.UTC(), expiresAt.UTC(), nil
}

func oldestMarketRetry(states []*ent.BotState) (*ent.BotState, error) {
	if len(states) == 0 {
		return nil, errors.New("market retry eviction requires stored state")
	}
	oldest := states[0]
	oldestPool, _, _, err := marketRetryFromState(oldest.Value)
	if err != nil {
		return oldest, nil
	}
	for _, state := range states[1:] {
		pool, _, _, err := marketRetryFromState(state.Value)
		if err != nil {
			return state, nil
		}
		if pool.CreatedAt.Before(oldestPool.CreatedAt) || (pool.CreatedAt.Equal(oldestPool.CreatedAt) && pool.Identity() < oldestPool.Identity()) {
			oldest = state
			oldestPool = pool
		}
	}
	return oldest, nil
}

var _ application.MarketRetryStore = (*CandidateRepository)(nil)
