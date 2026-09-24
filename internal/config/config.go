package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
	"github.com/caarlos0/env/v11"
)

type Provider string

const (
	// CanonicalSolanaMainnetUSDCMint is Circle's native six-decimal USDC mint.
	// PaperTradeUSD uses microdollars, so paper admission is restricted to it.
	CanonicalSolanaMainnetUSDCMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"

	ProviderBirdeye    Provider = "birdeye"
	ProviderTwitterAPI Provider = "twitterapiio"
	ProviderHermes     Provider = "hermes"
	ProviderJupiter    Provider = "jupiter"
	ProviderHelius     Provider = "helius"
)

type Config struct {
	AppEnv                      string        `env:"APP_ENV" envDefault:"local"`
	HTTPAddr                    string        `env:"HTTP_ADDR" envDefault:"127.0.0.1:8080"`
	DatabaseURL                 string        `env:"DATABASE_URL"`
	PaperAutomationEnabled      bool          `env:"PAPER_AUTOMATION_ENABLED" envDefault:"false"`
	SolanaRPCURL                string        `env:"SOLANA_RPC_URL"`
	HeliusAPIKey                string        `env:"HELIUS_API_KEY"`
	BirdeyeAPIKey               string        `env:"BIRDEYE_API_KEY"`
	TwitterAPIKey               string        `env:"TWITTERAPIIO_API_KEY"`
	HermesAPIURL                string        `env:"HERMES_API_URL" envDefault:"http://host.docker.internal:8642/v1"`
	HermesAPIKey                string        `env:"HERMES_API_KEY"`
	JupiterAPIURL               string        `env:"JUPITER_API_URL" envDefault:"https://api.jup.ag"`
	JupiterAPIKey               string        `env:"JUPITER_API_KEY"`
	DiscoveryInterval           time.Duration `env:"DISCOVERY_INTERVAL" envDefault:"5m"`
	PositionMarkInterval        time.Duration `env:"POSITION_MARK_INTERVAL" envDefault:"30s"`
	SimulatedEntryLatency       time.Duration `env:"SIMULATED_ENTRY_LATENCY" envDefault:"2s"`
	PaperTradeUSD               domain.USD    `env:"PAPER_TRADE_USD" envDefault:"100"`
	PaperQuoteMint              string        `env:"PAPER_QUOTE_MINT" envDefault:"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"`
	PaperNetworkFeeMicros       int64         `env:"PAPER_NETWORK_FEE_MICROS" envDefault:"1000"`
	PaperPriorityFeeMicros      int64         `env:"PAPER_PRIORITY_FEE_MICROS" envDefault:"1000"`
	MaxOpenPositions            int           `env:"MAX_OPEN_POSITIONS" envDefault:"3"`
	MaxDailyTrades              int           `env:"MAX_DAILY_TRADES" envDefault:"30"`
	MaxPoolAge                  time.Duration `env:"MAX_POOL_AGE" envDefault:"90m"`
	MinLiquidityUSD             domain.USD    `env:"MIN_LIQUIDITY_USD" envDefault:"5000"`
	MinFiveMinuteTransactions   int           `env:"MIN_FIVE_MINUTE_TRANSACTIONS" envDefault:"20"`
	MinFiveMinuteBuyShareBPS    int64         `env:"MIN_FIVE_MINUTE_BUY_SHARE_BPS" envDefault:"6000"`
	MinFiveMinuteTurnoverBPS    int64         `env:"MIN_FIVE_MINUTE_TURNOVER_BPS" envDefault:"1500"`
	MinFiveMinutePriceChangeBPS int64         `env:"MIN_FIVE_MINUTE_PRICE_CHANGE_BPS" envDefault:"200"`
	MaxFiveMinutePriceChangeBPS int64         `env:"MAX_FIVE_MINUTE_PRICE_CHANGE_BPS" envDefault:"6000"`
	MaxEntryPriceImpactBPS      int64         `env:"MAX_ENTRY_PRICE_IMPACT_BPS" envDefault:"1000"`
	MinHermesConfidence         int           `env:"MIN_HERMES_CONFIDENCE" envDefault:"70"`
	MinSocialScore              int           `env:"MIN_SOCIAL_SCORE" envDefault:"30"`
	MinSocialUniqueAuthors      int           `env:"MIN_SOCIAL_UNIQUE_AUTHORS" envDefault:"3"`
	MinSocialOriginalPosts      int           `env:"MIN_SOCIAL_ORIGINAL_POSTS" envDefault:"2"`
	MinSocialExactMintMentions  int           `env:"MIN_SOCIAL_EXACT_MINT_MENTIONS" envDefault:"2"`
	MaxSocialWarningPosts       int           `env:"MAX_SOCIAL_WARNING_POSTS" envDefault:"1"`
	MinHermesHypeQuality        int           `env:"MIN_HERMES_HYPE_QUALITY" envDefault:"60"`
	MaxHermesManipulationRisk   int           `env:"MAX_HERMES_MANIPULATION_RISK" envDefault:"35"`
	MaxAdmissionEvidenceAge     time.Duration `env:"MAX_ADMISSION_EVIDENCE_AGE" envDefault:"15m"`
	TakeProfitBPS               int64         `env:"TAKE_PROFIT_BPS" envDefault:"5000"`
	StopLossBPS                 int64         `env:"STOP_LOSS_BPS" envDefault:"2000"`
	MaxHoldDuration             time.Duration `env:"MAX_HOLD_DURATION" envDefault:"45m"`
	StrategyVersion             string        `env:"STRATEGY_VERSION" envDefault:"bold-momentum-v3"`
	LogDir                      string        `env:"LOG_DIR" envDefault:"var/log"`
	ReportDir                   string        `env:"REPORT_DIR" envDefault:"var/reports"`
}

type Health struct{ ReadyProviders []Provider }

func (Health) String() string { return "configuration health (redacted)" }
func (Config) String() string { return "configuration (redacted)" }

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse configuration: %w", err)
	}
	return cfg, cfg.Validate()
}

func (c Config) Validate() error {
	if c.PaperTradeUSD.Micros <= 0 {
		return errors.New("PAPER_TRADE_USD must be positive")
	}
	if c.MaxOpenPositions < 1 || c.MaxOpenPositions > 3 {
		return errors.New("MAX_OPEN_POSITIONS must be between 1 and 3")
	}
	if c.MaxDailyTrades < 1 || c.MaxDailyTrades > 30 {
		return errors.New("MAX_DAILY_TRADES must be between 1 and 30")
	}
	if c.MinFiveMinuteTransactions < 1 {
		return errors.New("MIN_FIVE_MINUTE_TRANSACTIONS must be positive")
	}
	if c.MinFiveMinuteBuyShareBPS < 1 || c.MinFiveMinuteBuyShareBPS > 10_000 {
		return errors.New("MIN_FIVE_MINUTE_BUY_SHARE_BPS must be between 1 and 10000")
	}
	if c.MinFiveMinuteTurnoverBPS < 1 || c.MinFiveMinuteTurnoverBPS > 10_000 {
		return errors.New("MIN_FIVE_MINUTE_TURNOVER_BPS must be between 1 and 10000")
	}
	if c.MinFiveMinutePriceChangeBPS < -10_000 || c.MinFiveMinutePriceChangeBPS > 100_000 || c.MaxFiveMinutePriceChangeBPS < -10_000 || c.MaxFiveMinutePriceChangeBPS > 100_000 || c.MinFiveMinutePriceChangeBPS > c.MaxFiveMinutePriceChangeBPS {
		return errors.New("five-minute price-change bounds are invalid")
	}
	if c.MaxPoolAge <= 0 {
		return errors.New("MAX_POOL_AGE must be positive")
	}
	if c.MinLiquidityUSD.Micros <= 0 {
		return errors.New("MIN_LIQUIDITY_USD must be positive")
	}

	if c.MaxEntryPriceImpactBPS < 0 {
		return errors.New("MAX_ENTRY_PRICE_IMPACT_BPS must be non-negative")
	}
	if c.MinHermesConfidence < 1 || c.MinHermesConfidence > 100 {
		return errors.New("MIN_HERMES_CONFIDENCE must be between 1 and 100")
	}
	if err := (domain.SocialPolicy{
		MinScore: c.MinSocialScore, MinUniqueAuthors: c.MinSocialUniqueAuthors,
		MinOriginalPosts: c.MinSocialOriginalPosts, MinExactMintMentions: c.MinSocialExactMintMentions,
		MaxWarningPosts: c.MaxSocialWarningPosts,
	}).Validate(); err != nil || c.MaxSocialWarningPosts > 100 {
		return errors.New("social policy configuration is invalid")
	}
	if err := (domain.VerdictPolicy{
		MinimumConfidence: c.MinHermesConfidence, MinimumHypeQuality: c.MinHermesHypeQuality,
		MaximumManipulationProbability: c.MaxHermesManipulationRisk,
	}).Validate(); err != nil {
		return errors.New("Hermes verdict policy configuration is invalid")
	}
	if c.MaxAdmissionEvidenceAge <= 0 {
		return errors.New("MAX_ADMISSION_EVIDENCE_AGE must be positive")
	}
	if c.SimulatedEntryLatency <= 0 {
		return errors.New("SIMULATED_ENTRY_LATENCY must be positive")
	}
	if c.PaperNetworkFeeMicros < 0 || c.PaperPriorityFeeMicros < 0 {
		return errors.New("paper fee estimates must be non-negative")
	}
	if c.TakeProfitBPS <= 0 || c.StopLossBPS <= 0 {
		return errors.New("take-profit and stop-loss basis points must be positive")
	}
	if strings.TrimSpace(c.StrategyVersion) == "" {
		return errors.New("STRATEGY_VERSION must not be empty")
	}
	if c.MaxHoldDuration <= 0 || c.DiscoveryInterval <= 0 || c.PositionMarkInterval <= 0 {
		return errors.New("durations must be positive")
	}
	if c.PaperAutomationEnabled {
		if c.DiscoveryInterval != 5*time.Minute || c.PositionMarkInterval != 30*time.Second {
			return errors.New("paper automation requires 5m discovery and 30s monitor cadence")
		}
		if c.PaperQuoteMint != CanonicalSolanaMainnetUSDCMint {
			return errors.New("PAPER_QUOTE_MINT must be canonical Solana Mainnet USDC when paper automation is enabled")
		}
		for _, provider := range []Provider{ProviderTwitterAPI, ProviderHermes, ProviderJupiter, ProviderHelius} {
			if err := c.ProviderReady(provider); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(c.ReportDir) == "" {
		return errors.New("REPORT_DIR must not be empty")
	}
	if !isPermittedHTTPAddress(c.AppEnv, c.HTTPAddr) {
		return errors.New("HTTP_ADDR must use a literal loopback IP and TCP port, except 0.0.0.0 in container mode")
	}
	return nil
}

func isPermittedHTTPAddress(appEnv, address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(appEnv), "container") && host == "0.0.0.0" {
		return isTCPPort(port)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	return isTCPPort(port)
}

func isTCPPort(port string) bool {
	parsedPort, err := strconv.ParseUint(port, 10, 16)
	return err == nil && parsedPort > 0
}

func (c Config) ProviderReady(provider Provider) error {
	var key string
	switch provider {
	case ProviderBirdeye:
		key = c.BirdeyeAPIKey
	case ProviderTwitterAPI:
		key = c.TwitterAPIKey
	case ProviderHermes:
		key = c.HermesAPIKey
	case ProviderJupiter:
		key = c.JupiterAPIKey
	case ProviderHelius:
		key = c.HeliusAPIKey
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("%s provider is not configured", provider)
	}
	return nil
}

func (c Config) HeliusMainnetRPCURL() (string, error) {
	apiKey := strings.TrimSpace(c.HeliusAPIKey)
	if apiKey == "" {
		return "", errors.New("Helius Mainnet RPC provider is not configured")
	}
	endpoint := url.URL{Scheme: "https", Host: "mainnet.helius-rpc.com", Path: "/"}
	parameters := endpoint.Query()
	parameters.Set("api-key", apiKey)
	endpoint.RawQuery = parameters.Encode()
	return endpoint.String(), nil
}

func (c Config) SafeHealth() Health {
	providers := make([]Provider, 0, 4)
	for _, provider := range []Provider{ProviderBirdeye, ProviderTwitterAPI, ProviderHermes, ProviderJupiter, ProviderHelius} {
		if c.ProviderReady(provider) == nil {
			providers = append(providers, provider)
		}
	}
	return Health{ReadyProviders: providers}
}
