package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
)

func TestFreshScanJobBuildsANewScanForEachCycleUsingUTCNow(t *testing.T) {
	first := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.FixedZone("test", -5*60*60))
	second := first.Add(5 * time.Minute)
	times := []time.Time{first, second}
	var built []time.Time
	var ran int

	job := application.NewFreshScanJob(application.FreshScanJobOptions{
		Now: func() time.Time {
			now := times[0]
			times = times[1:]
			return now
		},
		Build: func(now time.Time) (application.ScheduledJob, error) {
			built = append(built, now)
			return scheduledJobFunc(func(context.Context) error {
				ran++
				return nil
			}), nil
		},
	})

	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	if err := job.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}

	if got, want := built, []time.Time{first.UTC(), second.UTC()}; len(got) != len(want) || !got[0].Equal(want[0]) || !got[1].Equal(want[1]) {
		t.Fatalf("build timestamps = %v, want fresh UTC timestamps %v", got, want)
	}
	if ran != 2 {
		t.Fatalf("built scan runs = %d, want 2", ran)
	}
}

type scheduledJobFunc func(context.Context) error

func (run scheduledJobFunc) RunOnce(ctx context.Context) error { return run(ctx) }
