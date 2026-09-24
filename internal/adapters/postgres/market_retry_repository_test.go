package postgres_test

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestCandidateRepositorySchedulesAndReservesOneDueMarketRetry(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()
	repository, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	pool := marketRetryPool(fmt.Sprintf("reserve-%d", time.Now().UnixNano()), now.Add(-2*time.Minute))
	defer func() { _ = repository.Complete(context.Background(), pool) }()

	if err := repository.Schedule(context.Background(), pool, now.Add(30*time.Second), now.Add(10*time.Minute)); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	beforeDue, err := repository.ReserveDue(context.Background(), now, now.Add(30*time.Second), 1)
	if err != nil {
		t.Fatalf("ReserveDue() before due error = %v", err)
	}
	if len(beforeDue) != 0 {
		t.Fatalf("before-due reservations = %#v, want none", beforeDue)
	}
	dueAt := now.Add(30 * time.Second)
	reserved, err := repository.ReserveDue(context.Background(), dueAt, dueAt.Add(30*time.Second), 1)
	if err != nil {
		t.Fatalf("ReserveDue() error = %v", err)
	}
	if len(reserved) != 1 || reserved[0].Identity() != pool.Identity() {
		t.Fatalf("reserved retries = %#v, want %s", reserved, pool.Identity())
	}
	leased, err := repository.ReserveDue(context.Background(), dueAt, dueAt.Add(30*time.Second), 1)
	if err != nil {
		t.Fatalf("ReserveDue() during lease error = %v", err)
	}
	if len(leased) != 0 {
		t.Fatalf("leased retry was reserved twice: %#v", leased)
	}
}

func TestCandidateRepositoryPreservesFirstExpiryOnRefresh(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()
	repository, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	pool := marketRetryPool(fmt.Sprintf("expiry-%d", time.Now().UnixNano()), now.Add(-time.Minute))
	defer func() { _ = repository.Complete(context.Background(), pool) }()
	firstExpiry := now.Add(2 * time.Minute)

	if err := repository.Schedule(context.Background(), pool, now.Add(30*time.Second), firstExpiry); err != nil {
		t.Fatalf("first Schedule() error = %v", err)
	}
	if err := repository.Schedule(context.Background(), pool, now.Add(time.Minute), now.Add(20*time.Minute)); err != nil {
		t.Fatalf("refreshed Schedule() error = %v", err)
	}
	reserved, err := repository.ReserveDue(context.Background(), firstExpiry.Add(time.Nanosecond), firstExpiry.Add(time.Minute), 1)
	if err != nil {
		t.Fatalf("ReserveDue() after first expiry error = %v", err)
	}
	if len(reserved) != 0 {
		t.Fatalf("refresh extended the first expiry: %#v", reserved)
	}
}

func TestCandidateRepositoryCapsRetriesAtFiveAndEvictsOldest(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()
	repository, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	unique := fmt.Sprintf("cap-%d", time.Now().UnixNano())
	pools := make([]domain.DiscoveredPool, 0, 6)
	for index := 0; index < 6; index++ {
		pool := marketRetryPool(fmt.Sprintf("%s-%d", unique, index), now.Add(time.Duration(index-6)*time.Minute))
		pools = append(pools, pool)
		defer func(candidate domain.DiscoveredPool) { _ = repository.Complete(context.Background(), candidate) }(pool)
		if err := repository.Schedule(context.Background(), pool, now.Add(30*time.Second), now.Add(10*time.Minute)); err != nil {
			t.Fatalf("Schedule(%d) error = %v", index, err)
		}
	}

	dueAt := now.Add(30 * time.Second)
	reserved, err := repository.ReserveDue(context.Background(), dueAt, dueAt.Add(30*time.Second), 5)
	if err != nil {
		t.Fatalf("ReserveDue() error = %v", err)
	}
	if len(reserved) != 5 {
		t.Fatalf("reserved retry count = %d, want five", len(reserved))
	}
	for _, pool := range reserved {
		if pool.Identity() == pools[0].Identity() {
			t.Fatalf("oldest retry was not evicted: %s", pool.Identity())
		}
	}
}

func TestCandidateRepositoryDeletesExpiredRetryAndCompletesIdempotently(t *testing.T) {
	client := openTestEntClient(t)
	defer func() { _ = client.Close() }()
	repository, err := postgres.NewCandidateRepository(client)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	now := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)
	pool := marketRetryPool(fmt.Sprintf("complete-%d", time.Now().UnixNano()), now.Add(-time.Minute))

	if err := repository.Schedule(context.Background(), pool, now.Add(30*time.Second), now.Add(time.Minute)); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	reserved, err := repository.ReserveDue(context.Background(), now.Add(time.Minute+time.Nanosecond), now.Add(2*time.Minute), 1)
	if err != nil {
		t.Fatalf("ReserveDue() expired error = %v", err)
	}
	if len(reserved) != 0 {
		t.Fatalf("expired retry was reserved: %#v", reserved)
	}
	if err := repository.Complete(context.Background(), pool); err != nil {
		t.Fatalf("first Complete() error = %v", err)
	}
	if err := repository.Complete(context.Background(), pool); err != nil {
		t.Fatalf("idempotent Complete() error = %v", err)
	}
}

func TestCandidateRepositoryCompletesRetryWithoutDeletePrivilege(t *testing.T) {
	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; run this contract against a reset local Supabase database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin test database: %v", err)
	}
	defer func() { _ = admin.Close(context.Background()) }()
	role := fmt.Sprintf("paper_retry_no_delete_%d", time.Now().UTC().UnixNano())
	password := "synthetic-retry-role-password"
	identifier := pgx.Identifier{role}.Sanitize()
	config, err := pgx.ParseConfig(adminURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE ROLE "+identifier+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatalf("create restricted retry role: %v", err)
	}
	pool := marketRetryPool(fmt.Sprintf("no-delete-%d", time.Now().UnixNano()), time.Now().UTC().Add(-time.Minute))
	defer func() {
		_, _ = admin.Exec(context.Background(), "DELETE FROM bot_states WHERE value->>'mint_address' = $1", pool.MintAddress)
		_, _ = admin.Exec(context.Background(), "DROP ROLE IF EXISTS "+identifier)
	}()
	grants := []string{
		"REVOKE DELETE ON TABLE bot_states FROM PUBLIC",
		"GRANT CONNECT ON DATABASE " + pgx.Identifier{config.Database}.Sanitize() + " TO " + identifier,
		"GRANT USAGE ON SCHEMA public TO " + identifier,
		"GRANT SELECT, INSERT, UPDATE ON TABLE bot_states TO " + identifier,
		"GRANT USAGE, SELECT ON SEQUENCE bot_states_id_seq TO " + identifier,
		"REVOKE DELETE ON TABLE bot_states FROM " + identifier,
	}
	for _, statement := range grants {
		if _, err := admin.Exec(ctx, statement); err != nil {
			t.Fatalf("configure restricted retry role: %v", err)
		}
	}
	config.User, config.Password = role, password
	probe, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect restricted-role probe: %v", err)
	}
	var currentUser string
	var canDelete bool
	if err := probe.QueryRow(ctx, "SELECT current_user, has_table_privilege(current_user, 'bot_states', 'DELETE')").Scan(&currentUser, &canDelete); err != nil {
		_ = probe.Close(context.Background())
		t.Fatalf("inspect restricted retry role: %v", err)
	}
	_ = probe.Close(context.Background())
	if currentUser != role || canDelete {
		t.Fatalf("restricted-role fixture is invalid: current_user_match=%t can_delete=%t", currentUser == role, canDelete)
	}
	restrictedURL := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(role, password),
		Host:     net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))),
		Path:     config.Database,
		RawQuery: "sslmode=disable",
	}).String()
	restricted, err := postgres.OpenEnt(ctx, restrictedURL)
	if err != nil {
		t.Fatalf("open restricted retry repository: %v", err)
	}
	defer func() { _ = restricted.Close() }()
	repository, err := postgres.NewCandidateRepository(restricted)
	if err != nil {
		t.Fatalf("NewCandidateRepository() error = %v", err)
	}
	now := time.Now().UTC()
	if err := repository.Schedule(ctx, pool, now.Add(30*time.Second), now.Add(10*time.Minute)); err != nil {
		t.Fatalf("Schedule() with restricted role error = %v", err)
	}
	if err := repository.Complete(ctx, pool); err != nil {
		t.Fatalf("Complete() required DELETE privilege: %v", err)
	}
}

func marketRetryPool(identity string, createdAt time.Time) domain.DiscoveredPool {
	return domain.DiscoveredPool{
		Source:      domain.SourceDexScreener,
		Network:     domain.NetworkSolana,
		MintAddress: identity + "-mint",
		PoolAddress: identity + "-pool",
		CreatedAt:   createdAt.UTC(),
	}
}
