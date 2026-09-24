package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/postgres"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/config"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/transport/httpapi"
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	exitCode := runMain(ctx, run)
	stop()
	if exitCode != 0 {
		slog.Error("paper bot stopped")
	}
	os.Exit(exitCode)
}

func runMain(ctx context.Context, runFn func(context.Context) error) int {
	if err := runFn(ctx); err != nil {
		return 1
	}
	return 0
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := postgres.OpenEnt(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	now := time.Now
	positions, err := postgres.NewPositionRepository(client, now)
	if err != nil {
		return err
	}
	dailyResults, err := postgres.NewDailyResultRepository(client, now)
	if err != nil {
		return err
	}
	twitterAnalytics, err := postgres.NewTwitterAnalyticsRepository(client)
	if err != nil {
		return err
	}
	dashboardActivity, err := postgres.NewDashboardActivityRepository(client)
	if err != nil {
		return err
	}
	reader := application.NewDashboardReadService(application.DashboardReadServiceOptions{
		StrategyVersion:    cfg.StrategyVersion,
		Positions:          positions,
		DailyResults:       dailyResults,
		TwitterAnalytics:   twitterAnalytics,
		AutomationActivity: dashboardActivity,
		ReviewedCandidates: dashboardActivity,
	})
	server := newHTTPServer(cfg.HTTPAddr, reader, now)
	stopAutomation, err := startAutomation(ctx, cfg, client, now)
	if err != nil {
		return err
	}
	defer stopAutomation()

	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}

func newHTTPServer(address string, reader application.DashboardReader, now func() time.Time) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           httpapi.NewHandler(httpapi.Options{Reader: reader, Now: now}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}
