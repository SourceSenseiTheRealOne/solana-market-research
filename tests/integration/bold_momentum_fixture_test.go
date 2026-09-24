package integration_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestBoldMomentumPaperFlowFixtureMeetsExactBoundaries(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	fixture := newBoldMomentumPaperFlowFixture(now)

	evaluation := fixture.candidatePolicy.Evaluate(now, fixture.candidateEvidence())
	if !evaluation.Eligible || len(evaluation.Rules) != 12 {
		t.Fatalf("candidate evaluation = %#v, want 12 passing Bold-v2 rules", evaluation)
	}
	if !fixture.socialPolicy.Evaluate(fixture.socialMetrics, fixture.socialScore).Eligible {
		t.Fatal("social boundary aggregates were rejected")
	}
	if !fixture.verdictPolicy.Accepts(fixture.verdict) {
		t.Fatal("BUY/70/60/35 boundary verdict was rejected")
	}
	if fixture.strategyVersion != "bold-momentum-v2" || fixture.entryInputAmount != 100_000_000 || fixture.notionalMicros != 100_000_000 {
		t.Fatalf("strategy/sizing = %q/%d/%d, want Bold-v2 and $100 paper sizing", fixture.strategyVersion, fixture.entryInputAmount, fixture.notionalMicros)
	}
	if fixture.exitPolicy != (domain.ExitPolicy{TakeProfitBPS: 5_000, StopLossBPS: 2_000, MaxHoldDuration: 45 * time.Minute}) {
		t.Fatalf("exit policy = %#v, want +5000/-2000/45m", fixture.exitPolicy)
	}
}
