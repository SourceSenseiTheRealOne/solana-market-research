package domain_test

import (
	"testing"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestParseUSDUsesSixDecimalFixedPointRounding(t *testing.T) {
	usd, err := domain.ParseUSD("1.2345678")
	if err != nil {
		t.Fatalf("ParseUSD() error = %v", err)
	}
	if want := int64(1_234_568); usd.Micros != want {
		t.Fatalf("micros = %d, want %d", usd.Micros, want)
	}
}

func TestParseUSDRejectsNegativeValues(t *testing.T) {
	if _, err := domain.ParseUSD("-0.01"); err == nil {
		t.Fatal("ParseUSD() accepted a negative amount")
	}
}

func TestUSDReturnBPSRejectsZeroCost(t *testing.T) {
	if _, err := (domain.USD{Micros: 10_000_000}).ReturnBPS(domain.USD{}); err == nil {
		t.Fatal("ReturnBPS() accepted zero cost")
	}
}

func TestUSDParsesTenDollarsAndComputesReturn(t *testing.T) {
	cost, err := domain.ParseUSD("10")
	if err != nil {
		t.Fatalf("ParseUSD() error = %v", err)
	}
	value, err := domain.ParseUSD("13")
	if err != nil {
		t.Fatalf("ParseUSD() error = %v", err)
	}
	if got, want := cost.Micros, int64(10_000_000); got != want {
		t.Fatalf("$10 micros = %d, want %d", got, want)
	}
	got, err := value.ReturnBPS(cost)
	if err != nil {
		t.Fatalf("ReturnBPS() error = %v", err)
	}
	if want := int64(3000); got != want {
		t.Fatalf("ReturnBPS() = %d, want %d", got, want)
	}
}
