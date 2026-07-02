package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type permanentKeywordAccountRepoStub struct {
	AccountRepository
	setErrorCalls  int
	lastErrorMsg   string
	tempCalls      int
	lastTempReason string
}

func (r *permanentKeywordAccountRepoStub) SetError(_ context.Context, _ int64, errorMsg string) error {
	r.setErrorCalls++
	r.lastErrorMsg = errorMsg
	return nil
}

func (r *permanentKeywordAccountRepoStub) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.tempCalls++
	r.lastTempReason = reason
	return nil
}

type permanentKeywordOpenAI403CounterStub struct{}

func (s permanentKeywordOpenAI403CounterStub) IncrementOpenAI403Count(context.Context, int64, int) (int64, error) {
	return 1, nil
}

func (s permanentKeywordOpenAI403CounterStub) ResetOpenAI403Count(context.Context, int64) error {
	return nil
}

func TestRateLimitServiceHandleUpstreamErrorAPIKeyPermanentKeywordsDisable(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		accountTyp string
		statusCode int
		body       []byte
		want       string
	}{
		{
			name:       "openai permission denied",
			platform:   PlatformOpenAI,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"Permission denied","type":"invalid_request_error"}}`),
			want:       "Permission denied",
		},
		{
			name:       "openai quota exhausted",
			platform:   PlatformOpenAI,
			statusCode: http.StatusTooManyRequests,
			body:       []byte(`{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","code":"insufficient_quota"}}`),
			want:       "You exceeded your current quota",
		},
		{
			name:       "openai account suspended",
			platform:   PlatformOpenAI,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"Your account has been suspended. Please contact support."}}`),
			want:       "account has been suspended",
		},
		{
			name:       "openai api key disabled",
			platform:   PlatformOpenAI,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"This API key has been disabled"}}`),
			want:       "API key has been disabled",
		},
		{
			name:       "openai api key revoked",
			platform:   PlatformOpenAI,
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"error":{"message":"The API key has been revoked."}}`),
			want:       "API key has been revoked",
		},
		{
			name:       "anthropic api key expired",
			platform:   PlatformAnthropic,
			statusCode: http.StatusUnauthorized,
			body:       []byte(`{"error":{"message":"API key expired"}}`),
			want:       "API key expired",
		},
		{
			name:       "openai billing hard limit",
			platform:   PlatformOpenAI,
			statusCode: http.StatusTooManyRequests,
			body:       []byte(`{"error":{"message":"Billing hard limit has been reached"}}`),
			want:       "Billing hard limit",
		},
		{
			name:       "gemini billing not enabled",
			platform:   PlatformGemini,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"Cloud Billing is not enabled for this project."}}`),
			want:       "Billing is not enabled",
		},
		{
			name:       "gemini project disabled",
			platform:   PlatformGemini,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"The project has been disabled."}}`),
			want:       "project has been disabled",
		},
		{
			name:       "service disabled",
			platform:   PlatformAnthropic,
			accountTyp: AccountTypeBedrock,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"message":"The service has been disabled for this account."}`),
			want:       "service has been disabled",
		},
		{
			name:       "anthropic credit balance",
			platform:   PlatformAnthropic,
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error":{"message":"Your credit balance is too low"}}`),
			want:       "Your credit balance is too low",
		},
		{
			name:       "security token invalid",
			platform:   PlatformGemini,
			statusCode: http.StatusForbidden,
			body:       []byte(`{"error":{"message":"The security token included in the request is invalid"}}`),
			want:       "security token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &permanentKeywordAccountRepoStub{}
			svc := &RateLimitService{accountRepo: repo}
			account := &Account{
				ID:       207,
				Platform: tt.platform,
				Type:     AccountTypeAPIKey,
			}
			if tt.accountTyp != "" {
				account.Type = tt.accountTyp
			}

			shouldDisable := svc.HandleUpstreamError(
				context.Background(),
				account,
				tt.statusCode,
				http.Header{},
				tt.body,
			)

			require.True(t, shouldDisable)
			require.Equal(t, 1, repo.setErrorCalls)
			require.Equal(t, 0, repo.tempCalls)
			require.Contains(t, repo.lastErrorMsg, "Permanent account error")
			require.Contains(t, repo.lastErrorMsg, tt.want)
		})
	}
}

func TestRateLimitServiceHandleUpstreamErrorPermanentErrorWritesRuntimeEvictionCache(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	cache := &runtimeTempUnschedCacheStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	account := &Account{
		ID:          55,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusUnauthorized,
		http.Header{},
		[]byte(`{"error":{"message":"The API key has been revoked."}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	require.Contains(t, account.ErrorMessage, "API key has been revoked")
	require.NotNil(t, cache.states[55])
	require.Equal(t, "account_error", cache.states[55].MatchedKeyword)
	require.True(t, cache.states[55].UntilUnix > time.Now().Add(time.Hour).Unix())
}

func TestRateLimitServiceHandleUpstreamErrorCustomErrorWritesRuntimeEvictionCache(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	cache := &runtimeTempUnschedCacheStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	account := &Account{
		ID:          56,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusServiceUnavailable)},
		},
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusServiceUnavailable,
		http.Header{},
		[]byte(`{"error":{"message":"temporary upstream account failure"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	require.Contains(t, account.ErrorMessage, "Custom error code")
	require.NotNil(t, cache.states[56])
	require.Equal(t, "account_error", cache.states[56].MatchedKeyword)
	require.True(t, cache.states[56].UntilUnix > time.Now().Add(time.Hour).Unix())
}

func TestRateLimitServiceTriggerStreamTimeoutErrorWritesRuntimeEvictionCache(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	cache := &runtimeTempUnschedCacheStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	account := &Account{
		ID:          57,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}

	disabled := svc.triggerStreamTimeoutError(context.Background(), account, "gpt-5")

	require.True(t, disabled)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	require.Contains(t, account.ErrorMessage, "Stream data interval timeout")
	require.NotNil(t, cache.states[57])
	require.Equal(t, "account_error", cache.states[57].MatchedKeyword)
	require.True(t, cache.states[57].UntilUnix > time.Now().Add(time.Hour).Unix())
}

func TestRateLimitServiceEvictAccountErrorFromRuntimeCache(t *testing.T) {
	cache := &runtimeTempUnschedCacheStub{}
	svc := NewRateLimitService(nil, nil, nil, nil, cache)

	svc.EvictAccountErrorFromRuntimeCache(context.Background(), 58, "connectivity failed", "account_share_connectivity_test")

	require.NotNil(t, cache.states[58])
	require.Equal(t, "account_error", cache.states[58].MatchedKeyword)
	require.Contains(t, cache.states[58].ErrorMessage, "connectivity failed")
	require.True(t, cache.states[58].UntilUnix > time.Now().Add(time.Hour).Unix())
}

func TestRateLimitServiceHandleUpstreamErrorOpenAIOAuthPermanentKeywordStillUses403Cooldown(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	svc.SetOpenAI403CounterCache(permanentKeywordOpenAI403CounterStub{})
	account := &Account{
		ID:       208,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"Permission denied","type":"invalid_request_error"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, 1, repo.tempCalls)
	require.Contains(t, repo.lastTempReason, "Permission denied")
	require.Contains(t, repo.lastTempReason, "(1/3)")
}

func TestRateLimitServiceHandleUpstreamErrorPermanentKeywordBeatsTempRule(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:       209,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       403,
					"keywords":         []any{"account"},
					"duration_minutes": 10,
				},
			},
		},
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"Your account has been suspended. Please contact support."}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Contains(t, repo.lastErrorMsg, "account has been suspended")
}

func TestRateLimitServiceHandleUpstreamErrorPermanentKeywordBypassesCustomErrorCodeFilter(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:       210,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"custom_error_codes_enabled": true,
			"custom_error_codes":         []any{float64(http.StatusTooManyRequests)},
		},
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusForbidden,
		http.Header{},
		[]byte(`{"error":{"message":"This API key has been disabled"}}`),
	)

	require.True(t, shouldDisable)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, 0, repo.tempCalls)
	require.Contains(t, repo.lastErrorMsg, "API key has been disabled")
}

func TestRateLimitServiceHandlePermanentAccountErrorIsIdempotentInRequest(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:       211,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Status:   StatusActive,
	}
	body := []byte(`{"error":{"message":"This API key has been disabled"}}`)

	require.True(t, svc.HandlePermanentAccountError(context.Background(), account, http.StatusForbidden, body))
	require.True(t, svc.HandlePermanentAccountError(context.Background(), account, http.StatusForbidden, body))

	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	require.Contains(t, account.ErrorMessage, "API key has been disabled")
}

func TestRateLimitServiceHandlePermanentAccountErrorSkipsOpenAIRequestPolicy(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          212,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	handled := svc.HandlePermanentAccountError(
		context.Background(),
		account,
		http.StatusForbidden,
		[]byte(`{"error":{"type":"safety_error","message":"This request has been flagged for potentially high-risk cyber activity."}}`),
	)

	require.False(t, handled)
	require.Equal(t, 0, repo.setErrorCalls)
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
}

func TestRateLimitServiceHandlePermanentAccountErrorAccountFailureBeatsOpenAIRequestPolicyCode(t *testing.T) {
	repo := &permanentKeywordAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	account := &Account{
		ID:          213,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
	}

	handled := svc.HandlePermanentAccountError(
		context.Background(),
		account,
		http.StatusForbidden,
		[]byte(`{"error":{"code":"content_policy_violation","message":"This account has been disabled after policy review."}}`),
	)

	require.True(t, handled)
	require.Equal(t, 1, repo.setErrorCalls)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	require.Contains(t, repo.lastErrorMsg, "account has been disabled")
}

func TestRateLimitServiceHandleUpstreamErrorNilAccountDoesNotPanic(t *testing.T) {
	svc := &RateLimitService{}

	require.NotPanics(t, func() {
		shouldDisable := svc.HandleUpstreamError(
			context.Background(),
			nil,
			http.StatusInternalServerError,
			http.Header{},
			[]byte(`{"error":{"message":"upstream failed"}}`),
		)
		require.False(t, shouldDisable)
	})
}
