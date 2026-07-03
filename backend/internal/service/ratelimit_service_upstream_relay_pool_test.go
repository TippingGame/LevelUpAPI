package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandleUpstreamErrorRelayNoAvailableAccountsTempUnschedulable(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          7201,
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
	require.Equal(t, http.StatusServiceUnavailable, state.StatusCode)
	require.Equal(t, "upstream_relay_pool_unavailable", state.MatchedKeyword)
	require.Contains(t, state.ErrorMessage, "No available accounts")
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
