package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandleUpstreamErrorRelayNoAvailableAccountsTempUnschedulable(t *testing.T) {
	statuses := []int{
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		529,
	}

	for _, status := range statuses {
		t.Run("status_"+strconv.Itoa(status), func(t *testing.T) {
			repo := &permanentKeywordAccountRepoStub{}
			svc := &RateLimitService{accountRepo: repo}
			account := &Account{
				ID:          int64(7201 + status),
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
			}

			shouldDisable := svc.HandleUpstreamError(
				context.Background(),
				account,
				status,
				http.Header{},
				[]byte(`{"type":"error","error":{"type":"api_error","message":"No available accounts: no available accounts"}}`),
			)

			require.True(t, shouldDisable)
			require.Zero(t, repo.setErrorCalls)
			require.Equal(t, 1, repo.tempCalls)
			require.WithinDuration(t, time.Now().Add(upstreamRelayPoolUnavailableCooldown), repo.lastTempUntil, 2*time.Second)
			require.NoError(t, repo.lastTempCtxErr)
			require.NotNil(t, account.TempUnschedulableUntil)

			var state TempUnschedState
			require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
			require.Equal(t, status, state.StatusCode)
			require.Equal(t, "upstream_relay_pool_unavailable", state.MatchedKeyword)
			require.Contains(t, state.ErrorMessage, "No available accounts")
		})
	}
}

func TestHandleUpstreamErrorRelayNoAvailableRoutesTempUnschedulable(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7202,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		http.Header{},
		[]byte(`{"error":{"message":"No available API key group routes","type":"api_error"},"type":"error"}`),
	)

	require.True(t, shouldDisable)
	require.Zero(t, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "upstream_relay_pool_unavailable")
}

func TestHandleUpstreamErrorRelayNoAvailableAccountsHonorsRetryAfter(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7206,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "37")

	before := time.Now()
	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		headers,
		[]byte(`{"error":{"message":"No available accounts: no available accounts"}}`),
	)
	after := time.Now()

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.False(t, repo.lastTempUntil.Before(before.Add(37*time.Second)))
	require.False(t, repo.lastTempUntil.After(after.Add(37*time.Second)))
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, repo.lastTempUntil.Unix(), account.TempUnschedulableUntil.Unix())
}

func TestHandleUpstreamErrorRelayNoAvailableAccountsHonorsGenericRateLimitReset(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7207,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("X-RateLimit-Reset", "43")

	before := time.Now()
	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusTooManyRequests,
		headers,
		[]byte(`{"error":{"message":"No available accounts: no available accounts"}}`),
	)
	after := time.Now()

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.False(t, repo.lastTempUntil.Before(before.Add(43*time.Second)))
	require.False(t, repo.lastTempUntil.After(after.Add(43*time.Second)))
	require.NotNil(t, account.TempUnschedulableUntil)
	require.Equal(t, repo.lastTempUntil.Unix(), account.TempUnschedulableUntil.Unix())
}

func TestHandleUpstreamErrorRelayNoAvailableAccountsPrefersRetryAfterOverGenericReset(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7208,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}
	headers := http.Header{}
	headers.Set("Retry-After", "17")
	headers.Set("RateLimit-Reset", "61")

	before := time.Now()
	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusTooManyRequests,
		headers,
		[]byte(`{"error":{"message":"No available accounts: no available accounts"}}`),
	)
	after := time.Now()

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.tempCalls)
	require.False(t, repo.lastTempUntil.Before(before.Add(17*time.Second)))
	require.False(t, repo.lastTempUntil.After(after.Add(17*time.Second)))
}

func TestHandleUpstreamErrorRelayClaudeCodeOnlyIsNotPoolUnavailable(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7203,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		http.Header{},
		[]byte(`{"type":"error","error":{"type":"api_error","message":"No available accounts: this group only allows Claude Code clients"}}`),
	)

	require.False(t, shouldDisable)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandleUpstreamErrorRelayNoAvailableAccountsSkipsReplayUnsafeTimeout(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7205,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusGatewayTimeout,
		http.Header{},
		[]byte(`{"error":{"message":"No available accounts: no available accounts"}}`),
	)

	require.False(t, shouldDisable)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
}

func TestHandleUpstreamErrorRelayNoAvailableAccountsRespectsCustomStatusPolicy(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7204,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusUnauthorized)},
		},
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		http.Header{},
		[]byte(`{"error":{"message":"No available accounts: no available accounts"}}`),
	)

	require.False(t, shouldDisable)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.tempCalls)
}
