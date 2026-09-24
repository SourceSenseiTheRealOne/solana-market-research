package domain_test

import (
	"testing"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestRealizedPNLMicrosUsesIntegerTerminalReturn(t *testing.T) {
	tests := []struct {
		name      string
		returnBPS int64
		want      int64
	}{
		{name: "take profit", returnBPS: 3_000, want: 3_000_000},
		{name: "stop loss", returnBPS: -1_500, want: -1_500_000},
		{name: "unsellable", returnBPS: domain.UnsellableReturnBPS, want: -10_000_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := domain.RealizedPNLMicros(10_000_000, test.returnBPS)
			if err != nil {
				t.Fatalf("RealizedPNLMicros() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("RealizedPNLMicros() = %d, want %d", got, test.want)
			}
		})
	}
}
