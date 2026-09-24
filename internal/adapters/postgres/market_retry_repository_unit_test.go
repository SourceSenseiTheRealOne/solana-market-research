package postgres

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestMarketRetryValueRoundTripsValidatedPoolAndUTCMetadata(t *testing.T) {
	zone := time.FixedZone("west", -7*60*60)
	createdAt := time.Date(2026, time.August, 31, 3, 1, 54, 0, zone)
	pool := domain.DiscoveredPool{
		Source:      domain.SourceDexScreener,
		Network:     domain.NetworkSolana,
		MintAddress: "retry-mint",
		PoolAddress: "retry-pool",
		CreatedAt:   createdAt,
	}
	nextAttemptAt := createdAt.Add(3 * time.Minute)
	expiresAt := nextAttemptAt.Add(10 * time.Minute)

	gotPool, gotNext, gotExpiry, err := marketRetryFromState(marketRetryValue(pool, nextAttemptAt, expiresAt))
	if err != nil {
		t.Fatalf("marketRetryFromState() error = %v", err)
	}
	if gotPool.Identity() != pool.Identity() || !gotPool.CreatedAt.Equal(createdAt.UTC()) {
		t.Fatalf("round-tripped pool = %#v", gotPool)
	}
	if !gotNext.Equal(nextAttemptAt.UTC()) || !gotExpiry.Equal(expiresAt.UTC()) {
		t.Fatalf("round-tripped retry bounds = %s/%s", gotNext, gotExpiry)
	}
}

func TestMarketRetryFromStateRejectsMissingMalformedAndInvertedTimes(t *testing.T) {
	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	pool := domain.DiscoveredPool{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: "mint", PoolAddress: "pool", CreatedAt: now}
	valid := marketRetryValue(pool, now.Add(30*time.Second), now.Add(10*time.Minute))

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "missing source", mutate: func(value map[string]any) { delete(value, "source") }},
		{name: "malformed created at", mutate: func(value map[string]any) { value["created_at"] = "not-a-time" }},
		{name: "malformed next attempt", mutate: func(value map[string]any) { value["next_attempt_at"] = "not-a-time" }},
		{name: "malformed expiry", mutate: func(value map[string]any) { value["expires_at"] = "not-a-time" }},
		{name: "next before creation", mutate: func(value map[string]any) { value["next_attempt_at"] = now.Add(-time.Second).Format(time.RFC3339Nano) }},
		{name: "expiry not after next", mutate: func(value map[string]any) { value["expires_at"] = now.Add(30 * time.Second).Format(time.RFC3339Nano) }},
		{name: "invalid pool", mutate: func(value map[string]any) { value["pool_address"] = "" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := make(map[string]any, len(valid))
			for key, item := range valid {
				value[key] = item
			}
			test.mutate(value)
			if _, _, _, err := marketRetryFromState(value); err == nil {
				t.Fatal("marketRetryFromState() accepted malformed state")
			}
		})
	}
}

func TestOldestMarketRetryUsesPoolCreationThenIdentityTieBreak(t *testing.T) {
	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	makeState := func(id int, mint string, createdAt time.Time) *ent.BotState {
		pool := domain.DiscoveredPool{Source: domain.SourceDexScreener, Network: domain.NetworkSolana, MintAddress: mint, PoolAddress: "pool-" + mint, CreatedAt: createdAt}
		return &ent.BotState{ID: id, StateKey: marketRetryKey(pool), Value: marketRetryValue(pool, now.Add(30*time.Second), now.Add(10*time.Minute))}
	}
	newer := makeState(1, "mint-c", now.Add(-time.Minute))
	tieLaterIdentity := makeState(2, "mint-b", now.Add(-2*time.Minute))
	tieEarlierIdentity := makeState(3, "mint-a", now.Add(-2*time.Minute))

	oldest, err := oldestMarketRetry([]*ent.BotState{newer, tieLaterIdentity, tieEarlierIdentity})
	if err != nil {
		t.Fatalf("oldestMarketRetry() error = %v", err)
	}
	if oldest.ID != tieEarlierIdentity.ID {
		t.Fatalf("oldest retry ID = %d, want %d", oldest.ID, tieEarlierIdentity.ID)
	}
}
