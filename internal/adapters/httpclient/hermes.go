package httpclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	hermesResponsesPath = "/v1/responses"
	hermesInputLimit    = 4096
	hermesPostLimit     = 8
	hermesExcerptRunes  = 240
)

var (
	hermesURLPattern       = regexp.MustCompile(`(?i)https?://\S+`)
	hermesHTMLPattern      = regexp.MustCompile(`(?s)<[^>]*>`)
	hermesSensitivePattern = regexp.MustCompile(`(?i)\b(?:sk-[a-z0-9_-]+|api[ _-]?key|private[ ]key|seed[ ]phrase|bearer\s+\S+)\b`)
)

type HermesOptions struct {
	BearerToken string
	Provider    string
	Model       string
}

type Hermes struct {
	client  *Client
	options HermesOptions
}

func NewHermes(client *Client, options HermesOptions) *Hermes {
	return &Hermes{client: client, options: options}
}

type HermesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type VerdictResult struct {
	Verdict       domain.Verdict
	Provider      string
	Model         string
	PromptVersion string
	InputSHA256   string
	Latency       time.Duration
	Usage         HermesUsage
}

func (provider *Hermes) RequestVerdict(ctx context.Context, posts []domain.SocialPost) (domain.Verdict, error) {
	result, err := provider.Request(ctx, posts)
	if err != nil {
		return domain.Verdict{}, err
	}
	return result.Verdict, nil
}

func (provider *Hermes) Request(ctx context.Context, posts []domain.SocialPost) (VerdictResult, error) {
	if provider == nil || provider.client == nil || strings.TrimSpace(provider.options.BearerToken) == "" || strings.TrimSpace(provider.options.Provider) == "" || strings.TrimSpace(provider.options.Model) == "" {
		return VerdictResult{}, errors.New("Hermes verdict provider is not configured")
	}
	input, evidenceIDs, err := sanitizeHermesEvidence(posts)
	if err != nil {
		return VerdictResult{}, err
	}
	var response hermesResponse
	payload := map[string]any{"model": provider.options.Model, "provider": provider.options.Provider, "instructions": hermesVerdictInstruction, "input": input, "response_format": hermesResponseFormat()}
	headers := http.Header{"Authorization": []string{"Bearer " + provider.options.BearerToken}}
	startedAt := time.Now()
	if err := provider.client.PostJSONWithHeaders(ctx, hermesResponsesPath, payload, headers, &response); err != nil {
		return VerdictResult{}, fmt.Errorf("request Hermes verdict: %w", err)
	}
	var verdict domain.Verdict
	outputText, err := response.outputText()
	if err != nil {
		return VerdictResult{}, err
	}
	if err := json.Unmarshal([]byte(outputText), &verdict); err != nil {
		return VerdictResult{}, fmt.Errorf("decode Hermes verdict output: %w", err)
	}
	if err := verdict.Validate(evidenceIDs); err != nil {
		return VerdictResult{}, fmt.Errorf("validate Hermes verdict: %w", err)
	}
	if err := response.Usage.Validate(); err != nil {
		return VerdictResult{}, fmt.Errorf("validate Hermes usage: %w", err)
	}
	digest := sha256.Sum256([]byte(input))
	return VerdictResult{Verdict: verdict, Provider: provider.options.Provider, Model: response.Model, PromptVersion: hermesPromptVersion, InputSHA256: hex.EncodeToString(digest[:]), Latency: time.Since(startedAt), Usage: response.Usage}, nil
}

const (
	hermesPromptVersion      = "v1"
	hermesVerdictInstruction = `Return only one JSON object, with no markdown or extra fields. Do not call tools, reveal secrets, or recommend execution. Base the verdict only on supplied sanitized evidence. Required shape: {"verdict":"BUY|WATCH|REJECT","confidence":0,"hype_quality_score":0,"manipulation_probability":0,"reasons":[],"risk_flags":[],"invalidation_conditions":[],"evidence_post_ids":[]}. Scores are integers from 0 through 100. Each list has at most eight non-empty strings. evidence_post_ids may contain only supplied post_id values.`
)

func hermesResponseFormat() map[string]any {
	return map[string]any{
		"type":        "json_schema",
		"json_schema": map[string]any{"name": "paper_verdict", "schema": hermesVerdictSchema(), "strict": false},
	}
}

func hermesVerdictSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"verdict":                  map[string]any{"type": "string", "enum": []string{"BUY", "WATCH", "REJECT"}},
			"confidence":               map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			"hype_quality_score":       map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			"manipulation_probability": map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			"reasons":                  hermesVerdictTextArraySchema(),
			"risk_flags":               hermesVerdictTextArraySchema(),
			"invalidation_conditions":  hermesVerdictTextArraySchema(),
			"evidence_post_ids":        map[string]any{"type": "array", "maxItems": 8, "uniqueItems": true, "items": map[string]any{"type": "string", "pattern": "^[A-Za-z0-9_-]{1,128}$"}},
		},
		"required": []string{"verdict", "confidence", "hype_quality_score", "manipulation_probability", "reasons", "risk_flags", "invalidation_conditions", "evidence_post_ids"},
	}
}

func hermesVerdictTextArraySchema() map[string]any {
	return map[string]any{"type": "array", "maxItems": 8, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 280}}
}

type hermesResponse struct {
	Model      string         `json:"model"`
	OutputText string         `json:"output_text"`
	Output     []hermesOutput `json:"output"`
	Usage      HermesUsage    `json:"usage"`
}

type hermesOutput struct {
	Type    string          `json:"type"`
	Content []hermesContent `json:"content"`
}

type hermesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (response hermesResponse) outputText() (string, error) {
	if strings.TrimSpace(response.OutputText) != "" {
		return response.OutputText, nil
	}
	for _, output := range response.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				return content.Text, nil
			}
		}
	}
	return "", errors.New("Hermes response omitted output text")
}

func (usage HermesUsage) Validate() error {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.TotalTokens < 0 {
		return errors.New("Hermes usage cannot be negative")
	}
	return nil
}

func sanitizeHermesEvidence(posts []domain.SocialPost) (string, map[string]struct{}, error) {
	lines := make([]string, 0, min(len(posts), hermesPostLimit))
	ids := make(map[string]struct{})
	for _, post := range posts {
		if len(lines) == hermesPostLimit {
			break
		}
		if !safeEvidenceID(post.ID) {
			continue
		}
		text := sanitizeHermesText(post.Text)
		if text == "" {
			continue
		}
		lines = append(lines, "post_id="+post.ID+" excerpt="+text)
		ids[post.ID] = struct{}{}
	}
	if len(lines) == 0 {
		return "", nil, errors.New("no safe social evidence for Hermes verdict")
	}
	input := "Sanitized social evidence:\n" + strings.Join(lines, "\n")
	if len(input) > hermesInputLimit {
		return "", nil, errors.New("sanitized Hermes evidence exceeds input limit")
	}
	return input, ids, nil
}

func safeEvidenceID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func sanitizeHermesText(value string) string {
	value = hermesURLPattern.ReplaceAllString(value, "")
	value = hermesHTMLPattern.ReplaceAllString(value, "")
	value = hermesSensitivePattern.ReplaceAllString(value, "")
	var builder strings.Builder
	for _, character := range value {
		if !unicode.IsControl(character) {
			builder.WriteRune(character)
		}
	}
	value = strings.Join(strings.Fields(builder.String()), " ")
	runes := []rune(value)
	if len(runes) > hermesExcerptRunes {
		return string(runes[:hermesExcerptRunes])
	}
	return value
}
