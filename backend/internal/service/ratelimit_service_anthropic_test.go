package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type anthropicDefaultTempUnschedRepoStub struct {
	AccountRepository
	tempCalls      int
	lastTempUntil  time.Time
	lastTempReason string
}

func (r *anthropicDefaultTempUnschedRepoStub) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.tempCalls++
	r.lastTempUntil = until
	r.lastTempReason = reason
	return nil
}

func TestCalculateAnthropic429ResetTime_Only5hExceeded(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset5h := now.Add(2 * time.Hour).Unix()
	reset7d := now.Add(72 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(reset5h))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.32")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(reset7d))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset5h)

	if result.fiveHourReset == nil || !result.fiveHourReset.Equal(time.Unix(reset5h, 0)) {
		t.Errorf("expected fiveHourReset=%d, got %v", reset5h, result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_Only7dExceeded(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset5h := now.Add(2 * time.Hour).Unix()
	reset7d := now.Add(72 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.50")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(reset5h))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.05")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(reset7d))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset7d)

	// fiveHourReset should still be populated for session window calculation
	if result.fiveHourReset == nil || !result.fiveHourReset.Equal(time.Unix(reset5h, 0)) {
		t.Errorf("expected fiveHourReset=%d, got %v", reset5h, result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_BothExceeded(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset5h := now.Add(2 * time.Hour).Unix()
	reset7d := now.Add(72 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.10")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(reset5h))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(reset7d))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset7d)
}

func TestCalculateAnthropic429ResetTime_NoPerWindowHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", "1771549200")

	result := calculateAnthropic429ResetTime(headers)
	if result != nil {
		t.Errorf("expected nil result when no per-window headers, got resetAt=%v", result.resetAt)
	}
}

func TestCalculateAnthropic429ResetTime_NoHeaders(t *testing.T) {
	result := calculateAnthropic429ResetTime(http.Header{})
	if result != nil {
		t.Errorf("expected nil result for empty headers, got resetAt=%v", result.resetAt)
	}
}

func TestCalculateAnthropic429ResetTime_SurpassedThreshold(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset5h := now.Add(2 * time.Hour).Unix()
	reset7d := now.Add(72 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-surpassed-threshold", "true")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(reset5h))
	headers.Set("anthropic-ratelimit-unified-7d-surpassed-threshold", "false")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(reset7d))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset5h)
}

func TestCalculateAnthropic429ResetTime_UtilizationExactlyOne(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset5h := now.Add(2 * time.Hour).Unix()
	reset7d := now.Add(72 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(reset5h))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.5")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(reset7d))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset5h)
}

func TestCalculateAnthropic429ResetTime_NeitherExceededReturnsNil(t *testing.T) {
	now := time.Unix(1800000000, 0)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.95")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(now.Add(2*time.Hour).Unix()))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.80")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(now.Add(72*time.Hour).Unix()))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	require.Nil(t, result)
}

func TestCalculateAnthropic429ResetTime_Only5hResetHeader(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset5h := now.Add(2 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.05")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(reset5h))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset5h)
}

func TestCalculateAnthropic429ResetTime_Only7dResetHeader(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset7d := now.Add(72 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.03")
	headers.Set("anthropic-ratelimit-unified-7d-reset", formatUnix(reset7d))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset7d)

	if result.fiveHourReset != nil {
		t.Errorf("expected fiveHourReset=nil when no 5h headers, got %v", result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_RejectedStatusCountsAs5hExhausted(t *testing.T) {
	now := time.Unix(1800000000, 0)
	reset5h := now.Add(2 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.75")
	headers.Set("anthropic-ratelimit-unified-5h-reset", formatUnix(reset5h))

	result := calculateAnthropic429ResetTimeAt(headers, now)
	assertAnthropicResult(t, result, reset5h)
}

func TestCalculateAnthropic429ResetTime_InvalidResetReturnsNil(t *testing.T) {
	now := time.Unix(1800000000, 0)
	tests := []struct {
		name  string
		reset string
	}{
		{name: "past reset", reset: formatUnix(now.Add(-time.Minute).Unix())},
		{name: "too far reset", reset: formatUnix(now.Add(9 * 24 * time.Hour).Unix())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.03")
			headers.Set("anthropic-ratelimit-unified-7d-reset", tt.reset)

			result := calculateAnthropic429ResetTimeAt(headers, now)
			require.Nil(t, result)
		})
	}
}

func TestIsAnthropicWindowExceeded(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		window   string
		expected bool
	}{
		{
			name:     "utilization above 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "1.02"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "utilization exactly 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "1.0"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "utilization below 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "0.99"),
			window:   "5h",
			expected: false,
		},
		{
			name:     "surpassed-threshold true",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "true"),
			window:   "7d",
			expected: true,
		},
		{
			name:     "surpassed-threshold True (case insensitive)",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "True"),
			window:   "7d",
			expected: true,
		},
		{
			name:     "surpassed-threshold false",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "false"),
			window:   "7d",
			expected: false,
		},
		{
			name:     "no headers",
			headers:  http.Header{},
			window:   "5h",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAnthropicWindowExceeded(tc.headers, tc.window)
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

// assertAnthropicResult is a test helper that verifies the result is non-nil and
// has the expected resetAt unix timestamp.
func assertAnthropicResult(t *testing.T, result *anthropic429Result, wantUnix int64) {
	t.Helper()
	if result == nil {
		t.Fatal("expected non-nil result")
		return // unreachable, but satisfies staticcheck SA5011
	}
	want := time.Unix(wantUnix, 0)
	if !result.resetAt.Equal(want) {
		t.Errorf("expected resetAt=%v, got %v", want, result.resetAt)
	}
}

func makeHeader(key, value string) http.Header {
	h := http.Header{}
	h.Set(key, value)
	return h
}

func formatUnix(ts int64) string {
	return strconv.FormatInt(ts, 10)
}

func TestHandleUpstreamErrorAnthropicOAuthDefault503OverloadTempUnschedulable(t *testing.T) {
	credentials, extra := applyAnthropicOAuthPoolProtectionDefaults(PlatformAnthropic, AccountTypeOAuth, nil, nil)
	repo := &anthropicDefaultTempUnschedRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          42,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: credentials,
		Extra:       extra,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		http.Header{},
		[]byte(`{"error":{"type":"overloaded_error","message":"temporarily unavailable"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.WithinDuration(t, time.Now().Add(time.Duration(anthropicOAuthDefaultCooldownMinutes)*time.Minute), repo.lastTempUntil, 2*time.Second)
	require.Contains(t, repo.lastTempReason, "temporarily unavailable")
}

func TestHandleUpstreamErrorAnthropicOAuthDefault503WithoutKeywordIsNotTempUnschedulable(t *testing.T) {
	credentials, extra := applyAnthropicOAuthPoolProtectionDefaults(PlatformAnthropic, AccountTypeOAuth, nil, nil)
	repo := &anthropicDefaultTempUnschedRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          42,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Credentials: credentials,
		Extra:       extra,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		http.Header{},
		[]byte(`{"error":{"type":"api_error","message":"internal server error"}}`),
	)

	require.False(t, shouldDisable)
	require.Zero(t, repo.tempCalls)
}
