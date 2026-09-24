package httpclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/adapters/httpclient"
	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

func TestTwitterAPIIOFetchesOneBoundedExactMintSearch(t *testing.T) {
	startsAt := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got, want := request.Method, http.MethodGet; got != want {
			t.Errorf("method = %s, want %s", got, want)
		}
		if got, want := request.URL.Path, "/twitter/tweet/advanced_search"; got != want {
			t.Errorf("path = %s, want %s", got, want)
		}
		if got, want := request.Header.Get("X-API-Key"), "test-key"; got != want {
			t.Errorf("X-API-Key = %q, want %q", got, want)
		}
		if got, want := request.URL.Query().Get("query"), `"mint-address" since_time:1787065200 until_time:1787066100`; got != want {
			t.Errorf("query = %q, want %q", got, want)
		}
		if got, want := request.URL.Query().Get("queryType"), "Latest"; got != want {
			t.Errorf("queryType = %q, want %q", got, want)
		}
		if got := request.URL.Query().Get("cursor"); got != "" {
			t.Errorf("cursor = %q, want omitted", got)
		}
		_, _ = writer.Write([]byte(`{"tweets":[{"id":"post-1","text":" mint-address  now\n","likeCount":2,"replyCount":1,"retweetCount":3,"quoteCount":4,"createdAt":"2026-08-18T15:01:00Z","isReply":false,"retweeted_tweet":{"id":"original"},"author":{"id":"author-1","followers":100,"createdAt":"2020-01-01T00:00:00Z"}}],"has_next_page":true,"next_cursor":"ignored"}`))
	}))
	defer server.Close()
	window := domain.SocialWindow{MintAddress: "mint-address", StartsAt: startsAt, EndsAt: startsAt.Add(15 * time.Minute)}

	posts, err := httpclient.NewTwitterAPIIO(newBoundedClient(t, server.URL), "test-key").Fetch(context.Background(), window)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got, want := len(posts), 1; got != want {
		t.Fatalf("posts = %d, want %d", got, want)
	}
	if got, want := posts[0].ID, "post-1"; got != want {
		t.Fatalf("post ID = %q, want %q", got, want)
	}
	if !posts[0].IsRepost || posts[0].Text != "mint-address now" {
		t.Fatalf("post = %#v, want normalized bounded evidence", posts[0])
	}
}

func TestTwitterAPIIOLimitsNormalizedEvidenceToTwentyPosts(t *testing.T) {
	startsAt := time.Date(2026, time.August, 18, 15, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		tweets := make([]map[string]any, 21)
		for index := range tweets {
			tweets[index] = map[string]any{"id": "post", "text": "mint-address", "createdAt": "2026-08-18T15:01:00Z", "author": map[string]any{"id": "author", "followers": 1}}
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"tweets": tweets, "has_next_page": true, "next_cursor": "ignored"})
	}))
	defer server.Close()

	posts, err := httpclient.NewTwitterAPIIO(newBoundedClient(t, server.URL), "test-key").Fetch(context.Background(), domain.SocialWindow{MintAddress: "mint-address", StartsAt: startsAt, EndsAt: startsAt.Add(15 * time.Minute)})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got, want := len(posts), 20; got != want {
		t.Fatalf("posts = %d, want %d", got, want)
	}
}
