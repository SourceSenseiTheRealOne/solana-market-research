package application_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
)

func TestSplitCadenceRunsInitialScanMonitorAndRetrySerially(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make([]string, 0, 3)
	scan := scheduledJobFake{run: func(context.Context) error {
		calls = append(calls, "scan")
		return nil
	}}
	monitor := scheduledJobFake{run: func(context.Context) error {
		calls = append(calls, "monitor")
		return nil
	}}
	retry := scheduledJobFake{run: func(context.Context) error {
		calls = append(calls, "retry")
		cancel()
		return nil
	}}

	err := application.NewSplitCadence(application.SplitCadenceOptions{
		Scan:            scan,
		Monitor:         monitor,
		Retry:           retry,
		ScanInterval:    5 * time.Minute,
		MonitorInterval: 30 * time.Second,
		ScanTimeout:     20 * time.Second,
		MonitorTimeout:  10 * time.Second,
		RetryTimeout:    10 * time.Second,
	}).Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := calls, []string{"scan", "monitor", "retry"}; !sameStrings(got, want) {
		t.Fatalf("scheduled calls = %v, want serial initial %v", got, want)
	}
}

func TestSplitCadenceRunsMonitorBeforeRetryOnFastTickWithoutOverlap(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make([]string, 0, 5)
	var inFlight int32
	var maximumInFlight int32
	record := func(name string) {
		current := atomic.AddInt32(&inFlight, 1)
		for current > atomic.LoadInt32(&maximumInFlight) {
			if atomic.CompareAndSwapInt32(&maximumInFlight, atomic.LoadInt32(&maximumInFlight), current) {
				break
			}
		}
		calls = append(calls, name)
		atomic.AddInt32(&inFlight, -1)
	}
	retryCalls := 0
	scan := scheduledJobFake{run: func(context.Context) error { record("scan"); return nil }}
	monitor := scheduledJobFake{run: func(context.Context) error { record("monitor"); return nil }}
	retry := scheduledJobFake{run: func(context.Context) error {
		record("retry")
		retryCalls++
		if retryCalls == 2 {
			cancel()
		}
		return nil
	}}

	err := application.NewSplitCadence(application.SplitCadenceOptions{
		Scan: scan, Monitor: monitor, Retry: retry,
		ScanInterval: time.Hour, MonitorInterval: 5 * time.Millisecond,
		ScanTimeout: time.Second, MonitorTimeout: time.Second, RetryTimeout: time.Second,
	}).Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := calls, []string{"scan", "monitor", "retry", "monitor", "retry"}; !sameStrings(got, want) {
		t.Fatalf("scheduled calls = %v, want %v", got, want)
	}
	if got := atomic.LoadInt32(&maximumInFlight); got != 1 {
		t.Fatalf("maximum in-flight jobs = %d, want exactly one", got)
	}
}

type scheduledJobFake struct{ run func(context.Context) error }

func (fake scheduledJobFake) RunOnce(ctx context.Context) error { return fake.run(ctx) }
