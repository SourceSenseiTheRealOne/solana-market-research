package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/SourceSenseiTheRealOne/solana-market-research/internal/domain"
)

const (
	twitterSearchPath = "/twitter/tweet/advanced_search"
	socialWindowSize  = 15 * time.Minute
	maxSocialPosts    = 20
	maxEvidenceRunes  = 280
)

type TwitterAPIIO struct {
	client *Client
	apiKey string
}

func NewTwitterAPIIO(client *Client, apiKey string) *TwitterAPIIO {
	return &TwitterAPIIO{client: client, apiKey: apiKey}
}

func (provider *TwitterAPIIO) Fetch(ctx context.Context, window domain.SocialWindow) ([]domain.SocialPost, error) {
	if provider.client == nil || strings.TrimSpace(provider.apiKey) == "" {
		return nil, errors.New("TwitterAPI.io provider is not configured")
	}
	if err := validateSocialWindow(window); err != nil {
		return nil, err
	}
	query := fmt.Sprintf("\"%s\" since_time:%d until_time:%d", window.MintAddress, window.StartsAt.Unix(), window.EndsAt.Unix())
	values := url.Values{"query": {query}, "queryType": {"Latest"}}
	var response twitterSearchResponse
	if err := provider.client.GetJSONWithHeaders(ctx, twitterSearchPath, values, http.Header{"X-API-Key": []string{provider.apiKey}}, &response); err != nil {
		return nil, fmt.Errorf("fetch TwitterAPI.io social evidence: %w", err)
	}
	posts := make([]domain.SocialPost, 0, min(len(response.Tweets), maxSocialPosts))
	for _, tweet := range response.Tweets {
		if len(posts) == maxSocialPosts {
			break
		}
		post, err := tweet.normalize()
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, nil
}

func validateSocialWindow(window domain.SocialWindow) error {
	if strings.TrimSpace(window.MintAddress) == "" || strings.ContainsAny(window.MintAddress, "\"\t\r\n ") {
		return errors.New("social evidence requires a non-empty mint address without query syntax")
	}
	if window.StartsAt.IsZero() || window.EndsAt.Sub(window.StartsAt) != socialWindowSize {
		return errors.New("social evidence requires an exact fifteen-minute UTC window")
	}
	return nil
}

type twitterSearchResponse struct {
	Tweets []twitterTweet `json:"tweets"`
}
type twitterTweet struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	LikeCount    int64  `json:"likeCount"`
	ReplyCount   int64  `json:"replyCount"`
	RetweetCount int64  `json:"retweetCount"`
	QuoteCount   int64  `json:"quoteCount"`
	CreatedAt    string `json:"createdAt"`
	IsReply      bool   `json:"isReply"`
	Retweeted    *struct {
		ID string `json:"id"`
	} `json:"retweeted_tweet"`
	Author struct {
		ID        string `json:"id"`
		Followers int64  `json:"followers"`
		CreatedAt string `json:"createdAt"`
	} `json:"author"`
}

func (tweet twitterTweet) normalize() (domain.SocialPost, error) {
	if strings.TrimSpace(tweet.ID) == "" || strings.TrimSpace(tweet.Author.ID) == "" {
		return domain.SocialPost{}, errors.New("TwitterAPI.io tweet is missing post or author ID")
	}
	createdAt, err := parseTwitterTime(tweet.CreatedAt)
	if err != nil {
		return domain.SocialPost{}, fmt.Errorf("parse TwitterAPI.io tweet time: %w", err)
	}
	authorCreatedAt, err := optionalTwitterTime(tweet.Author.CreatedAt)
	if err != nil {
		return domain.SocialPost{}, fmt.Errorf("parse TwitterAPI.io author time: %w", err)
	}
	return domain.SocialPost{ID: tweet.ID, AuthorID: tweet.Author.ID, CreatedAt: createdAt, AuthorCreatedAt: authorCreatedAt, Text: sanitizeEvidenceText(tweet.Text), Likes: maxInt64(tweet.LikeCount, 0), Replies: maxInt64(tweet.ReplyCount, 0), RepostCount: maxInt64(tweet.RetweetCount, 0), QuoteCount: maxInt64(tweet.QuoteCount, 0), Followers: maxInt64(tweet.Author.Followers, 0), IsReply: tweet.IsReply, IsRepost: tweet.Retweeted != nil}, nil
}

func parseTwitterTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "Mon Jan 02 15:04:05 -0700 2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, errors.New("unsupported timestamp")
}
func optionalTwitterTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	return parseTwitterTime(value)
}
func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}
func sanitizeEvidenceText(value string) string {
	var builder strings.Builder
	for _, character := range strings.TrimSpace(value) {
		if !unicode.IsControl(character) || character == ' ' || character == '\n' || character == '\t' {
			builder.WriteRune(character)
		}
	}
	text := strings.Join(strings.Fields(builder.String()), " ")
	if len([]rune(text)) <= maxEvidenceRunes {
		return text
	}
	return string([]rune(text)[:maxEvidenceRunes])
}
