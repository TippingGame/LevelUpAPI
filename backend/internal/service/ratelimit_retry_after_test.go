package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandle429RetryAfterSecondsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7301,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "17")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.NoError(t, repo.lastRateCtxErr)
	require.False(t, repo.lastRateReset.Before(before.Add(17*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(17*time.Second)))
	require.NotNil(t, account.RateLimitResetAt)
	require.Equal(t, repo.lastRateReset.Unix(), account.RateLimitResetAt.Unix())
}

func TestHandle429RetryAfterHTTPDateSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7302,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	resetAt := time.Now().Add(25 * time.Second).UTC().Truncate(time.Second)
	headers := http.Header{}
	headers.Set("Retry-After", resetAt.Format(http.TimeFormat))

	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"slow down"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.Equal(t, resetAt.Unix(), repo.lastRateReset.Unix())
}

func TestHandle429AnthropicAPIKeyHonorsRetryAfter(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7303,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "11")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"rate limited"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(11*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(11*time.Second)))
}

func TestHandle429RetryAfterTooLargeFallsBackToDefault(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7304,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "999999")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.False(t, repo.lastRateReset.Before(before.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
}

func TestHandle429RateLimitResetSecondsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7309,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("RateLimit-Reset", "19")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(19*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(19*time.Second)))
}

func TestHandle429XRateLimitResetUnixSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7310,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	resetAt := time.Now().Add(33 * time.Second).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"rate limited"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.Equal(t, resetAt.Unix(), repo.lastRateReset.Unix())
}

func TestHandle429RateLimitResetHTTPDateSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7311,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	resetAt := time.Now().Add(41 * time.Second).UTC().Truncate(time.Second)
	headers := http.Header{}
	headers.Set("X-Rate-Limit-Reset", resetAt.Format(http.TimeFormat))

	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.Equal(t, resetAt.Unix(), repo.lastRateReset.Unix())
}

func TestHandle429RateLimitResetTooLargeFallsBackToDefault(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7312,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("RateLimit-Reset", "999999")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.False(t, repo.lastRateReset.Before(before.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
}

func TestHandle529RetryAfterSetsOverloadCooldown(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7305,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "23")

	before := time.Now()
	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		529,
		headers,
		[]byte(`{"error":{"type":"overloaded_error","message":"overloaded"}}`),
	)
	after := time.Now()

	require.False(t, shouldDisable)
	require.Equal(t, 1, repo.overloadCalls)
	require.Equal(t, account.ID, repo.lastOverloadID)
	require.NoError(t, repo.lastOverloadErr)
	require.False(t, repo.lastOverloadEnd.Before(before.Add(23*time.Second)))
	require.False(t, repo.lastOverloadEnd.After(after.Add(23*time.Second)))
	require.NotNil(t, account.OverloadUntil)
	require.Equal(t, repo.lastOverloadEnd.Unix(), account.OverloadUntil.Unix())
}

func TestHandle5xxRetryAfterTempUnschedulable(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7306,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "31")

	before := time.Now()
	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		headers,
		[]byte(`{"error":{"type":"api_error","message":"upstream is warming up"}}`),
	)
	after := time.Now()

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "upstream_retry_after")
	require.False(t, repo.lastTempUntil.Before(before.Add(31*time.Second)))
	require.False(t, repo.lastTempUntil.After(after.Add(31*time.Second)))
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, repo.lastTempUntil.Unix(), account.TempUnschedulableUntil.Unix())
}

func TestHandle5xxRetryAfterSkipsReplayUnsafeTimeout(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7307,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "31")

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusGatewayTimeout,
		headers,
		[]byte(`{"error":{"message":"upstream timeout"}}`),
	)

	require.False(t, shouldDisable)
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandle5xxRetryAfterPreservesCustomErrorCodePriority(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7308,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusServiceUnavailable)},
		},
	}
	headers := http.Header{}
	headers.Set("Retry-After", "31")

	before := time.Now()
	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		headers,
		[]byte(`{"error":{"message":"custom outage"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "custom_error_code")
	require.NotContains(t, repo.lastTempReason, "upstream_retry_after")
	require.False(t, repo.lastTempUntil.Before(before.Add(customErrorCodeCooldown-2*time.Second)))
}
