package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardAsChatCompletions_StreamResponseFailedUsageLimitPersistsRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resetAt := time.Now().Add(time.Hour).Unix()
	upstreamSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_usage_limit","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		"",
		"event: response.failed",
		fmt.Sprintf(`data: {"type":"response.failed","response":{"id":"resp_usage_limit","object":"response","model":"gpt-5.5","status":"failed","output":[],"error":{"code":"rate_limit_exceeded","type":"usage_limit_reached","message":"The usage limit has been reached","resets_at":%d}}}`, resetAt),
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid_stream_usage_limit"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	repo := &openAIPassthroughFailoverRepo{}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
		rateLimitService: &RateLimitService{
			accountRepo: repo,
			cfg:         &config.Config{},
		},
	}
	account := &Account{
		ID:          70701,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(nil))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`), "", "")

	require.Error(t, err)
	require.Len(t, repo.rateLimitCalls, 1)
	require.WithinDuration(t, time.Unix(resetAt, 0), repo.rateLimitCalls[0], 2*time.Second)
	require.Empty(t, repo.tempCalls)
}

func TestOpenAIResponsesStreamErrorSideEffect_AccessDeniedTempUnschedulable(t *testing.T) {
	repo := &openAIPassthroughFailoverRepo{}
	svc := &OpenAIGatewayService{
		rateLimitService: &RateLimitService{
			accountRepo: repo,
			cfg:         &config.Config{},
		},
	}
	account := &Account{
		ID:       70702,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}
	payload := []byte(`{"type":"response.failed","response":{"status":"failed","error":{"type":"access_denied","message":"workspace forbidden by policy","details":{"reason":"ip_blocked"}}}}`)

	handled := svc.handleOpenAIResponsesStreamErrorSideEffect(context.Background(), account, http.Header{}, payload, "", false)

	require.True(t, handled)
	require.Len(t, repo.tempCalls, 1)
	require.Contains(t, repo.tempReasons[0], "workspace forbidden by policy")
	require.Contains(t, repo.tempReasons[0], "openai_403_counter_unavailable")
	require.Empty(t, repo.rateLimitCalls)
}

func TestOpenAIResponsesStreamErrorSideEffect_RequestPolicyDoesNotTouchAccount(t *testing.T) {
	repo := &openAIPassthroughFailoverRepo{}
	svc := &OpenAIGatewayService{
		rateLimitService: &RateLimitService{
			accountRepo: repo,
			cfg:         &config.Config{},
		},
	}
	account := &Account{
		ID:       70703,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}
	payload := []byte(`{"type":"response.failed","response":{"status":"failed","error":{"type":"invalid_request_error","code":"content_policy_violation","message":"access denied by content policy"}}}`)

	handled := svc.handleOpenAIResponsesStreamErrorSideEffect(context.Background(), account, http.Header{}, payload, "", false)

	require.False(t, handled)
	require.Empty(t, repo.tempCalls)
	require.Empty(t, repo.rateLimitCalls)
}
