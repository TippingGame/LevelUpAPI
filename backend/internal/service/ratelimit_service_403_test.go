//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRateLimitService_HandleUpstreamError_OpenAI403FirstHitTempUnschedulable(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{1}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       301,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"temporary edge rejection"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "temporary edge rejection")
	require.Contains(t, repo.lastTempReason, "(1/3)")
}

func TestRateLimitService_HandleUpstreamError_OpenAI403ThresholdLongCooldown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	counter := &openAI403CounterCacheStub{counts: []int64{3}}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	service.SetOpenAI403CounterCache(counter)
	account := &Account{
		ID:       302,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"workspace forbidden by policy"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.WithinDuration(t, time.Now().Add(2*time.Hour), repo.lastTempUntil, 3*time.Second)

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, "openai_403_long_cooldown", state.MatchedKeyword)
	require.Equal(t, 3, state.ConsecutiveCount)
	require.Contains(t, state.ErrorMessage, "workspace forbidden by policy")
	require.Contains(t, state.ErrorMessage, "(3/3)")
}

func TestRateLimitService_HandleUpstreamError_OpenAI403RequestPolicyDoesNotTouchAccount(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "cyber policy",
			body: []byte(`{"error":{"code":"cyber_policy","message":"This request has been blocked"}}`),
		},
		{
			name: "safety error",
			body: []byte(`{"error":{"type":"safety_error","message":"This request has been flagged for potentially high-risk cyber activity."}}`),
		},
		{
			name: "content policy violation",
			body: []byte(`{"error":{"code":"content_policy_violation","type":"invalid_request_error","message":"This request violates the content policy"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			counter := &openAI403CounterCacheStub{counts: []int64{3}}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			service.SetOpenAI403CounterCache(counter)
			account := &Account{
				ID:       3030,
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Status:   StatusActive,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(http.StatusForbidden)},
				},
			}

			shouldDisable := service.HandleUpstreamError(
				context.Background(),
				account,
				http.StatusForbidden,
				http.Header{},
				tt.body,
			)

			require.False(t, shouldDisable)
			require.Equal(t, 0, repo.setErrorCalls)
			require.Equal(t, 0, repo.tempCalls)
			require.Equal(t, StatusActive, account.Status)
		})
	}
}

func TestRateLimitService_HandleUpstreamError_AnthropicOAuth403FirstHitTempUnschedulable(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       305,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
	}

	before := time.Now()
	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"temporary access forbidden"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.NotNil(t, account.TempUnschedulableUntil)
	require.WithinDuration(t, before.Add(time.Duration(anthropicOAuthDefaultCooldownMinutes)*time.Minute), repo.lastTempUntil, 3*time.Second)

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, http.StatusForbidden, state.StatusCode)
	require.Equal(t, "anthropic_oauth_403", state.MatchedKeyword)
	require.Contains(t, state.ErrorMessage, "temporary access forbidden")
}

func TestRateLimitService_HandleUpstreamError_AnthropicRequestPolicyDoesNotTouchAccount(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "safety error",
			body: []byte(`{"type":"error","error":{"type":"safety_error","message":"Your request violates Anthropic's Usage Policy."}}`),
		},
		{
			name: "content policy",
			body: []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"This request violates the content policy."}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{
				ID:       306,
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
				Status:   StatusActive,
			}

			shouldDisable := service.HandleUpstreamError(
				context.Background(),
				account,
				http.StatusForbidden,
				http.Header{},
				tt.body,
			)

			require.False(t, shouldDisable)
			require.Equal(t, 0, repo.setErrorCalls)
			require.Equal(t, 0, repo.tempCalls)
			require.Equal(t, StatusActive, account.Status)
		})
	}
}

func TestRateLimitService_HandleUpstreamError_GeminiRequestPolicyDoesNotTouchAccount(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{
			name: "safety reason",
			body: []byte(`{"error":{"code":403,"message":"The prompt was blocked due to safety filters.","status":"FAILED_PRECONDITION","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"SAFETY"}]}}`),
		},
		{
			name: "wrapped prohibited content",
			body: []byte(`{"response":{"error":{"code":403,"message":"The response was blocked due to prohibited content.","status":"FAILED_PRECONDITION","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"PROHIBITED_CONTENT"}]}}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{
				ID:       307,
				Platform: PlatformGemini,
				Type:     AccountTypeAPIKey,
				Status:   StatusActive,
				Credentials: map[string]any{
					"custom_error_codes_enabled": true,
					"custom_error_codes":         []any{float64(http.StatusForbidden)},
				},
			}

			shouldDisable := service.HandleUpstreamError(
				context.Background(),
				account,
				http.StatusForbidden,
				http.Header{},
				tt.body,
			)

			require.False(t, shouldDisable)
			require.Equal(t, 0, repo.setErrorCalls)
			require.Equal(t, 0, repo.tempCalls)
			require.Equal(t, StatusActive, account.Status)
		})
	}
}

func TestRateLimitService_HandleUpstreamError_AnthropicOAuthSecond403KeepsTempUnschedulable(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	previousReason, err := json.Marshal(&TempUnschedState{
		TriggeredAtUnix:  time.Now().Add(-time.Minute).Unix(),
		StatusCode:       http.StatusForbidden,
		MatchedKeyword:   "anthropic_oauth_403",
		ConsecutiveCount: 1,
	})
	require.NoError(t, err)
	account := &Account{
		ID:                      306,
		Platform:                PlatformAnthropic,
		Type:                    AccountTypeSetupToken,
		TempUnschedulableReason: string(previousReason),
		TempUnschedulableUntil:  ptrTime(time.Now().Add(-time.Minute)),
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"still forbidden"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "still forbidden")

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, "anthropic_oauth_403", state.MatchedKeyword)
	require.Equal(t, 2, state.ConsecutiveCount)
	require.Contains(t, state.ErrorMessage, "(2/3)")
}

func TestRateLimitService_HandleUpstreamError_AnthropicOAuthThird403LongCooldown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	previousReason, err := json.Marshal(&TempUnschedState{
		TriggeredAtUnix:  time.Now().Add(-time.Minute).Unix(),
		StatusCode:       http.StatusForbidden,
		MatchedKeyword:   "anthropic_oauth_403",
		ConsecutiveCount: 2,
	})
	require.NoError(t, err)
	account := &Account{
		ID:                      3061,
		Platform:                PlatformAnthropic,
		Type:                    AccountTypeSetupToken,
		TempUnschedulableReason: string(previousReason),
		TempUnschedulableUntil:  ptrTime(time.Now().Add(-time.Minute)),
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"still forbidden"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.WithinDuration(t, time.Now().Add(2*time.Hour), repo.lastTempUntil, 3*time.Second)

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, "anthropic_oauth_403_long_cooldown", state.MatchedKeyword)
	require.Equal(t, 3, state.ConsecutiveCount)
	require.Contains(t, state.ErrorMessage, "(3/3)")
	require.Contains(t, state.ErrorMessage, "still forbidden")
}

func TestRateLimitService_HandleUpstreamError_AnthropicOAuthFourth403MaxCooldown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	previousReason, err := json.Marshal(&TempUnschedState{
		TriggeredAtUnix:  time.Now().Add(-time.Minute).Unix(),
		StatusCode:       http.StatusForbidden,
		MatchedKeyword:   "anthropic_oauth_403_long_cooldown",
		ConsecutiveCount: 3,
	})
	require.NoError(t, err)
	account := &Account{
		ID:                      3063,
		Platform:                PlatformAnthropic,
		Type:                    AccountTypeOAuth,
		TempUnschedulableReason: string(previousReason),
		TempUnschedulableUntil:  ptrTime(time.Now().Add(-time.Minute)),
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"still forbidden"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.WithinDuration(t, time.Now().Add(12*time.Hour), repo.lastTempUntil, 3*time.Second)

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, "anthropic_oauth_403_long_cooldown", state.MatchedKeyword)
	require.Equal(t, 4, state.ConsecutiveCount)
}

func TestRateLimitService_HandleUpstreamError_AnthropicOAuthStale403ReasonResetsCount(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	previousReason, err := json.Marshal(&TempUnschedState{
		TriggeredAtUnix:  time.Now().Add(-4 * time.Hour).Unix(),
		StatusCode:       http.StatusForbidden,
		MatchedKeyword:   "anthropic_oauth_403",
		ConsecutiveCount: 2,
	})
	require.NoError(t, err)
	account := &Account{
		ID:                      3062,
		Platform:                PlatformAnthropic,
		Type:                    AccountTypeOAuth,
		TempUnschedulableReason: string(previousReason),
		TempUnschedulableUntil:  ptrTime(time.Now().Add(-4 * time.Hour)),
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"temporary edge rejection"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, 1, state.ConsecutiveCount)
	require.Contains(t, state.ErrorMessage, "(1/3)")
}

func TestRateLimitService_HandleUpstreamError_Generic403TempUnschedulable(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		typ      string
	}{
		{name: "gemini apikey", platform: PlatformGemini, typ: AccountTypeAPIKey},
		{name: "anthropic apikey", platform: PlatformAnthropic, typ: AccountTypeAPIKey},
		{name: "anthropic bedrock", platform: PlatformAnthropic, typ: AccountTypeBedrock},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{
				ID:       307,
				Platform: tt.platform,
				Type:     tt.typ,
			}

			before := time.Now()
			shouldDisable := service.HandleUpstreamError(
				context.Background(),
				account,
				http.StatusForbidden,
				http.Header{},
				[]byte(`{"error":{"message":"Permission denied"}}`),
			)

			require.True(t, shouldDisable)
			require.Equal(t, 0, repo.setErrorCalls)
			require.Equal(t, 1, repo.tempCalls)
			require.WithinDuration(t, before.Add(time.Duration(generic403CooldownMinutesDefault)*time.Minute), repo.lastTempUntil, 3*time.Second)

			var state TempUnschedState
			require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
			require.Equal(t, "generic_403", state.MatchedKeyword)
			require.Equal(t, http.StatusForbidden, state.StatusCode)
			require.Equal(t, 1, state.ConsecutiveCount)
			require.Contains(t, state.ErrorMessage, "Permission denied")
		})
	}
}

func TestRateLimitService_HandleUpstreamError_Generic403EscalatesCooldown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	previousReason, err := json.Marshal(&TempUnschedState{
		TriggeredAtUnix:  time.Now().Add(-time.Minute).Unix(),
		StatusCode:       http.StatusForbidden,
		MatchedKeyword:   "generic_403",
		ConsecutiveCount: 2,
	})
	require.NoError(t, err)
	account := &Account{
		ID:                      308,
		Platform:                PlatformGemini,
		Type:                    AccountTypeAPIKey,
		TempUnschedulableReason: string(previousReason),
		TempUnschedulableUntil:  ptrTime(time.Now().Add(-time.Minute)),
	}

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"Permission denied"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.WithinDuration(t, time.Now().Add(2*time.Hour), repo.lastTempUntil, 3*time.Second)

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, "generic_403_long_cooldown", state.MatchedKeyword)
	require.Equal(t, 3, state.ConsecutiveCount)
}

func TestRateLimitService_HandleUpstreamError_Cloudflare403TempUnschedulable(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       303,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
	}
	headers := http.Header{}
	headers.Set("content-type", "text/html; charset=UTF-8")
	headers.Set("cf-ray", "abc123-SJC")
	body := []byte(`<!doctype html><html><head><title>Access denied</title></head><body>Cloudflare restrict access</body></html>`)

	shouldDisable := service.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		headers,
		body,
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "Cloudflare challenge (403)")
	require.Contains(t, repo.lastTempReason, "cf-ray: abc123-SJC")

	var state TempUnschedState
	require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
	require.Equal(t, "cloudflare_challenge", state.MatchedKeyword)
	require.Equal(t, 1, state.ConsecutiveCount)
	require.WithinDuration(t, time.Now().Add(30*time.Second), repo.lastTempUntil, 3*time.Second)
	require.Contains(t, state.ErrorMessage, "cf-ray: abc123-SJC")
}

func TestRateLimitService_HandleUpstreamError_Cloudflare403EscalatesCooldown(t *testing.T) {
	repo := &rateLimitAccountRepoStub{}
	service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       304,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
	}
	headers := http.Header{"cf-mitigated": []string{"challenge"}}
	body := []byte(`<!doctype html><html><body>Just a moment...</body></html>`)

	expected := []time.Duration{
		30 * time.Second,
		time.Minute,
		2 * time.Minute,
		5 * time.Minute,
		5 * time.Minute,
	}

	for i, wantCooldown := range expected {
		before := time.Now()
		shouldDisable := service.HandleUpstreamError(
			context.Background(),
			account,
			http.StatusForbidden,
			headers,
			body,
		)

		require.True(t, shouldDisable)
		require.Equal(t, 0, repo.setErrorCalls)
		require.Equal(t, i+1, repo.tempCalls)
		require.WithinDuration(t, before.Add(wantCooldown), repo.lastTempUntil, 3*time.Second)

		var state TempUnschedState
		require.NoError(t, json.Unmarshal([]byte(repo.lastTempReason), &state))
		require.Equal(t, "cloudflare_challenge", state.MatchedKeyword)
		require.Equal(t, i+1, state.ConsecutiveCount)
	}
}
