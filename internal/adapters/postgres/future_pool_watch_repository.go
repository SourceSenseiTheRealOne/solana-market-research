package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/botstate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	futurePoolWatchStatePrefix = "future-pool-watch:"
	futurePoolWatchCap         = 5
)

func (repository *CandidateRepository) Watch(ctx context.Context, pool domain.DiscoveredPool, expiresAt time.Time) error {
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("validate future pool watch: %w", err)
	}
	if expiresAt.IsZero() || !expiresAt.After(pool.CreatedAt) {
		return errors.New("future pool watch expiry must be after launch time")
	}
	key := futurePoolWatchKey(pool)
	states, err := repository.client.BotState.Query().Where(botstate.StateKeyHasPrefix(futurePoolWatchStatePrefix)).Limit(futurePoolWatchCap).All(ctx)
	if err != nil {
		return fmt.Errorf("load future pool watches: %w", err)
	}
	found := false
	for _, state := range states {
		if state.StateKey == key {
			found = true
			break
		}
	}
	if !found && len(states) == futurePoolWatchCap {
		oldest, err := oldestFuturePoolWatch(states)
		if err != nil {
			return err
		}
		if err := repository.client.BotState.DeleteOneID(oldest.ID).Exec(ctx); err != nil {
			return fmt.Errorf("evict oldest future pool watch: %w", err)
		}
	}
	value := futurePoolWatchValue(pool, expiresAt)
	if err := repository.client.BotState.Create().SetStateKey(key).SetValue(value).OnConflictColumns(botstate.FieldStateKey).UpdateNewValues().Exec(ctx); err != nil {
		return fmt.Errorf("save future pool watch: %w", err)
	}
	return nil
}

func (repository *CandidateRepository) TakeDue(ctx context.Context, now time.Time, limit int) ([]domain.DiscoveredPool, error) {
	if now.IsZero() || limit < 1 || limit > futurePoolWatchCap {
		return nil, errors.New("future pool watch retrieval is invalid")
	}
	states, err := repository.client.BotState.Query().Where(botstate.StateKeyHasPrefix(futurePoolWatchStatePrefix)).Limit(futurePoolWatchCap).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load future pool watches: %w", err)
	}
	due := make([]domain.DiscoveredPool, 0, limit)
	for _, state := range states {
		pool, expiresAt, err := futurePoolWatchFromState(state.Value)
		if err != nil {
			if deleteErr := repository.client.BotState.DeleteOneID(state.ID).Exec(ctx); deleteErr != nil {
				return nil, fmt.Errorf("delete invalid future pool watch: %w", deleteErr)
			}
			continue
		}
		if now.After(expiresAt) || !pool.CreatedAt.After(now) {
			if err := repository.client.BotState.DeleteOneID(state.ID).Exec(ctx); err != nil {
				return nil, fmt.Errorf("consume future pool watch: %w", err)
			}
			if !now.After(expiresAt) && len(due) < limit {
				due = append(due, pool)
			}
		}
	}
	return due, nil
}

func futurePoolWatchKey(pool domain.DiscoveredPool) string {
	digest := sha256.Sum256([]byte(pool.Identity()))
	return futurePoolWatchStatePrefix + hex.EncodeToString(digest[:])
}

func futurePoolWatchValue(pool domain.DiscoveredPool, expiresAt time.Time) map[string]any {
	return map[string]any{
		"source":       pool.Source,
		"network":      pool.Network,
		"mint_address": pool.MintAddress,
		"pool_address": pool.PoolAddress,
		"created_at":   pool.CreatedAt.UTC().Format(time.RFC3339Nano),
		"expires_at":   expiresAt.UTC().Format(time.RFC3339Nano),
	}
}

func futurePoolWatchFromState(value map[string]any) (domain.DiscoveredPool, time.Time, error) {
	read := func(name string) (string, error) {
		raw, ok := value[name].(string)
		if !ok || strings.TrimSpace(raw) == "" {
			return "", fmt.Errorf("future pool watch %s is missing", name)
		}
		return raw, nil
	}
	source, err := read("source")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, err
	}
	network, err := read("network")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, err
	}
	mint, err := read("mint_address")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, err
	}
	address, err := read("pool_address")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, err
	}
	createdAtRaw, err := read("created_at")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, err
	}
	expiresAtRaw, err := read("expires_at")
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return domain.DiscoveredPool{}, time.Time{}, fmt.Errorf("parse future pool watch created_at: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil || !expiresAt.After(createdAt) {
		return domain.DiscoveredPool{}, time.Time{}, errors.New("future pool watch expiry is invalid")
	}
	pool := domain.DiscoveredPool{Source: source, Network: network, MintAddress: mint, PoolAddress: address, CreatedAt: createdAt.UTC()}
	if err := pool.Validate(); err != nil {
		return domain.DiscoveredPool{}, time.Time{}, fmt.Errorf("validate future pool watch: %w", err)
	}
	return pool, expiresAt.UTC(), nil
}

func oldestFuturePoolWatch(states []*ent.BotState) (*ent.BotState, error) {
	if len(states) == 0 {
		return nil, errors.New("future pool watch eviction requires a stored watch")
	}
	oldest := states[0]
	oldestPool, _, err := futurePoolWatchFromState(oldest.Value)
	if err != nil {
		return oldest, nil
	}
	for _, state := range states[1:] {
		pool, _, err := futurePoolWatchFromState(state.Value)
		if err != nil || pool.CreatedAt.Before(oldestPool.CreatedAt) {
			oldest, oldestPool = state, pool
		}
	}
	return oldest, nil
}

var _ application.FuturePoolWatchStore = (*CandidateRepository)(nil)
