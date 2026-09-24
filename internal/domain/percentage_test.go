package domain_test

import (
	"testing"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestParsePercentageBPSRoundsHalfAwayFromZero(t *testing.T) {
	tests := []struct {
		raw  string
		want int64
	}{
		{raw: "2", want: 200},
		{raw: "2.345", want: 235},
		{raw: "-18.046", want: -1805},
		{raw: "60", want: 6_000},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			got, err := domain.ParsePercentageBPS(test.raw)
			if err != nil {
				t.Fatalf("ParsePercentageBPS(%q) error = %v", test.raw, err)
			}
			if got != test.want {
				t.Fatalf("ParsePercentageBPS(%q) = %d, want %d", test.raw, got, test.want)
			}
		})
	}
}

func TestParsePercentageBPSRejectsInvalidOrOutOfRangeValues(t *testing.T) {
	for _, raw := range []string{"", "not-a-number", "NaN", "Inf", "92233720368547759"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := domain.ParsePercentageBPS(raw); err == nil {
				t.Fatalf("ParsePercentageBPS(%q) accepted invalid input", raw)
			}
		})
	}
}
