package observability_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/observability"
)

func TestDailyJSONLoggerDropsUnknownFieldsByDefault(t *testing.T) {
	dir := t.TempDir()
	logger, err := observability.NewDailyJSONLogger(observability.LoggerOptions{
		Dir: dir,
		Now: func() time.Time {
			return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewDailyJSONLogger() error = %v", err)
	}
	defer logger.Close()

	logger.Info("provider call", slog.String("provider", "dexscreener"), slog.String("raw_payload", "must-not-be-written"))

	payload, err := os.ReadFile(filepath.Join(dir, "2026-08-19.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(payload), "must-not-be-written") || strings.Contains(string(payload), "raw_payload") {
		t.Fatal("logger wrote an unknown field")
	}
	if !strings.Contains(string(payload), "dexscreener") {
		t.Fatal("logger omitted an allowlisted field")
	}
}
