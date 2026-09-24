package config_test

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/config"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestValidateRejectsUnsafeStrategyLimits(t *testing.T) {
	base := validConfig()

	tests := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{"zero notional", func(c *config.Config) { c.PaperTradeUSD = domain.USD{} }},
		{"too many open positions", func(c *config.Config) { c.MaxOpenPositions = 4 }},
		{"too many daily trades", func(c *config.Config) { c.MaxDailyTrades = 31 }},
		{"zero take profit", func(c *config.Config) { c.TakeProfitBPS = 0 }},
		{"zero stop loss", func(c *config.Config) { c.StopLossBPS = 0 }},
		{"zero five-minute transaction floor", func(c *config.Config) { c.MinFiveMinuteTransactions = 0 }},
		{"zero five-minute buy-share floor", func(c *config.Config) { c.MinFiveMinuteBuyShareBPS = 0 }},
		{"buy-share floor above one hundred percent", func(c *config.Config) { c.MinFiveMinuteBuyShareBPS = 10_001 }},
		{"zero five-minute turnover floor", func(c *config.Config) { c.MinFiveMinuteTurnoverBPS = 0 }},
		{"turnover floor above one hundred percent", func(c *config.Config) { c.MinFiveMinuteTurnoverBPS = 10_001 }},
		{"momentum minimum below negative one hundred percent", func(c *config.Config) { c.MinFiveMinutePriceChangeBPS = -10_001 }},
		{"momentum maximum above one thousand percent", func(c *config.Config) { c.MaxFiveMinutePriceChangeBPS = 100_001 }},
		{"momentum minimum above maximum", func(c *config.Config) { c.MinFiveMinutePriceChangeBPS = c.MaxFiveMinutePriceChangeBPS + 1 }},
		{"zero Hermes confidence floor", func(c *config.Config) { c.MinHermesConfidence = 0 }},
		{"confidence above one hundred", func(c *config.Config) { c.MinHermesConfidence = 101 }},
		{"zero social score floor", func(c *config.Config) { c.MinSocialScore = 0 }},
		{"social score above one hundred", func(c *config.Config) { c.MinSocialScore = 101 }},
		{"zero social author floor", func(c *config.Config) { c.MinSocialUniqueAuthors = 0 }},
		{"zero original-post floor", func(c *config.Config) { c.MinSocialOriginalPosts = 0 }},
		{"zero exact-mint-mention floor", func(c *config.Config) { c.MinSocialExactMintMentions = 0 }},
		{"negative warning-post ceiling", func(c *config.Config) { c.MaxSocialWarningPosts = -1 }},
		{"warning-post ceiling above one hundred", func(c *config.Config) { c.MaxSocialWarningPosts = 101 }},
		{"zero Hermes hype-quality floor", func(c *config.Config) { c.MinHermesHypeQuality = 0 }},
		{"Hermes hype-quality floor above one hundred", func(c *config.Config) { c.MinHermesHypeQuality = 101 }},
		{"negative Hermes manipulation ceiling", func(c *config.Config) { c.MaxHermesManipulationRisk = -1 }},
		{"Hermes manipulation ceiling above one hundred", func(c *config.Config) { c.MaxHermesManipulationRisk = 101 }},
		{"zero admission evidence age", func(c *config.Config) { c.MaxAdmissionEvidenceAge = 0 }},
		{"zero maximum pool age", func(c *config.Config) { c.MaxPoolAge = 0 }},

		{"zero minimum liquidity", func(c *config.Config) { c.MinLiquidityUSD = domain.USD{} }},
		{"negative entry price impact", func(c *config.Config) { c.MaxEntryPriceImpactBPS = -1 }},
		{"zero simulated entry latency", func(c *config.Config) { c.SimulatedEntryLatency = 0 }},
		{"negative paper network fee", func(c *config.Config) { c.PaperNetworkFeeMicros = -1 }},
		{"negative paper priority fee", func(c *config.Config) { c.PaperPriorityFeeMicros = -1 }},
		{"empty report directory", func(c *config.Config) { c.ReportDir = "" }},
		{"wildcard HTTP address", func(c *config.Config) { c.HTTPAddr = ":8080" }},
		{"non-loopback HTTP address", func(c *config.Config) { c.HTTPAddr = "192.0.2.10:8080" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() accepted unsafe strategy configuration")
			}
		})
	}
}

func TestLoadDefaultsPaperAutomationDisabled(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("DATABASE_URL", "postgres://redacted")
	t.Setenv("PAPER_AUTOMATION_ENABLED", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PaperAutomationEnabled {
		t.Fatal("PAPER_AUTOMATION_ENABLED defaulted to true")
	}
	if got, want := cfg.PaperQuoteMint, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"; got != want {
		t.Fatalf("PAPER_QUOTE_MINT default = %q, want canonical Solana Mainnet USDC mint %q", got, want)
	}
	if cfg.MaxPoolAge != 90*time.Minute || cfg.MinLiquidityUSD.Micros != 5_000_000_000 ||
		cfg.MinFiveMinuteTransactions != 20 || cfg.MinFiveMinuteBuyShareBPS != 6_000 ||
		cfg.MinFiveMinuteTurnoverBPS != 1_500 || cfg.MinFiveMinutePriceChangeBPS != 200 ||
		cfg.MaxFiveMinutePriceChangeBPS != 6_000 || cfg.TakeProfitBPS != 5_000 ||
		cfg.StopLossBPS != 2_000 || cfg.MaxHoldDuration != 45*time.Minute ||
		cfg.StrategyVersion != "bold-momentum-v3" || cfg.MinSocialScore != 30 ||
		cfg.MinSocialUniqueAuthors != 3 || cfg.MinSocialOriginalPosts != 2 ||
		cfg.MinSocialExactMintMentions != 2 || cfg.MaxSocialWarningPosts != 1 ||
		cfg.MinHermesHypeQuality != 60 || cfg.MaxHermesManipulationRisk != 35 {
		t.Fatalf("unexpected bold-v2 defaults: %s", cfg.SafeHealth())
	}
}

func TestAutomationValidationRequiresHeliusAndExactCadence(t *testing.T) {
	cfg := validConfig()
	cfg.PaperAutomationEnabled = true
	cfg.HeliusAPIKey = ""
	cfg.JupiterAPIKey = "jupiter"
	cfg.TwitterAPIKey = "twitter"
	cfg.HermesAPIKey = "hermes"
	cfg.PaperQuoteMint = "quote-mint"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted enabled automation without Helius")
	}

	cfg.HeliusAPIKey = "helius"
	cfg.DiscoveryInterval = time.Minute
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted enabled automation without five-minute scan cadence")
	}
}

func TestProviderReadinessIsScopedAndConfigurationIsRedacted(t *testing.T) {
	cfg := validConfig()
	cfg.TwitterAPIKey = "twitter-secret-value"
	cfg.BirdeyeAPIKey = "birdeye-secret-value"
	cfg.HermesAPIKey = "hermes-secret-value"
	cfg.JupiterAPIKey = "jupiter-secret-value"

	cfg.TwitterAPIKey = ""
	if err := cfg.ProviderReady(config.ProviderTwitterAPI); err == nil {
		t.Fatal("Twitter provider unexpectedly reported ready")
	}
	if err := cfg.ProviderReady(config.ProviderJupiter); err != nil {
		t.Fatalf("Jupiter readiness was affected by missing Twitter key: %v", err)
	}
	if err := cfg.ProviderReady(config.ProviderBirdeye); err != nil {
		t.Fatalf("Birdeye readiness was affected by missing Twitter key: %v", err)
	}

	for _, rendered := range []string{fmt.Sprint(cfg), cfg.SafeHealth().String()} {
		for _, secret := range []string{"birdeye-secret-value", "hermes-secret-value", "jupiter-secret-value"} {
			if contains(rendered, secret) {
				t.Fatalf("safe configuration rendering leaked %q", secret)
			}
		}
	}
}

func TestValidateAllowsWildcardOnlyForContainerInternalListener(t *testing.T) {
	cfg := validConfig()
	cfg.AppEnv = "container"
	cfg.HTTPAddr = "0.0.0.0:8080"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected a container-internal listener: %v", err)
	}
}

func TestConfigDerivesCanonicalHeliusMainnetRPCURL(t *testing.T) {
	cfg := validConfig()
	key := "test helius key&value"
	field := reflect.ValueOf(&cfg).Elem().FieldByName("HeliusAPIKey")
	if !field.IsValid() {
		t.Fatal("Config is missing HeliusAPIKey")
	}
	field.SetString(key)
	resolver, ok := any(cfg).(interface{ HeliusMainnetRPCURL() (string, error) })
	if !ok {
		t.Fatal("Config is missing HeliusMainnetRPCURL")
	}

	rpcURL, err := resolver.HeliusMainnetRPCURL()
	if err != nil {
		t.Fatalf("HeliusMainnetRPCURL() error = %v", err)
	}
	parsed, err := url.Parse(rpcURL)
	if err != nil {
		t.Fatalf("HeliusMainnetRPCURL() returned an invalid URL: %v", err)
	}
	if parsed.Scheme != "https" || parsed.Host != "mainnet.helius-rpc.com" || parsed.Path != "/" || parsed.Query().Get("api-key") != key {
		t.Fatalf("HeliusMainnetRPCURL() did not return the canonical Helius Mainnet URL")
	}
	if contains(fmt.Sprint(cfg), key) || contains(cfg.SafeHealth().String(), key) {
		t.Fatal("safe configuration rendering leaked the Helius API key")
	}
}

func validConfig() config.Config {
	return config.Config{
		HTTPAddr:                    "127.0.0.1:8080",
		DatabaseURL:                 "postgres://redacted",
		PaperTradeUSD:               domain.USD{Micros: 100_000_000},
		SimulatedEntryLatency:       2 * time.Second,
		PaperNetworkFeeMicros:       1_000,
		PaperPriorityFeeMicros:      1_000,
		PaperQuoteMint:              "quote-mint",
		MaxOpenPositions:            3,
		MaxDailyTrades:              30,
		TakeProfitBPS:               3000,
		StopLossBPS:                 1500,
		MaxPoolAge:                  90 * 24 * time.Hour,
		MinLiquidityUSD:             domain.USD{Micros: 10_000_000_000},
		MinFiveMinuteTransactions:   10,
		MinFiveMinuteBuyShareBPS:    6_500,
		MinFiveMinuteTurnoverBPS:    1_500,
		MinFiveMinutePriceChangeBPS: 200,
		MaxFiveMinutePriceChangeBPS: 6_000,
		MaxEntryPriceImpactBPS:      1_000,
		MinHermesConfidence:         70,
		MinSocialScore:              30,
		MinSocialUniqueAuthors:      3,
		MinSocialOriginalPosts:      2,
		MinSocialExactMintMentions:  2,
		MaxSocialWarningPosts:       1,
		MinHermesHypeQuality:        60,
		MaxHermesManipulationRisk:   35,
		MaxAdmissionEvidenceAge:     15 * time.Minute,
		MaxHoldDuration:             time.Hour,
		DiscoveryInterval:           5 * time.Minute,
		PositionMarkInterval:        30 * time.Second,
		StrategyVersion:             "test-v1",
		LogDir:                      "var/log",
		ReportDir:                   "var/reports",
	}
}

func contains(value, fragment string) bool {
	return len(fragment) > 0 && len(value) >= len(fragment) && stringContains(value, fragment)
}

func stringContains(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
