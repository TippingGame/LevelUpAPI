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

func TestHandle429RetryAfterFractionalSecondsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7318,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "1.5")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(1500*time.Millisecond)))
	require.False(t, repo.lastRateReset.After(after.Add(1500*time.Millisecond)))
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

func TestHandle429RetryAfterNonFiniteFallsBackToDefault(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7319,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "+Inf")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.False(t, repo.lastRateReset.Before(before.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)))
}

func TestHandle429RetryAfterMsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7320,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After-Ms", "1500")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(1500*time.Millisecond)))
	require.False(t, repo.lastRateReset.After(after.Add(1500*time.Millisecond)))
}

func TestHandle429RetryAfterPrefersStandardHeaderOverMs(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7321,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "2")
	headers.Set("Retry-After-Ms", "90000")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.False(t, repo.lastRateReset.Before(before.Add(2*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(2*time.Second)))
}

func TestHandle429InvalidRetryAfterFallsBackToMsHeader(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7322,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "+Inf")
	headers.Set("X-Ms-Retry-After-Ms", "2500")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.False(t, repo.lastRateReset.Before(before.Add(2500*time.Millisecond)))
	require.False(t, repo.lastRateReset.After(after.Add(2500*time.Millisecond)))
}

func TestHandle429BodyRetryAfterSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7328,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}

	before := time.Now()
	svc.handle429(context.Background(), account, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","retry_after":"2s","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(2*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(2*time.Second)))
}

func TestHandle429BodyRetryAfterMsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7329,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}

	before := time.Now()
	svc.handle429(context.Background(), account, http.Header{}, []byte(`{"error":{"type":"rate_limit_error","retry_after_ms":2500,"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(2500*time.Millisecond)))
	require.False(t, repo.lastRateReset.After(after.Add(2500*time.Millisecond)))
}

func TestHandle429HeadersPreferOverBodyRetryAfter(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7330,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "2")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","retry_after_ms":90000,"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(2*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(2*time.Second)))
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

func TestHandle429RateLimitResetDurationSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7315,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-RateLimit-Reset-After", "1m30s")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(90*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(90*time.Second)))
}

func TestHandle429RateLimitResetFractionalSecondsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7316,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-Rate-Limit-Reset-After", "1.5")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(1500*time.Millisecond)))
	require.False(t, repo.lastRateReset.After(after.Add(1500*time.Millisecond)))
}

func TestHandle429RateLimitResetMsSetsRateLimit(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7323,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-RateLimit-Reset-After-Ms", "1750")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(1750*time.Millisecond)))
	require.False(t, repo.lastRateReset.After(after.Add(1750*time.Millisecond)))
}

func TestHandle429RateLimitResetPrefersStandardHeaderOverMs(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7324,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("RateLimit-Reset", "3")
	headers.Set("X-RateLimit-Reset-After-Ms", "90000")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.False(t, repo.lastRateReset.Before(before.Add(3*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(3*time.Second)))
}

func TestHandle429BucketedRateLimitResetUsesExhaustedBucket(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7325,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-RateLimit-Remaining-Requests", "0")
	headers.Set("X-RateLimit-Reset-Requests", "2s")
	headers.Set("X-RateLimit-Remaining-Tokens", "100")
	headers.Set("X-RateLimit-Reset-Tokens", "45s")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(2*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(2*time.Second)))
}

func TestHandle429BucketedRateLimitResetUsesLatestExhaustedBucket(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7326,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-RateLimit-Remaining-Requests", "0")
	headers.Set("X-RateLimit-Reset-Requests", "2s")
	headers.Set("X-RateLimit-Remaining-Tokens", "0")
	headers.Set("X-RateLimit-Reset-Tokens", "6s")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(6*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(6*time.Second)))
}

func TestHandle429BucketedRateLimitResetFallbackWithoutRemainingUsesLatest(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7327,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-RateLimit-Reset-Requests", "2s")
	headers.Set("X-RateLimit-Reset-Tokens", "7s")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`))
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, account.ID, repo.lastRateLimitID)
	require.False(t, repo.lastRateReset.Before(before.Add(7*time.Second)))
	require.False(t, repo.lastRateReset.After(after.Add(7*time.Second)))
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

func TestHandle429RateLimitResetNonFiniteFallsBackToDefault(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7317,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-RateLimit-Reset-After", "NaN")

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

func TestHandle529GenericRateLimitResetSetsOverloadCooldown(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7313,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("RateLimit-Reset", "29")

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
	require.False(t, repo.lastOverloadEnd.Before(before.Add(29*time.Second)))
	require.False(t, repo.lastOverloadEnd.After(after.Add(29*time.Second)))
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

func TestHandle5xxGenericRateLimitResetTempUnschedulable(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7314,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-Rate-Limit-Reset-After", "37s")

	before := time.Now()
	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		headers,
		[]byte(`{"error":{"type":"api_error","message":"upstream is cooling down"}}`),
	)
	after := time.Now()

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "upstream_retry_after")
	require.False(t, repo.lastTempUntil.Before(before.Add(37*time.Second)))
	require.False(t, repo.lastTempUntil.After(after.Add(37*time.Second)))
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
