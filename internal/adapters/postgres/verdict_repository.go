package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/ent"
	"github.com/SourceSenseiTheRealOne/solana-market-research/ent/candidate"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

type VerdictRecord struct {
	Provider      string
	Model         string
	PromptVersion string
	InputSHA256   string
	Latency       time.Duration
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	Verdict       domain.Verdict
}

type VerdictRepository struct {
	client *ent.Client
}

func NewVerdictRepository(client *ent.Client) (*VerdictRepository, error) {
	if client == nil {
		return nil, errors.New("verdict repository requires an Ent client")
	}
	return &VerdictRepository{client: client}, nil
}

func (repository *VerdictRepository) Save(ctx context.Context, pool domain.DiscoveredPool, record VerdictRecord) error {
	if err := pool.Validate(); err != nil {
		return fmt.Errorf("validate verdict candidate: %w", err)
	}
	if err := record.Validate(); err != nil {
		return fmt.Errorf("validate verdict record: %w", err)
	}
	storedCandidate, err := repository.client.Candidate.Query().Where(
		candidate.NetworkEQ(pool.Network),
		candidate.MintAddressEQ(pool.MintAddress),
		candidate.PoolAddressEQ(pool.PoolAddress),
	).Only(ctx)
	if err != nil {
		return fmt.Errorf("load verdict candidate: %w", err)
	}
	if err := repository.client.Verdict.Create().
		SetCandidate(storedCandidate).
		SetProvider(record.Provider).
		SetModel(record.Model).
		SetOutcome(string(record.Verdict.Outcome)).
		SetEvidence(verdictEvidence(record)).
		Exec(ctx); err != nil {
		return fmt.Errorf("save verdict: %w", err)
	}
	return nil
}

func (record VerdictRecord) Validate() error {
	if strings.TrimSpace(record.Provider) == "" || strings.TrimSpace(record.Model) == "" || strings.TrimSpace(record.PromptVersion) == "" {
		return errors.New("verdict provider, model, and prompt version are required")
	}
	digest, err := hex.DecodeString(record.InputSHA256)
	if err != nil || len(digest) != 32 {
		return errors.New("verdict input SHA-256 must be a 64-character hexadecimal digest")
	}
	if record.Latency < 0 || record.InputTokens < 0 || record.OutputTokens < 0 || record.TotalTokens < 0 {
		return errors.New("verdict audit metrics cannot be negative")
	}
	knownPostIDs := make(map[string]struct{}, len(record.Verdict.EvidencePostIDs))
	for _, postID := range record.Verdict.EvidencePostIDs {
		knownPostIDs[postID] = struct{}{}
	}
	return record.Verdict.Validate(knownPostIDs)
}

func verdictEvidence(record VerdictRecord) map[string]any {
	verdict := record.Verdict
	return map[string]any{
		"prompt_version": record.PromptVersion,
		"input_sha256":   record.InputSHA256,
		"latency_ms":     record.Latency.Milliseconds(),
		"usage": map[string]any{
			"input_tokens":  record.InputTokens,
			"output_tokens": record.OutputTokens,
			"total_tokens":  record.TotalTokens,
		},
		"response": map[string]any{
			"confidence":               verdict.Confidence,
			"hype_quality_score":       verdict.HypeQualityScore,
			"manipulation_probability": verdict.ManipulationProbability,
			"reasons":                  verdict.Reasons,
			"risk_flags":               verdict.RiskFlags,
			"invalidation_conditions":  verdict.InvalidationConditions,
			"evidence_post_ids":        verdict.EvidencePostIDs,
		},
	}
}
