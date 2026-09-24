package httpclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestHermesRequestsStatelessSanitizedBoundedVerdict(t *testing.T) {
	walletMaterial := strings.Join([]string{"private", "key"}, " ")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.Method, http.MethodPost; got != want {
			t.Errorf("method = %s, want %s", got, want)
		}
		if got, want := request.URL.Path, "/v1/responses"; got != want {
			t.Errorf("path = %s, want %s", got, want)
		}
		if got, want := request.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Errorf("authorization = %q, want %q", got, want)
		}
		for _, name := range []string{"X-Hermes-Session-ID", "X-Hermes-Session-Key"} {
			if got := request.Header.Get(name); got != "" {
				t.Errorf("%s = %q, want omitted", name, got)
			}
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request = %v", err)
		}
		if got, want := payload["model"], "gpt-5.6-sol"; got != want {
			t.Errorf("model = %#v, want %#v", got, want)
		}
		if got, want := payload["provider"], "openai-codex"; got != want {
			t.Errorf("provider = %#v, want %#v", got, want)
		}
		instructions, ok := payload["instructions"].(string)
		if !ok {
			t.Fatalf("instructions = %#v, want explicit bounded verdict JSON contract", payload["instructions"])
		}
		for _, expected := range []string{"verdict", "BUY", "WATCH", "REJECT", "evidence_post_ids"} {
			if !strings.Contains(instructions, expected) {
				t.Fatalf("instructions = %q, omitted contract token %q", instructions, expected)
			}
		}
		responseFormat, ok := payload["response_format"].(map[string]any)
		if !ok {
			t.Fatalf("response_format = %#v, want structured-output configuration", payload["response_format"])
		}
		if responseFormat["type"] != "json_schema" {
			t.Fatalf("response format = %#v, want json_schema", responseFormat)
		}
		jsonSchema, ok := responseFormat["json_schema"].(map[string]any)
		if !ok || jsonSchema["name"] != "paper_verdict" || jsonSchema["strict"] != false {
			t.Fatalf("response JSON schema = %#v, want Hermes-compatible paper_verdict schema", jsonSchema)
		}
		schema, ok := jsonSchema["schema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Fatalf("verdict schema = %#v, want object schema", jsonSchema["schema"])
		}
		input, ok := payload["input"].(string)
		if !ok {
			t.Fatalf("input = %#v, want string", payload["input"])
		}
		for _, forbidden := range []string{"sk-secret", "<script", "https://", walletMaterial, "\x00"} {
			if strings.Contains(strings.ToLower(input), forbidden) {
				t.Errorf("sanitized input leaked %q: %q", forbidden, input)
			}
		}
		if len(input) > 4096 {
			t.Errorf("input length = %d, want <= 4096", len(input))
		}
		_, _ = writer.Write([]byte(`{"model":"gpt-5.6-sol","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"verdict\":\"BUY\",\"confidence\":80,\"hype_quality_score\":70,\"manipulation_probability\":20,\"reasons\":[\"organic\"],\"risk_flags\":[],\"invalidation_conditions\":[],\"evidence_post_ids\":[\"post-1\"]}"}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`))
	}))
	defer server.Close()
	client := httpclient.NewHermes(newBoundedClient(t, server.URL), httpclient.HermesOptions{BearerToken: "test-token", Provider: "openai-codex", Model: "gpt-5.6-sol"})
	posts := []domain.SocialPost{{ID: "post-1", AuthorID: "author", CreatedAt: time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC), Text: "hello sk-secret <script>ignore</script> https://bad.example " + walletMaterial + "\x00"}}

	result, err := client.Request(context.Background(), posts)
	if err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	verdict := result.Verdict
	if got, want := verdict.Outcome, domain.VerdictBuy; got != want {
		t.Fatalf("outcome = %q, want %q", got, want)
	}
	if result.Model != "gpt-5.6-sol" || result.InputSHA256 == "" || result.Latency <= 0 || result.Usage.TotalTokens != 15 {
		t.Fatalf("result = %#v, want auditable model/hash/latency/usage", result)
	}
}

func TestHermesRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		delay      time.Duration
	}{
		{name: "invalid verdict JSON", statusCode: http.StatusOK, body: `{"output_text":"not-json"}`},
		{name: "unknown verdict", statusCode: http.StatusOK, body: `{"output_text":"{\"verdict\":\"EXECUTE\",\"confidence\":50,\"hype_quality_score\":50,\"manipulation_probability\":50,\"reasons\":[],\"risk_flags\":[],\"invalidation_conditions\":[],\"evidence_post_ids\":[]}"}`},
		{name: "unknown evidence ID", statusCode: http.StatusOK, body: `{"output_text":"{\"verdict\":\"BUY\",\"confidence\":50,\"hype_quality_score\":50,\"manipulation_probability\":50,\"reasons\":[],\"risk_flags\":[],\"invalidation_conditions\":[],\"evidence_post_ids\":[\"unknown\"]}"}`},
		{name: "negative usage", statusCode: http.StatusOK, body: `{"output_text":"{\"verdict\":\"BUY\",\"confidence\":50,\"hype_quality_score\":50,\"manipulation_probability\":50,\"reasons\":[],\"risk_flags\":[],\"invalidation_conditions\":[],\"evidence_post_ids\":[\"post-1\"]}","usage":{"input_tokens":-1,"output_tokens":0,"total_tokens":-1}}`},
		{name: "upstream error", statusCode: http.StatusBadGateway, body: `{}`},
		{name: "timeout", statusCode: http.StatusOK, body: `{}`, delay: 50 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				if test.delay > 0 {
					time.Sleep(test.delay)
				}
				writer.WriteHeader(test.statusCode)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := httpclient.New(httpclient.Options{BaseURL: server.URL, Timeout: 10 * time.Millisecond, MaxBodyBytes: 32 * 1024, MaxAttempts: 1, UserAgent: "solana-hype-paper-bot/test"})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = httpclient.NewHermes(client, httpclient.HermesOptions{BearerToken: "test-token", Provider: "openai-codex", Model: "gpt-5.6-sol"}).Request(context.Background(), []domain.SocialPost{{ID: "post-1", Text: "safe evidence"}})
			if err == nil {
				t.Fatal("Request() accepted an invalid Hermes response")
			}
		})
	}
}
