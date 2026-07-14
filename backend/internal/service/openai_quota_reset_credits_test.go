package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type resetCreditsAccountRepo struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *resetCreditsAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	account, ok := r.accounts[id]
	if !ok {
		return nil, fmt.Errorf("account %d not found", id)
	}
	return account, nil
}

type resetCreditsTokenCache struct {
	tokens map[string]string
}

func (c *resetCreditsTokenCache) GetAccessToken(_ context.Context, key string) (string, error) {
	if token, ok := c.tokens[key]; ok {
		return token, nil
	}
	return "", errors.New("token not found")
}

func (c *resetCreditsTokenCache) SetAccessToken(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (c *resetCreditsTokenCache) DeleteAccessToken(_ context.Context, _ string) error { return nil }

func (c *resetCreditsTokenCache) AcquireRefreshLock(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (c *resetCreditsTokenCache) ReleaseRefreshLock(_ context.Context, _ string) error {
	return nil
}

func newResetCreditsRedirectingFactory(server *httptest.Server) PrivacyClientFactory {
	targetURL, _ := url.Parse(server.URL)
	return func(_ string) (*req.Client, error) {
		client := req.C().WrapRoundTripFunc(func(roundTripper req.RoundTripper) req.RoundTripFunc {
			return func(request *req.Request) (*req.Response, error) {
				request.URL.Scheme = targetURL.Scheme
				request.URL.Host = targetURL.Host
				return roundTripper.RoundTrip(request)
			}
		})
		return client, nil
	}
}

func TestParseOpenAIRateLimitResetCreditDetails_PreservesAvailableCreditOrder(t *testing.T) {
	body := []byte(`{
		"availableCount":"2",
		"credits":[
			{"reset_type":"codex_rate_limits","status":"redeemed","expires_at":"2026-07-01T04:05:06Z"},
			{"reset_type":"codex_rate_limits","status":"available","expires_at":"2026-07-04T04:05:06Z"},
			{"resetType":"codex_rate_limits","status":"available","expiresAt":"2026-07-03T04:05:06Z"},
			{"reset_type":"other","status":"available","expires_at":"2026-07-02T04:05:06Z"}
		]
	}`)

	details, err := parseOpenAIRateLimitResetCreditDetails(body)
	require.NoError(t, err)
	require.NotNil(t, details.AvailableCount)
	require.Equal(t, 2, *details.AvailableCount)
	require.Equal(t, []OpenAIRateLimitResetCreditDetail{
		{ExpiresAt: "2026-07-04T04:05:06Z"},
		{ExpiresAt: "2026-07-03T04:05:06Z"},
	}, details.Credits)
}

func TestQueryUsageResetCreditCountPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		usageBody   string
		detailBody  string
		wantCount   int
		wantCredits int
		wantNil     bool
	}{
		{
			name:       "detail count creates missing usage credits",
			usageBody:  `{}`,
			detailBody: `{"available_count":3,"credits":[{"expires_at":"2026-07-03T04:05:06Z"}]}`,
			wantCount:  3, wantCredits: 1,
		},
		{
			name:       "explicit detail zero overrides usage and records",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":4}}`,
			detailBody: `{"available_count":0,"credits":[{"expires_at":"2026-07-03T04:05:06Z"}]}`,
			wantCount:  0, wantCredits: 1,
		},
		{
			name:       "available records override usage when detail count is absent",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":7}}`,
			detailBody: `{"credits":[{"expires_at":"2026-07-03T04:05:06Z"},{"expiresAt":"2026-07-04T04:05:06Z"}]}`,
			wantCount:  2, wantCredits: 2,
		},
		{
			name:       "empty detail list overrides usage with zero",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":7}}`,
			detailBody: `{"credits":[]}`,
			wantCount:  0,
		},
		{
			name:       "fully filtered list overrides usage with zero",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":7}}`,
			detailBody: `{"credits":[{"reset_type":"codex_rate_limits","status":"redeemed","expires_at":"2026-07-03T04:05:06Z"},{"reset_type":"other","status":"available","expires_at":"2026-07-04T04:05:06Z"}]}`,
			wantCount:  0,
		},
		{
			name:       "available records without expiry still count",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":7}}`,
			detailBody: `{"credits":[{"status":"available"},{"status":"available","expires_at":"2026-07-04T04:05:06Z"}]}`,
			wantCount:  2, wantCredits: 1,
		},
		{
			name:        "shape without count or list preserves usage details",
			usageBody:   `{"rate_limit_reset_credits":{"available_count":5,"credits":[{"expires_at":"usage-expiry"}]}}`,
			detailBody:  `{}`,
			wantCount:   5,
			wantCredits: 1,
		},
		{
			name:        "valid detail count survives malformed authoritative list",
			usageBody:   `{"rate_limit_reset_credits":{"available_count":7,"credits":[{"expires_at":"usage-expiry"}]}}`,
			detailBody:  `{"available_count":2,"credits":"malformed"}`,
			wantCount:   2,
			wantCredits: 1,
		},
		{
			name:       "valid detail count creates quota despite malformed authoritative list",
			usageBody:  `{}`,
			detailBody: `{"available_count":2,"credits":"malformed"}`,
			wantCount:  2,
		},
		{
			name:       "negative detail count without list preserves usage",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":4}}`,
			detailBody: `{"available_count":-1}`,
			wantCount:  4,
		},
		{
			name:       "negative detail count falls back to available records",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":4}}`,
			detailBody: `{"available_count":-1,"credits":[{"status":"available","expires_at":"2026-07-04T04:05:06Z"}]}`,
			wantCount:  1, wantCredits: 1,
		},
		{
			name:       "empty object preserves missing usage credits",
			usageBody:  `{}`,
			detailBody: `{}`,
			wantNil:    true,
		},
		{
			name:       "null body preserves missing usage credits",
			usageBody:  `{}`,
			detailBody: `null`,
			wantNil:    true,
		},
		{
			name:       "empty body preserves missing usage credits",
			usageBody:  `{}`,
			detailBody: ``,
			wantNil:    true,
		},
		{
			name:       "null object record is not counted",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":7}}`,
			detailBody: `{"credits":[null]}`,
			wantCount:  0,
		},
		{
			name:       "null top level record is not counted",
			usageBody:  `{"rate_limit_reset_credits":{"available_count":7}}`,
			detailBody: `[null]`,
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				ID:       100,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Status:   StatusActive,
				Credentials: map[string]any{
					"chatgpt_account_id": "org-parent123",
				},
			}
			repo := &resetCreditsAccountRepo{accounts: map[int64]*Account{100: account}}
			tokenCache := &resetCreditsTokenCache{tokens: map[string]string{
				OpenAITokenCacheKey(account): "fake-token",
			}}
			tokenProvider := NewOpenAITokenProvider(repo, tokenCache, nil)

			var detailCalls int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				switch r.URL.Path {
				case "/backend-api/wham/usage":
					_, _ = w.Write([]byte(tt.usageBody))
				case "/backend-api/wham/rate-limit-reset-credits":
					detailCalls++
					_, _ = w.Write([]byte(tt.detailBody))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			svc := NewOpenAIQuotaService(repo, nil, tokenProvider, newResetCreditsRedirectingFactory(srv))
			usage, err := svc.QueryUsage(context.Background(), 100)
			require.NoError(t, err)
			require.NotNil(t, usage)
			require.Equal(t, 1, detailCalls)
			if tt.wantNil {
				require.Nil(t, usage.RateLimitResetCredits)
				return
			}
			require.NotNil(t, usage.RateLimitResetCredits)
			require.Equal(t, tt.wantCount, usage.RateLimitResetCredits.AvailableCount)
			require.Len(t, usage.RateLimitResetCredits.Credits, tt.wantCredits)
		})
	}
}
