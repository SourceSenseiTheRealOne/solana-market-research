package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/application"
)

func TestPaperBrokerAcceptsStructurallyValidSecondQuoteWithoutAgeLimit(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	positions := &brokerPositions{result: application.OpenPositionResult{Opened: true}}
	broker := application.NewPaperBroker(application.PaperBrokerOptions{
		Now:                    (&brokerClock{now: now}).Now,
		Sleep:                  (&brokerClock{now: now}).Sleep,
		Quotes:                 &brokerQuotes{quote: validBrokerQuote(now.Add(-24 * time.Hour))},
		Positions:              positions,
		SimulatedLatency:       time.Second,
		MaxEntryPriceImpactBPS: 1_000,
	})

	result, err := broker.Open(context.Background(), validOpenPositionRequest())
	if err != nil || !result.Opened || len(positions.inputs) != 1 {
		t.Fatalf("Open() result/error/writes = %#v/%v/%d, want accepted structurally valid quote", result, err, len(positions.inputs))
	}
}
