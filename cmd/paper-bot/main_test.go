package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
)

func TestNewHTTPServerServesDashboardWithBoundedTimeouts(t *testing.T) {
	date := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)
	reader := &dashboardReaderFake{snapshot: application.DashboardSnapshot{UTCDate: date}}
	server := newHTTPServer("127.0.0.1:8080", reader, func() time.Time { return date })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard?date=2026-08-20", nil)
	response := httptest.NewRecorder()

	server.Handler.ServeHTTP(response, request)

	if server.Addr != "127.0.0.1:8080" || server.ReadHeaderTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 || response.Code != http.StatusOK || reader.date != date {
		t.Fatalf("server addr=%q timeouts=%s/%s/%s status=%d date=%s, want bounded dashboard server", server.Addr, server.ReadHeaderTimeout, server.WriteTimeout, server.IdleTimeout, response.Code, reader.date)
	}
}

func TestRunMainReturnsFailureForStartupError(t *testing.T) {
	if exitCode := runMain(context.Background(), func(context.Context) error { return errors.New("startup failed") }); exitCode != 1 {
		t.Fatalf("runMain() exit code = %d, want 1", exitCode)
	}
}

func TestRunMainReturnsSuccessWhenRuntimeStopsCleanly(t *testing.T) {
	if exitCode := runMain(context.Background(), func(context.Context) error { return nil }); exitCode != 0 {
		t.Fatalf("runMain() exit code = %d, want 0", exitCode)
	}
}

func TestLaunchAutomationIsOptInAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &automationRunnerFake{}

	if errors := launchAutomation(ctx, false, runner); errors != nil {
		t.Fatal("disabled automation returned an error channel")
	}
	if runner.calls != 0 {
		t.Fatalf("disabled automation calls = %d, want 0", runner.calls)
	}

	errors := launchAutomation(ctx, true, runner)
	if errors == nil {
		t.Fatal("enabled automation returned no error channel")
	}
	cancel()
	if err := <-errors; err != nil {
		t.Fatalf("enabled automation shutdown error = %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("enabled automation calls = %d, want 1", runner.calls)
	}
}

type dashboardReaderFake struct {
	snapshot application.DashboardSnapshot
	date     time.Time
}

func (fake *dashboardReaderFake) Read(_ context.Context, date time.Time) (application.DashboardSnapshot, error) {
	fake.date = date
	return fake.snapshot, nil
}

type automationRunnerFake struct{ calls int }

func (fake *automationRunnerFake) Run(ctx context.Context) error {
	fake.calls++
	<-ctx.Done()
	return nil
}
