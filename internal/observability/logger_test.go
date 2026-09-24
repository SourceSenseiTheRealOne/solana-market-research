package observability_test

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/observability"
)

func TestDailyJSONLoggerRedactsSecretsAndWritesUTCNamedFile(t *testing.T) {
	dir := t.TempDir()
	logger, err := observability.NewDailyJSONLogger(observability.LoggerOptions{
		Dir:           dir,
		Now:           func() time.Time { return time.Date(2026, 8, 18, 23, 0, 0, 0, time.FixedZone("local", -7*60*60)) },
		RetentionDays: 30,
	})
	if err != nil {
		t.Fatalf("NewDailyJSONLogger() error = %v", err)
	}
	defer func() {
		if err := logger.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	logger.Info("provider call", slog.String("api_key", "synthetic-secret-value"), slog.String("provider", "dexscreener"))

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "2026-08-19.jsonl" {
		t.Fatalf("UTC log file = %#v, want only 2026-08-19.jsonl", entries)
	}
	payload, err := os.ReadFile(dir + "/" + entries[0].Name())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(payload), "synthetic-secret-value") {
		t.Fatal("logger leaked a secret-like value")
	}
	if !strings.Contains(string(payload), "dexscreener") {
		t.Fatal("logger omitted allowlisted provider field")
	}
}
