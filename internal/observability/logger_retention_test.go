package observability_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/observability"
)

func TestDailyJSONLoggerRetainsOnlyThirtyUTCFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2026-08-01.jsonl", "2026-08-02.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	logger, err := observability.NewDailyJSONLogger(observability.LoggerOptions{
		Dir:           dir,
		Now:           func() time.Time { return time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC) },
		RetentionDays: 30,
	})
	if err != nil {
		t.Fatalf("NewDailyJSONLogger() error = %v", err)
	}
	defer logger.Close()

	if _, err := os.Stat(filepath.Join(dir, "2026-08-01.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("31st UTC log file still exists, stat error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-08-02.jsonl")); err != nil {
		t.Fatalf("30th UTC log file was removed: %v", err)
	}
}
