package application

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestProductionAdmissionKeyIsStrategyScopedAndDeterministic(t *testing.T) {
	pool := domain.DiscoveredPool{
		Source: domain.SourceGeckoTerminal, Network: domain.NetworkSolana,
		MintAddress: "mint", PoolAddress: "pool",
		CreatedAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
	}
	keyV1 := productionAdmissionKey("production-v1", pool)
	keyV2 := productionAdmissionKey("bold-momentum-v2", pool)
	if keyV1 == keyV2 {
		t.Fatal("strategy versions shared one admission key")
	}
	if got := productionAdmissionKey("bold-momentum-v2", pool); got != keyV2 {
		t.Fatalf("productionAdmissionKey() = %q, want deterministic %q", got, keyV2)
	}
}
