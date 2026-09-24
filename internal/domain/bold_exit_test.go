package domain_test

import (
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestBoldMomentumExitPolicyExactBoundaries(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	policy := domain.ExitPolicy{TakeProfitBPS: 5_000, StopLossBPS: 2_000, MaxHoldDuration: 45 * time.Minute}
	tests := []struct {
		name      string
		returnBPS int64
		openedAt  time.Time
		want      domain.PositionCloseReason
	}{
		{name: "holds one basis point below take profit", returnBPS: 4_999, openedAt: now.Add(-10 * time.Minute), want: domain.PositionCloseNone},
		{name: "closes at take profit", returnBPS: 5_000, openedAt: now.Add(-10 * time.Minute), want: domain.PositionCloseTakeProfit},
		{name: "holds one basis point above stop loss", returnBPS: -1_999, openedAt: now.Add(-10 * time.Minute), want: domain.PositionCloseNone},
		{name: "closes at stop loss", returnBPS: -2_000, openedAt: now.Add(-10 * time.Minute), want: domain.PositionCloseStopLoss},
		{name: "holds one millisecond before timeout", openedAt: now.Add(-45*time.Minute + time.Millisecond), want: domain.PositionCloseNone},
		{name: "closes at timeout", openedAt: now.Add(-45 * time.Minute), want: domain.PositionCloseTimeout},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			position := domain.PaperPosition{OpenedAt: test.openedAt}
			decision, err := policy.Decide(now, position, domain.ExitMark{ReturnBPS: test.returnBPS})
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if decision.Reason != test.want {
				t.Fatalf("close reason = %q, want %q", decision.Reason, test.want)
			}
		})
	}
}
